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
