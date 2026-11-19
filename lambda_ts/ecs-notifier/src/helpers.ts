/**
 * ECS タスク状態変更通知 Lambda - ヘルパー関数
 *
 * メッセージフォーマット・件名生成・SNS 送信を index.ts から分離。
 */

import { PublishCommand } from "@aws-sdk/client-sns";
import type { EcsTaskDetail, NotificationResult, SnsPublisher } from "./types";
import { withRetry } from "./retry";
import type { RetryOptions } from "./retry";
import { createLogger, retryLogger } from "./logger";
import type { Logger } from "./logger";
import { retryMetrics } from "./metrics";
import type { Metrics } from "./metrics";

/** publishNotification の差し替え用オプション（テストから注入する） */
export interface PublishOptions {
  /** ログ出力先。省略時は ecs-notifier の既定ロガー */
  logger?: Logger;
  /** リトライの設定・待機関数・乱数などを差し替える */
  retry?: Omit<RetryOptions, "onRetry">;
  /**
   * メトリクスの記録先。省略時は記録しない。
   * ロガーと違って既定値を作らない。EMF は 1 回の呼び出しで 1 ドキュメントを
   * flush する形なので、ここで勝手に作ると呼び出しごとに別文書が出てしまう。
   * 生成と flush は呼び出し側（index.ts）に持たせる。
   */
  metrics?: Metrics;
}

/** onRetry に渡せるフックの型（retry.ts の RetryOptions と揃える） */
type RetryHook = (attempt: number, delayMs: number, error: unknown) => void;

// ── 文字列ヘルパー ──────────────────────────────────────────────────────────────

/** ARN の末尾セグメント（リソース名）を取り出す。 */
export function extractResourceName(arn: string): string {
  const parts = arn.split("/");
  return parts[parts.length - 1] ?? arn;
}

// ── メッセージ生成 ──────────────────────────────────────────────────────────────

/** ECS タスク詳細から通知メッセージ本文を生成する。 */
export function formatMessage(detail: EcsTaskDetail): string {
  const taskId = extractResourceName(detail.taskArn);
  const clusterName = extractResourceName(detail.clusterArn);

  const lines: string[] = [
    "ECS タスク状態変更",
    `クラスター : ${clusterName}`,
    `タスク ID  : ${taskId}`,
    `ステータス : ${detail.lastStatus}`,
  ];

  if (detail.group) {
    lines.push(`サービス   : ${detail.group}`);
  }
  if (detail.stoppedReason) {
    lines.push(`停止理由   : ${detail.stoppedReason}`);
  }

  return lines.join("\n");
}

/** ステータスに応じた件名文字列を生成する。 */
export function buildSubject(lastStatus: string): string {
  const statusEmoji: Record<string, string> = {
    RUNNING: "OK",
    STOPPED: "ALERT",
    PROVISIONING: "INFO",
    DEPROVISIONING: "INFO",
  };
  const label = statusEmoji[lastStatus] ?? "INFO";
  return `[ECS ${label}] タスク ${lastStatus}`;
}

// ── SNS 送信 ─────────────────────────────────────────────────────────────────

/** SNS トピックに通知を送信する。 */
/** リトライのログ・計測に使う操作名 */
export const RETRY_OPERATION = "Publish";

/**
 * SNS へ通知を送る。
 *
 * スロットリングや一時的な 5xx で通知が落ちないよう、retry.ts の withRetry で包む。
 * onRetry には logger.ts の retryLogger() を渡し、リトライの発生をログに残す。
 * retry / logger を差し替えられるよう、どちらも引数で受け取る。
 */
export async function publishNotification(
  client: SnsPublisher,
  topicArn: string,
  subject: string,
  message: string,
  options: PublishOptions = {}
): Promise<NotificationResult> {
  const logger =
    options.logger ?? createLogger().child({ component: "ecs-notifier" });
  const { metrics } = options;

  const publishWithRetry = withRetry(
    () =>
      client.send(
        new PublishCommand({ TopicArn: topicArn, Subject: subject, Message: message })
      ),
    {
      ...options.retry,
      // ログとメトリクスの両方に同じリトライを伝える。onRetry は 1 つしか
      // 渡せないため、ここで合成する。
      onRetry: combineRetryHooks(
        retryLogger(logger, RETRY_OPERATION),
        metrics && retryMetrics(metrics, RETRY_OPERATION)
      ),
    }
  );

  // 計測はリトライを含めた「通知が出るまで」。待たされた分を見たいため。
  const stopTimer = metrics?.timer("PublishLatency");

  try {
    const output = await publishWithRetry();

    recordMetrics(() => {
      stopTimer?.();
      metrics?.count("NotificationSuccess");
    });

    logger.info("通知を送信しました", {
      operation: RETRY_OPERATION,
      topicArn,
      subject,
      messageId: output.MessageId,
    });

    return { status: "published", messageId: output.MessageId };
  } catch (error) {
    recordMetrics(() => {
      stopTimer?.();
      metrics?.count("NotificationError");
    });
    // 通知の失敗は呼び出し側に伝える。メトリクスに出したことで
    // 握りつぶしてよい話にはならない。
    throw error;
  }
}

// ── メトリクス結線の補助 ──────────────────────────────────────────────────────

/**
 * 複数のリトライフックを 1 つにまとめる。undefined は無視する。
 *
 * 片方が投げてももう片方は呼ぶ。ログが落ちたせいでメトリクスまで
 * 欠けると、障害時に手がかりが両方なくなる。
 */
function combineRetryHooks(...hooks: (RetryHook | undefined | false)[]): RetryHook {
  const active = hooks.filter((h): h is RetryHook => typeof h === "function");
  return (attempt, delayMs, error) => {
    for (const hook of active) {
      try {
        hook(attempt, delayMs, error);
      } catch {
        // 記録できないこと自体は通知の失敗ではない
      }
    }
  };
}

/**
 * メトリクスの記録を行う。失敗しても通知を落とさない。
 *
 * metrics.ts は設定ミス（不正な単位・名前の長さ超過など）を例外にする。
 * CloudWatch が不正な EMF を黙って破棄するため、モジュール単体では
 * 大きな声で失敗させたい。ただし通知 Lambda の仕事は通知であって、
 * 計測の失敗で通知を落とすのは割に合わない。
 */
export function recordMetrics(fn: () => void): void {
  try {
    fn();
  } catch {
    // 握りつぶす
  }
}
