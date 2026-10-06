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

/** publishNotification の差し替え用オプション（テストから注入する） */
export interface PublishOptions {
  /** ログ出力先。省略時は ecs-notifier の既定ロガー */
  logger?: Logger;
  /** リトライの設定・待機関数・乱数などを差し替える */
  retry?: Omit<RetryOptions, "onRetry">;
}

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

  const publishWithRetry = withRetry(
    () =>
      client.send(
        new PublishCommand({ TopicArn: topicArn, Subject: subject, Message: message })
      ),
    {
      ...options.retry,
      onRetry: retryLogger(logger, RETRY_OPERATION),
    }
  );

  const output = await publishWithRetry();

  logger.info("通知を送信しました", {
    operation: RETRY_OPERATION,
    topicArn,
    subject,
    messageId: output.MessageId,
  });

  return { status: "published", messageId: output.MessageId };
}
