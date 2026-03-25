output "table_name" {
  description = "DynamoDB テーブル名"
  value       = aws_dynamodb_table.this.name
}

output "table_arn" {
  description = "DynamoDB テーブル ARN（IAM ポリシーで使用）"
  value       = aws_dynamodb_table.this.arn
}
