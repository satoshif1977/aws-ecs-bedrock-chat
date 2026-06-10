/**
 * ECS タスク状態変更通知 Lambda
 *
 * EventBridge から ECS Task State Change イベントを受け取り、
 * SNS トピックに日本語でフォーマットした通知メッセージを送信する。
 *
 * 環境変数:
 *   SNS_TOPIC_ARN - 通知先 SNS トピック ARN（未設定時はスキップ）
 *   AWS_REGION    - AWS リージョン（デフォルト: ap-northeast-1）
 */

import { SNSClient, PublishCommand, PublishCommandOutput } from "@aws-sdk/client-sns";

// ── 型定義 ────────────────────────────────────────────────────────────────────

export interface EcsTaskDetail {
  clusterArn: string;
  taskArn: string;
  lastStatus: string;
  desiredStatus: string;
  stoppedReason?: string;
  group?: string;
}

export interface EventBridgeEcsEvent {
  source: string;
  "detail-type": string;
  detail: EcsTaskDetail;
}

export interface NotificationResult {
  status: "published" | "skipped";
  messageId?: string;
}

/** SNS クライアントのインターフェース（テスト時にモックを注入できるよう分離）。 */
export interface SnsPublisher {
  send: (command: PublishCommand) => Promise<PublishCommandOutput>;
}

// ── ヘルパー関数 ──────────────────────────────────────────────────────────────

/** ARN の末尾セグメント（リソース名）を取り出す。 */
export function extractResourceName(arn: string): string {
  const parts = arn.split("/");
  return parts[parts.length - 1] ?? arn;
}

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

// ── SNS 送信 ──────────────────────────────────────────────────────────────────

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

// ── Lambda ハンドラー（テスト可能なファクトリ構造）──────────────────────────

/**
 * ハンドラーをファクトリ関数で生成する。
 * テスト時はモック client を渡して SNS 呼び出しを検証できる。
 */
export function createHandler(client: SnsPublisher) {
  return async (event: EventBridgeEcsEvent): Promise<NotificationResult> => {
    const topicArn = process.env.SNS_TOPIC_ARN;
    if (!topicArn) {
      console.warn("SNS_TOPIC_ARN が未設定のため通知をスキップします");
      return { status: "skipped" };
    }

    const { detail } = event;
    const message = formatMessage(detail);
    const subject = buildSubject(detail.lastStatus);

    console.log(`[ECS Notifier] ${subject}`);
    return publishNotification(client, topicArn, subject, message);
  };
}

export const handler = createHandler(
  new SNSClient({ region: process.env.AWS_REGION ?? "ap-northeast-1" })
);
