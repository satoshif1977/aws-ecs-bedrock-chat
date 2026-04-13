output "knowledge_base_id" {
  description = "Bedrock Knowledge Base ID（アプリの環境変数に渡す）"
  value       = aws_bedrockagent_knowledge_base.main.id
}

output "knowledge_bucket_name" {
  description = "ナレッジドキュメント格納 S3 バケット名"
  value       = aws_s3_bucket.knowledge.bucket
}

output "data_source_id" {
  description = "Knowledge Base データソース ID（Sync 時に使用）"
  value       = aws_bedrockagent_data_source.s3.data_source_id
}
