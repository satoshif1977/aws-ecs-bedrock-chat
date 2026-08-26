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

import { SNSClient } from "@aws-sdk/client-sns";
import type { EventBridgeEcsEvent, NotificationResult, SnsPublisher } from "./types";
import { formatMessage, buildSubject, publishNotification } from "./helpers";

// 型・ヘルパーを re-export（テストファイルが "./index" から import しているため）
export type { EcsTaskDetail, EventBridgeEcsEvent, NotificationResult, SnsPublisher } from "./types";
export { extractResourceName, formatMessage, buildSubject, publishNotification } from "./helpers";

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
