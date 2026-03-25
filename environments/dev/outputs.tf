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

output "github_actions_role_arn" {
  description = "GitHub Actions 用 IAM Role ARN（GitHub Variables: AWS_ROLE_ARN に設定する）"
  value       = module.cicd.github_actions_role_arn
}
