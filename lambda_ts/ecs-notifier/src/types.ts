/**
 * ECS タスク状態変更通知 Lambda - 型定義
 *
 * EventBridge ECS イベント・SNS 通知結果・DI 用インターフェースを定義する。
 */

import { PublishCommand, PublishCommandOutput } from "@aws-sdk/client-sns";

// ── ECS イベント型 ───────────────────────────────────────────────────────────

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

// ── 通知結果型 ───────────────────────────────────────────────────────────────

export interface NotificationResult {
  status: "published" | "skipped";
  messageId?: string;
}

// ── DI 用インターフェース ────────────────────────────────────────────────────

/** SNS クライアントのインターフェース（テスト時にモックを注入できるよう分離）。 */
export interface SnsPublisher {
  send: (command: PublishCommand) => Promise<PublishCommandOutput>;
}
