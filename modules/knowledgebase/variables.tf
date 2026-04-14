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
  description = "AWS アカウント ID"
  type        = string
}

variable "tags" {
  description = "共通タグ"
  type        = map(string)
  default     = {}
}

variable "caller_arn" {
  description = "Terraform 実行者の ARN（AOSS データアクセスポリシーに追加する）"
  type        = string
}
