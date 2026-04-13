# ── Task Execution Role ────────────────────────────────────
# ECS がコンテナを起動する際に使用するロール
# 役割: ECR からイメージを pull し、CloudWatch Logs にログを書き込む

resource "aws_iam_role" "task_execution" {
  name = "${var.project}-${var.env}-task-execution-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })

  tags = var.tags
}

# AWS 管理ポリシーをアタッチ（ECR pull + CloudWatch Logs 書き込み）
resource "aws_iam_role_policy_attachment" "task_execution" {
  role       = aws_iam_role.task_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# ── Task Role ──────────────────────────────────────────────
# コンテナ内アプリが AWS サービスを呼び出す際に使用するロール
# 役割: Bedrock の Claude Haiku 4.5 を呼び出す権限を付与

resource "aws_iam_role" "task" {
  name = "${var.project}-${var.env}-task-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })

  tags = var.tags
}

# DynamoDB 会話履歴への読み書き権限（Phase 6 追加）
# GetItem / PutItem のみ許可（最小権限）
resource "aws_iam_role_policy" "task_dynamodb" {
  name = "${var.project}-${var.env}-task-dynamodb-policy"
  role = aws_iam_role.task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "dynamodb:GetItem",
        "dynamodb:PutItem",
      ]
      Resource = [var.dynamodb_table_arn]
    }]
  })
}

# Bedrock Knowledge Base 検索権限（Phase 9 追加）
# Retrieve: 関連チャンク取得 / RetrieveAndGenerate: RAG 回答生成
resource "aws_iam_role_policy" "task_bedrock_kb" {
  name = "${var.project}-${var.env}-task-bedrock-kb-policy"
  role = aws_iam_role.task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "bedrock:Retrieve",
        "bedrock:RetrieveAndGenerate",
      ]
      Resource = "arn:aws:bedrock:${var.region}:${var.account_id}:knowledge-base/*"
    }]
  })
}

# Bedrock InvokeModel 権限（Claude Haiku 4.5 クロスリージョン推論プロファイル）
# クロスリージョン推論では 2つの Resource ARN が必要:
#   1. 推論プロファイル ARN（jp.* プレフィックス、アカウント ID 付き）
#   2. 基盤モデル ARN（ルーティング先リージョンのモデル、アカウント ID なし）
resource "aws_iam_role_policy" "task_bedrock" {
  name = "${var.project}-${var.env}-task-bedrock-policy"
  role = aws_iam_role.task.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = ["bedrock:InvokeModel", "bedrock:InvokeModelWithResponseStream"]
      Resource = [
        "arn:aws:bedrock:${var.region}:${var.account_id}:inference-profile/jp.anthropic.claude-haiku-4-5-20251001-v1:0",
        "arn:aws:bedrock:*::foundation-model/anthropic.claude-haiku-4-5-20251001-v1:0"
      ]
    }]
  })
}
