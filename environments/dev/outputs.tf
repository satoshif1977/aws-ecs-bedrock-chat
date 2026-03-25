output "alb_url" {
  description = "アプリへのアクセス URL（ブラウザで開く）"
  value       = "http://${module.alb.alb_dns_name}"
}

output "ecs_cluster_name" {
  description = "ECS Cluster 名"
  value       = module.ecs.cluster_name
}

output "ecs_service_name" {
  description = "ECS Service 名"
  value       = module.ecs.service_name
}

output "task_definition_arn" {
  description = "Task Definition ARN"
  value       = module.ecs.task_definition_arn
}

output "log_group_name" {
  description = "CloudWatch Log Group 名"
  value       = module.ecs.log_group_name
}
