variable "project" {
  description = "プロジェクト名"
  type        = string
}

variable "env" {
  description = "環境名（dev / prod）"
  type        = string
}

variable "tags" {
  description = "共通タグ"
  type        = map(string)
  default     = {}
}
