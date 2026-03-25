variable "project" {
  description = "プロジェクト名"
  type        = string
}

variable "env" {
  description = "環境名"
  type        = string
}

variable "aws_region" {
  description = "AWS リージョン"
  type        = string
}

variable "ecr_image_uri" {
  description = "ECR イメージ URI"
  type        = string
}

variable "task_cpu" {
  description = "タスク CPU ユニット"
  type        = number
  default     = 256
}

variable "task_memory" {
  description = "タスクメモリ MiB"
  type        = number
  default     = 512
}

variable "container_port" {
  description = "コンテナポート"
  type        = number
  default     = 8501
}

variable "log_retention_days" {
  description = "ログ保持期間（日）"
  type        = number
  default     = 7
}

variable "desired_count" {
  description = "ECS Service の起動タスク数"
  type        = number
  default     = 1
}

variable "ecr_repository_name" {
  description = "ECR リポジトリ名（ARN 取得用）"
  type        = string
  default     = "bedrock-chat"
}

variable "github_repo" {
  description = "GitHub リポジトリ（owner/repo 形式）"
  type        = string
}
