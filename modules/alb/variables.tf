variable "project" {
  description = "プロジェクト名"
  type        = string
}

variable "env" {
  description = "環境名"
  type        = string
}

variable "vpc_id" {
  description = "VPC ID"
  type        = string
}

variable "subnet_ids" {
  description = "ALB を配置するサブネット ID リスト（2AZ 以上必須）"
  type        = list(string)
}

variable "alb_security_group_id" {
  description = "ALB Security Group ID"
  type        = string
}

variable "container_port" {
  description = "コンテナポート（Target Group のポート）"
  type        = number
  default     = 8501
}

variable "tags" {
  description = "共通タグ"
  type        = map(string)
  default     = {}
}
