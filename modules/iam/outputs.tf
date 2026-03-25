output "task_execution_role_arn" {
  description = "Task Execution Role ARN（ECS がコンテナ起動時に使用）"
  value       = aws_iam_role.task_execution.arn
}

output "task_role_arn" {
  description = "Task Role ARN（コンテナが AWS サービスを呼び出す際に使用）"
  value       = aws_iam_role.task.arn
}
