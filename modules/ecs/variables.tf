variable "project" {
  description = "プロジェクト名"
  type        = string
}

variable "env" {
  description = "環境名（dev / prod）"
  type        = string
}

variable "region" {
  description = "AWS リージョン"
  type        = string
}

variable "ecr_image_uri" {
  description = "ECR イメージ URI（例: 123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/repo:tag）"
  type        = string
}

variable "task_cpu" {
  description = "タスクに割り当てる CPU ユニット（256 = 0.25 vCPU）"
  type        = number
  default     = 256
}

variable "task_memory" {
  description = "タスクに割り当てるメモリ MiB（512 = 0.5 GB）"
  type        = number
  default     = 512
}

variable "container_port" {
  description = "コンテナが LISTEN するポート（Streamlit デフォルト: 8501）"
  type        = number
  default     = 8501
}

variable "task_execution_role_arn" {
  description = "Task Execution Role ARN（IAM モジュールから渡す）"
  type        = string
}

variable "task_role_arn" {
  description = "Task Role ARN（IAM モジュールから渡す）"
  type        = string
}

variable "log_retention_days" {
  description = "CloudWatch Logs の保持期間（日）"
  type        = number
  default     = 7
}

# ── Phase 6 追加変数（DynamoDB 連携）──────────────────────

variable "dynamodb_table_name" {
  description = "DynamoDB テーブル名（コンテナの環境変数 DYNAMODB_TABLE_NAME に渡す）"
  type        = string
}

# ── Phase 5 追加変数（ECS Service 用）──────────────────────

variable "desired_count" {
  description = "ECS Service の起動タスク数"
  type        = number
  default     = 1
}

variable "subnet_ids" {
  description = "ECS Task を起動するサブネット ID リスト"
  type        = list(string)
}

variable "ecs_task_security_group_id" {
  description = "ECS Task に適用するセキュリティグループ ID"
  type        = string
}

variable "alb_target_group_arn" {
  description = "ALB Target Group ARN（ECS Service のロードバランサー登録先）"
  type        = string
}

variable "tags" {
  description = "共通タグ"
  type        = map(string)
  default     = {}
}
