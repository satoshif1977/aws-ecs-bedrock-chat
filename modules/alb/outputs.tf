output "alb_dns_name" {
  description = "ALB の DNS 名（ブラウザアクセス用）"
  value       = aws_lb.this.dns_name
}

output "target_group_arn" {
  description = "Target Group ARN（ECS Service の load_balancer ブロックで使用）"
  value       = aws_lb_target_group.this.arn
}

output "listener_arn" {
  description = "HTTP Listener ARN"
  value       = aws_lb_listener.http.arn
}
