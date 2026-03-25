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

variable "account_id" {
  description = "AWS アカウント ID（推論プロファイル ARN の組み立てに使用）"
  type        = string
}

variable "dynamodb_table_arn" {
  description = "DynamoDB テーブル ARN（Task Role に GetItem / PutItem を付与）"
  type        = string
}

variable "tags" {
  description = "共通タグ"
  type        = map(string)
  default     = {}
}
