# ── Bedrock Knowledge Base 用 IAM ロール ──────────────────
# Knowledge Base が S3・OpenSearch Serverless・Bedrock Embedding に
# アクセスするための専用ロール

resource "aws_iam_role" "knowledge_base" {
  name = "${var.project}-${var.env}-kb-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "bedrock.amazonaws.com" }
      Action    = "sts:AssumeRole"
      Condition = {
        StringEquals = {
          "aws:SourceAccount" = var.account_id
        }
        ArnLike = {
          "aws:SourceArn" = "arn:aws:bedrock:${var.region}:${var.account_id}:knowledge-base/*"
        }
      }
    }]
  })

  tags = var.tags
}

# S3 からドキュメントを読み込む権限
resource "aws_iam_role_policy" "kb_s3" {
  name = "s3-read"
  role = aws_iam_role.knowledge_base.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["s3:GetObject", "s3:ListBucket"]
      Resource = [
        aws_s3_bucket.knowledge.arn,
        "${aws_s3_bucket.knowledge.arn}/*"
      ]
    }]
  })
}

# OpenSearch Serverless コレクションにベクトルを書き込む権限
resource "aws_iam_role_policy" "kb_aoss" {
  name = "aoss-access"
  role = aws_iam_role.knowledge_base.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["aoss:APIAccessAll"]
      Resource = aws_opensearchserverless_collection.knowledge.arn
    }]
  })
}

# Terraform 実行者（admin ロール）に AOSS データプレーンアクセス権を付与
# ─────────────────────────────────────────────────────────────────────
# AOSS はデータプレーン操作（インデックス作成等）に
# データアクセスポリシーへの追加 に加えて
# IAM ポリシー側にも aoss:APIAccessAll が必要（IAM と AOSS の二重認証モデル）
# ロール名を ARN から split で取り出す例: arn:aws:iam::ACCOUNT:role/ROLE → ROLE

resource "aws_iam_role_policy" "admin_aoss_access" {
  name = "aoss-data-plane-access-for-setup"
  role = split("/", local.admin_iam_role_arn)[1]

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["aoss:APIAccessAll"]
      Resource = aws_opensearchserverless_collection.knowledge.arn
    }]
  })
}

# IAM ポリシー伝播待機（IAM は最大10秒程度で有効化される）
resource "time_sleep" "wait_iam_propagation" {
  create_duration = "20s"
  depends_on      = [aws_iam_role_policy.admin_aoss_access]
}

# Bedrock Titan Embed でテキストをベクトル化する権限
resource "aws_iam_role_policy" "kb_bedrock_embed" {
  name = "bedrock-embed"
  role = aws_iam_role.knowledge_base.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["bedrock:InvokeModel"]
      Resource = "arn:aws:bedrock:${var.region}::foundation-model/amazon.titan-embed-text-v2:0"
    }]
  })
}
