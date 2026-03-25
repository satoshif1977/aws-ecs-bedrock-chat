output "github_actions_role_arn" {
  description = "GitHub Actions が AssumeRole する IAM Role ARN（GitHub Variables に設定する）"
  value       = aws_iam_role.github_actions.arn
}
