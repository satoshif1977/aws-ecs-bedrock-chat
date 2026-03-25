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

variable "github_repo" {
  description = "GitHub リポジトリ（owner/repo 形式）例: satoshif1977/aws-ecs-bedrock-chat"
  type        = string
}

variable "ecr_repository_arn" {
  description = "ECR リポジトリ ARN（イメージ push 権限の対象）"
  type        = string
}

variable "tags" {
  description = "共通タグ"
  type        = map(string)
  default     = {}
}
