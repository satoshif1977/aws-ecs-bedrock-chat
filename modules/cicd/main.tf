# ── GitHub Actions OIDC Provider ───────────────────────────
# GitHub Actions が AWS に一時トークンで認証するための設定
# アクセスキーの発行・管理が不要になる（セキュリティベストプラクティス）

resource "aws_iam_openid_connect_provider" "github" {
  url = "https://token.actions.githubusercontent.com"

  client_id_list = ["sts.amazonaws.com"]

  # GitHub Actions OIDC の公式サムプリント
  thumbprint_list = ["6938fd4d98bab03faadb97b34396831e3780aea1"]
}

# ── GitHub Actions 用 IAM Role ─────────────────────────────
# GitHub の特定リポジトリ / ブランチからのみ AssumeRole を許可する
# Condition で repo と ref を絞り込むことで最小権限を実現

resource "aws_iam_role" "github_actions" {
  name = "${var.project}-${var.env}-github-actions-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Principal = {
        Federated = aws_iam_openid_connect_provider.github.arn
      }
      Action = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
        }
        StringEquals = {
          # 対象リポジトリの master ブランチのみ許可（完全一致・ワイルドカード不使用）
          "token.actions.githubusercontent.com:sub" = "repo:${var.github_repo}:ref:refs/heads/master"
        }
      }
    }]
  })

  tags = var.tags
}

# ── ECR への push 権限 ─────────────────────────────────────
# GetAuthorizationToken: ECR ログインに必要
# BatchCheckLayerAvailability / PutImage / InitiateLayerUpload / UploadLayerPart / CompleteLayerUpload: イメージ push に必要

resource "aws_iam_role_policy" "ecr_push" {
  name = "${var.project}-${var.env}-github-ecr-push-policy"
  role = aws_iam_role.github_actions.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["ecr:GetAuthorizationToken"]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "ecr:BatchCheckLayerAvailability",
          "ecr:PutImage",
          "ecr:InitiateLayerUpload",
          "ecr:UploadLayerPart",
          "ecr:CompleteLayerUpload",
        ]
        Resource = var.ecr_repository_arn
      }
    ]
  })
}

# ── ECS Service 更新権限 ───────────────────────────────────
# force-new-deployment で新タスクを起動するために必要

resource "aws_iam_role_policy" "ecs_deploy" {
  name = "${var.project}-${var.env}-github-ecs-deploy-policy"
  role = aws_iam_role.github_actions.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      # UpdateService: force-new-deployment に必要
      # DescribeServices: aws ecs wait services-stable が内部で呼び出す
      Action = ["ecs:UpdateService", "ecs:DescribeServices"]
      Resource = "arn:aws:ecs:${var.region}:${var.account_id}:service/${var.project}-${var.env}-cluster/${var.project}-${var.env}-service"
    }]
  })
}
