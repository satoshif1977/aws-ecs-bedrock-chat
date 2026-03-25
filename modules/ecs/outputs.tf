output "cluster_id" {
  description = "ECS Cluster ID"
  value       = aws_ecs_cluster.this.id
}

output "cluster_arn" {
  description = "ECS Cluster ARN"
  value       = aws_ecs_cluster.this.arn
}

output "cluster_name" {
  description = "ECS Cluster 名"
  value       = aws_ecs_cluster.this.name
}

output "task_definition_arn" {
  description = "Task Definition ARN"
  value       = aws_ecs_task_definition.this.arn
}

output "service_name" {
  description = "ECS Service 名"
  value       = aws_ecs_service.this.name
}

output "log_group_name" {
  description = "CloudWatch Log Group 名"
  value       = aws_cloudwatch_log_group.this.name
}
