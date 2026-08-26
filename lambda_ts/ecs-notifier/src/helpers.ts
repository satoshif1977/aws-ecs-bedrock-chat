/**
 * ECS タスク状態変更通知 Lambda - ヘルパー関数
 *
 * メッセージフォーマット・件名生成・SNS 送信を index.ts から分離。
 */

import { PublishCommand } from "@aws-sdk/client-sns";
import type { EcsTaskDetail, NotificationResult, SnsPublisher } from "./types";

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
export async function publishNotification(
  client: SnsPublisher,
  topicArn: string,
  subject: string,
  message: string
): Promise<NotificationResult> {
  const output = await client.send(
    new PublishCommand({ TopicArn: topicArn, Subject: subject, Message: message })
  );
  return { status: "published", messageId: output.MessageId };
}
