output "alb_security_group_id" {
  description = "ALB Security Group ID"
  value       = aws_security_group.alb.id
}

output "ecs_task_security_group_id" {
  description = "ECS Task Security Group ID"
  value       = aws_security_group.ecs_task.id
}
