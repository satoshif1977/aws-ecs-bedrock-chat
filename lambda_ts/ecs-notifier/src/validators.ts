/**
 * ECS タスク状態変更イベント バリデーター
 *
 * EventBridge ECS イベントの入力データを検証する純粋関数群。
 * AWS SDK に依存しないため単体テストが容易。
 *
 * 検証内容:
 *   - ECS クラスター ARN / タスク ARN のフォーマット
 *   - タスクステータスの有効値（PROVISIONING〜STOPPED）
 *   - EventBridge イベントの source / detail-type
 *   - コンテナ終了コード分類
 *   - サービスグループ名のパターン
 *   - SNS Subject の長さ制約
 */

// ── 型定義 ────────────────────────────────────────────────────

export interface ValidationError {
  field: string;
  message: string;
  severity: "error" | "warning";
}

export interface EcsEventInput {
  source?: string;
  "detail-type"?: string;
  detail?: {
    clusterArn?: string;
    taskArn?: string;
    lastStatus?: string;
    desiredStatus?: string;
    stoppedReason?: string;
    group?: string;
  };
}

// ── 定数 ─────────────────────────────────────────────────────

/** ECS タスクの有効なステータス値 */
export const VALID_TASK_STATUSES = [
  "PROVISIONING",
  "PENDING",
  "ACTIVATING",
  "RUNNING",
  "DEACTIVATING",
  "STOPPING",
  "DEPROVISIONING",
  "STOPPED",
] as const;

/** アラートを発するステータス（注意が必要） */
export const ALERT_STATUSES = ["STOPPED"] as const;

/** 情報レベルのステータス */
export const INFO_STATUSES = [
  "PROVISIONING",
  "PENDING",
  "ACTIVATING",
  "DEACTIVATING",
  "STOPPING",
  "DEPROVISIONING",
] as const;

/** EventBridge ECS イベントの source */
export const ECS_EVENT_SOURCE = "aws.ecs";

/** EventBridge ECS タスク状態変更の detail-type */
export const ECS_TASK_STATE_CHANGE = "ECS Task State Change";

/** ECS クラスター ARN の正規表現 */
export const CLUSTER_ARN_PATTERN =
  /^arn:aws:ecs:[a-z0-9-]+:\d{12}:cluster\/[\w-]+$/;

/** ECS タスク ARN の正規表現 */
export const TASK_ARN_PATTERN =
  /^arn:aws:ecs:[a-z0-9-]+:\d{12}:task\/[\w-]+\/[a-f0-9]+$/;

/** ECS サービスグループの正規表現（service:サービス名） */
export const SERVICE_GROUP_PATTERN = /^service:[\w-]+$/;

/** SNS Subject の最大バイト数（AWS 制約） */
export const MAX_SNS_SUBJECT_BYTES = 256;

/** OOM Kill の終了コード */
export const EXIT_CODE_OOM_KILL = 137;

/** 正常終了の終了コード */
export const EXIT_CODE_SUCCESS = 0;

// ── ARN バリデーション ────────────────────────────────────────

/** ECS クラスター ARN が有効なフォーマットか */
export function isValidClusterArn(arn: string): boolean {
  return CLUSTER_ARN_PATTERN.test(arn);
}

/** ECS タスク ARN が有効なフォーマットか */
export function isValidTaskArn(arn: string): boolean {
  return TASK_ARN_PATTERN.test(arn);
}

/** ARN からリージョンを抽出する（無効な ARN は空文字） */
export function extractRegion(arn: string): string {
  const match = arn.match(/^arn:aws:ecs:([a-z0-9-]+):/);
  return match ? match[1] : "";
}

/** ARN からアカウント ID を抽出する（無効な ARN は空文字） */
export function extractAccountId(arn: string): string {
  const match = arn.match(/^arn:aws:ecs:[a-z0-9-]+:(\d{12}):/);
  return match ? match[1] : "";
}

// ── ステータスバリデーション ──────────────────────────────────

/** タスクステータスが有効な値か */
export function isValidTaskStatus(status: string): boolean {
  return (VALID_TASK_STATUSES as readonly string[]).includes(status);
}

/** アラートステータスか */
export function isAlertStatus(status: string): boolean {
  return (ALERT_STATUSES as readonly string[]).includes(status);
}

/** ステータスの遷移が論理的に正しいか（lastStatus → desiredStatus） */
export function isValidStatusTransition(
  lastStatus: string,
  desiredStatus: string
): boolean {
  if (!isValidTaskStatus(lastStatus) || !isValidTaskStatus(desiredStatus)) {
    return false;
  }
  const lastIdx = VALID_TASK_STATUSES.indexOf(
    lastStatus as (typeof VALID_TASK_STATUSES)[number]
  );
  const desiredIdx = VALID_TASK_STATUSES.indexOf(
    desiredStatus as (typeof VALID_TASK_STATUSES)[number]
  );
  // desiredStatus は RUNNING か STOPPED のどちらかが一般的
  if (desiredStatus !== "RUNNING" && desiredStatus !== "STOPPED") {
    return false;
  }
  // lastStatus は desiredStatus と同じか、それより前のステータス
  return lastIdx <= desiredIdx;
}

// ── 終了コード分類 ───────────────────────────────────────────

/** 終了コードの意味を分類する */
export function classifyExitCode(
  exitCode: number
): "success" | "oom_kill" | "error" {
  if (exitCode === EXIT_CODE_SUCCESS) return "success";
  if (exitCode === EXIT_CODE_OOM_KILL) return "oom_kill";
  return "error";
}

/** 終了コードが正常か */
export function isSuccessExitCode(exitCode: number): boolean {
  return exitCode === EXIT_CODE_SUCCESS;
}

// ── サービスグループバリデーション ────────────────────────────

/** ECS サービスグループ名が有効なパターンか */
export function isValidServiceGroup(group: string): boolean {
  return SERVICE_GROUP_PATTERN.test(group);
}

/** グループ名からサービス名を抽出する（無効な場合は空文字） */
export function extractServiceName(group: string): string {
  if (!isValidServiceGroup(group)) return "";
  return group.replace("service:", "");
}

// ── SNS Subject バリデーション ────────────────────────────────

/** SNS Subject のバイト数が制限内か（UTF-8 換算） */
export function isValidSnsSubject(subject: string): boolean {
  const byteLength = new TextEncoder().encode(subject).length;
  return byteLength > 0 && byteLength <= MAX_SNS_SUBJECT_BYTES;
}

/** SNS Subject のバイト数を取得する */
export function getSnsSubjectByteLength(subject: string): number {
  return new TextEncoder().encode(subject).length;
}

// ── EventBridge イベントバリデーション ────────────────────────

/** EventBridge ECS イベントの構造を検証する */
export function validateEcsEvent(event: EcsEventInput): ValidationError[] {
  const errors: ValidationError[] = [];

  // source チェック
  if (event.source !== ECS_EVENT_SOURCE) {
    errors.push({
      field: "source",
      message: `無効な source: "${event.source}"。期待値: "${ECS_EVENT_SOURCE}"`,
      severity: "error",
    });
  }

  // detail-type チェック
  if (event["detail-type"] !== ECS_TASK_STATE_CHANGE) {
    errors.push({
      field: "detail-type",
      message: `無効な detail-type: "${event["detail-type"]}"。期待値: "${ECS_TASK_STATE_CHANGE}"`,
      severity: "error",
    });
  }

  // detail の存在チェック
  if (!event.detail) {
    errors.push({
      field: "detail",
      message: "detail フィールドが未定義です",
      severity: "error",
    });
    return errors;
  }

  const { detail } = event;

  // clusterArn
  if (!detail.clusterArn) {
    errors.push({
      field: "detail.clusterArn",
      message: "clusterArn が未定義です",
      severity: "error",
    });
  } else if (!isValidClusterArn(detail.clusterArn)) {
    errors.push({
      field: "detail.clusterArn",
      message: `無効なクラスター ARN フォーマット: "${detail.clusterArn}"`,
      severity: "error",
    });
  }

  // taskArn
  if (!detail.taskArn) {
    errors.push({
      field: "detail.taskArn",
      message: "taskArn が未定義です",
      severity: "error",
    });
  } else if (!isValidTaskArn(detail.taskArn)) {
    errors.push({
      field: "detail.taskArn",
      message: `無効なタスク ARN フォーマット: "${detail.taskArn}"`,
      severity: "error",
    });
  }

  // リージョン・アカウント ID の一致チェック
  if (detail.clusterArn && detail.taskArn) {
    const clusterRegion = extractRegion(detail.clusterArn);
    const taskRegion = extractRegion(detail.taskArn);
    if (clusterRegion && taskRegion && clusterRegion !== taskRegion) {
      errors.push({
        field: "detail",
        message: `クラスターとタスクのリージョンが不一致: ${clusterRegion} vs ${taskRegion}`,
        severity: "error",
      });
    }

    const clusterAccount = extractAccountId(detail.clusterArn);
    const taskAccount = extractAccountId(detail.taskArn);
    if (clusterAccount && taskAccount && clusterAccount !== taskAccount) {
      errors.push({
        field: "detail",
        message: `クラスターとタスクのアカウント ID が不一致: ${clusterAccount} vs ${taskAccount}`,
        severity: "error",
      });
    }
  }

  // lastStatus
  if (!detail.lastStatus) {
    errors.push({
      field: "detail.lastStatus",
      message: "lastStatus が未定義です",
      severity: "error",
    });
  } else if (!isValidTaskStatus(detail.lastStatus)) {
    errors.push({
      field: "detail.lastStatus",
      message: `無効なタスクステータス: "${detail.lastStatus}"。有効値: ${VALID_TASK_STATUSES.join(", ")}`,
      severity: "error",
    });
  }

  // desiredStatus
  if (!detail.desiredStatus) {
    errors.push({
      field: "detail.desiredStatus",
      message: "desiredStatus が未定義です",
      severity: "error",
    });
  } else if (!isValidTaskStatus(detail.desiredStatus)) {
    errors.push({
      field: "detail.desiredStatus",
      message: `無効なタスクステータス: "${detail.desiredStatus}"。有効値: ${VALID_TASK_STATUSES.join(", ")}`,
      severity: "error",
    });
  }

  // ステータス遷移チェック
  if (
    detail.lastStatus &&
    detail.desiredStatus &&
    isValidTaskStatus(detail.lastStatus) &&
    isValidTaskStatus(detail.desiredStatus) &&
    !isValidStatusTransition(detail.lastStatus, detail.desiredStatus)
  ) {
    errors.push({
      field: "detail",
      message: `不正なステータス遷移: ${detail.lastStatus} → ${detail.desiredStatus}`,
      severity: "warning",
    });
  }

  // group（オプショナルだが、指定されていればフォーマット検証）
  if (detail.group !== undefined && !isValidServiceGroup(detail.group)) {
    errors.push({
      field: "detail.group",
      message: `無効なサービスグループ名: "${detail.group}"。"service:<name>" 形式が期待されます`,
      severity: "warning",
    });
  }

  // STOPPED + stoppedReason なし → warning
  if (detail.lastStatus === "STOPPED" && !detail.stoppedReason) {
    errors.push({
      field: "detail.stoppedReason",
      message:
        "タスクが STOPPED なのに stoppedReason がありません。原因特定が困難になります",
      severity: "warning",
    });
  }

  return errors;
}

// ── ユーティリティ ────────────────────────────────────────────

/** エラーの有無を判定する（warning は含まない） */
export function hasErrors(errors: ValidationError[]): boolean {
  return errors.some((e) => e.severity === "error");
}

/** エラーをフォーマットする */
export function formatErrors(errors: ValidationError[]): string {
  if (errors.length === 0) return "すべてのチェックが通過しました";
  return errors
    .map((e) => `[${e.severity.toUpperCase()}] ${e.field}: ${e.message}`)
    .join("\n");
}
