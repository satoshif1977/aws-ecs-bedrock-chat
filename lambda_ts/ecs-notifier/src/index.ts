/**
 * ECS タスク状態変更通知 Lambda
 *
 * EventBridge から ECS Task State Change イベントを受け取り、
 * SNS トピックに日本語でフォーマットした通知メッセージを送信する。
 *
 * 環境変数:
 *   SNS_TOPIC_ARN - 通知先 SNS トピック ARN（未設定時はスキップ）
 *   AWS_REGION    - AWS リージョン（デフォルト: ap-northeast-1）
 *   LOG_LEVEL     - 検証結果の出力レベル（デフォルト: info）
 *   METRICS_NAMESPACE - EMF メトリクスの名前空間（デフォルト: EcsNotifier）
 *
 * メトリクスは同ディレクトリの metrics.ts（EMF・標準出力に 1 行 JSON）を使う。
 * クラスター名・タスク ARN・ステータスはディメンションに入れない。値の種類の
 * 数だけ課金対象のカスタムメトリクスが作られるため、setProperty() で持たせる。
 */

import { SNSClient } from "@aws-sdk/client-sns";
import type { EventBridgeEcsEvent, NotificationResult, SnsPublisher } from "./types";
import {
  formatMessage,
  buildSubject,
  publishNotification,
  recordMetrics,
} from "./helpers";
import {
  validateEcsEvent,
  isUnprocessable,
  missingRequiredFields,
  formatErrors,
} from "./validators";
import type { EcsEventInput } from "./validators";
import { createLoggerFromEnv } from "./logger";
import type { Logger } from "./logger";
import { createMetrics } from "./metrics";
import type { Metrics } from "./metrics";

// 型・ヘルパーを re-export（テストファイルが "./index" から import しているため）
export type { EcsTaskDetail, EventBridgeEcsEvent, NotificationResult, SnsPublisher } from "./types";
export { extractResourceName, formatMessage, buildSubject, publishNotification } from "./helpers";

/** METRICS_NAMESPACE 未設定時に使う EMF の名前空間 */
export const DEFAULT_METRICS_NAMESPACE = "EcsNotifier";

/** createHandler の差し替え用オプション（テストから注入する） */
export interface HandlerOptions {
  /** 検証結果の出力先。省略時は環境変数からロガーを組み立てる */
  logger?: Logger;
  /** メトリクスの記録先。省略時は既定の名前空間で組み立てる */
  metrics?: Metrics;
}

/**
 * メトリクスレコーダを作る。作れなければ null を返す。
 *
 * metrics.ts は設定ミス（空の名前空間など）を例外にする。CloudWatch が
 * 不正な EMF を黙って破棄するため、モジュール単体では大きな声で失敗させたい。
 * ただしこの Lambda の仕事は通知であって、環境変数の設定ミスで通知まで
 * 落とすのは割に合わない。
 */
function tryCreateMetrics(logger: Logger): Metrics | null {
  try {
    return createMetrics({
      namespace: process.env.METRICS_NAMESPACE ?? DEFAULT_METRICS_NAMESPACE,
      dimensions: { Handler: "ecs-notifier" },
    });
  } catch (error) {
    logger.warn("メトリクスの初期化に失敗しました", {
      error: error instanceof Error ? error.message : String(error),
    });
    return null;
  }
}

// ── Lambda ハンドラー（テスト可能なファクトリ構造）──────────────────────────

/**
 * ハンドラーをファクトリ関数で生成する。
 * テスト時はモック client を渡して SNS 呼び出しを検証できる。
 */
export function createHandler(client: SnsPublisher, options: HandlerOptions = {}) {
  return async (event: EventBridgeEcsEvent): Promise<NotificationResult> => {
    const log = (options.logger ?? createLoggerFromEnv()).child({
      component: "ecs-notifier",
    });
    // EMF は 1 回の呼び出しで 1 ドキュメントを書く。実行環境が使い回されたときに
    // 前回のプロパティが残らないよう、呼び出しごとに作る。
    const metrics = options.metrics ?? tryCreateMetrics(log);

    try {
      const topicArn = process.env.SNS_TOPIC_ARN;
      if (!topicArn) {
        console.warn("SNS_TOPIC_ARN が未設定のため通知をスキップします");
        recordMetrics(() => {
          metrics?.setProperty("skipReason", "missing-topic-arn");
          metrics?.count("EventSkipped");
        });
        return { status: "skipped" };
      }

      // ── 入力の検証 ──
      // イベントは EventBridge 経由で外から来るため、型どおりとは限らない。
      const input = event as EcsEventInput;

      if (isUnprocessable(input)) {
        // 本文を組み立てられない。このまま進むと formatMessage が TypeError で落ち、
        // EventBridge が同じイベントを再試行し続けることになる。
        const missing = missingRequiredFields(input);
        log.error("必須フィールドが欠けているため通知できません", {
          missingFields: missing,
        });
        recordMetrics(() => {
          metrics?.setProperty("skipReason", "missing-required-fields");
          metrics?.setProperty("missingFields", missing);
          metrics?.count("EventSkipped");
        });
        return { status: "skipped", reason: `必須フィールドの欠落: ${missing.join(", ")}` };
      }

      const problems = validateEcsEvent(input);
      if (problems.length > 0) {
        // フォーマット不正や想定外のステータス遷移。通知は止めない。
        // 通知 Lambda にとっては「通知が出ないこと」のほうが重大なため。
        log.warn("イベントに想定外の内容が含まれています", {
          problemCount: problems.length,
          problems: formatErrors(problems),
        });
        recordMetrics(() => metrics?.count("ValidationProblem", problems.length));
      }

      const { detail } = event;
      const message = formatMessage(detail);
      const subject = buildSubject(detail.lastStatus);

      // ステータス・クラスター・タスクはディメンションにせずプロパティに置く。
      // ECS のステータスは将来増えうるうえ、ディメンションを足すとその組み合わせ
      // の数だけ課金対象のカスタムメトリクスが作られる。
      recordMetrics(() => {
        metrics?.setProperty("lastStatus", detail.lastStatus);
        metrics?.setProperty("clusterArn", detail.clusterArn);
        metrics?.setProperty("taskArn", detail.taskArn);
      });

      console.log(`[ECS Notifier] ${subject}`);
      // await してから返す。await を外すと finally が送信の完了を待たずに走り、
      // 成否のメトリクスが入る前に flush してしまう。
      return await publishNotification(client, topicArn, subject, message, {
        logger: log,
        ...(metrics ? { metrics } : {}),
      });
    } finally {
      // EMF は 1 行の JSON を標準出力に書くだけなので、ここでの失敗は本処理に影響しない
      if (metrics) recordMetrics(() => metrics.flush());
    }
  };
}

export const handler = createHandler(
  new SNSClient({ region: process.env.AWS_REGION ?? "ap-northeast-1" })
);
