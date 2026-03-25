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

variable "container_port" {
  description = "コンテナポート（Streamlit: 8501）"
  type        = number
  default     = 8501
}

variable "tags" {
  description = "共通タグ"
  type        = map(string)
  default     = {}
}
