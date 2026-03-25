output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.this.id
}

output "public_subnet_ids" {
  description = "パブリックサブネット ID リスト（ALB・ECS Task に使用）"
  value       = aws_subnet.public[*].id
}
