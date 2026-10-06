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
 */

import { SNSClient } from "@aws-sdk/client-sns";
import type { EventBridgeEcsEvent, NotificationResult, SnsPublisher } from "./types";
import { formatMessage, buildSubject, publishNotification } from "./helpers";
import {
  validateEcsEvent,
  isUnprocessable,
  missingRequiredFields,
  formatErrors,
} from "./validators";
import type { EcsEventInput } from "./validators";
import { createLoggerFromEnv } from "./logger";
import type { Logger } from "./logger";

// 型・ヘルパーを re-export（テストファイルが "./index" から import しているため）
export type { EcsTaskDetail, EventBridgeEcsEvent, NotificationResult, SnsPublisher } from "./types";
export { extractResourceName, formatMessage, buildSubject, publishNotification } from "./helpers";

/** createHandler の差し替え用オプション（テストから注入する） */
export interface HandlerOptions {
  /** 検証結果の出力先。省略時は環境変数からロガーを組み立てる */
  logger?: Logger;
}

// ── Lambda ハンドラー（テスト可能なファクトリ構造）──────────────────────────

/**
 * ハンドラーをファクトリ関数で生成する。
 * テスト時はモック client を渡して SNS 呼び出しを検証できる。
 */
export function createHandler(client: SnsPublisher, options: HandlerOptions = {}) {
  return async (event: EventBridgeEcsEvent): Promise<NotificationResult> => {
    const topicArn = process.env.SNS_TOPIC_ARN;
    if (!topicArn) {
      console.warn("SNS_TOPIC_ARN が未設定のため通知をスキップします");
      return { status: "skipped" };
    }

    const log = (options.logger ?? createLoggerFromEnv()).child({
      component: "ecs-notifier",
    });

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
    }

    const { detail } = event;
    const message = formatMessage(detail);
    const subject = buildSubject(detail.lastStatus);

    console.log(`[ECS Notifier] ${subject}`);
    return publishNotification(client, topicArn, subject, message, {
      logger: log,
    });
  };
}

export const handler = createHandler(
  new SNSClient({ region: process.env.AWS_REGION ?? "ap-northeast-1" })
);
