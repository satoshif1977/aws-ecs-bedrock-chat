# ── Terraform 実行者 ARN の正規化 ─────────────────────────
# AOSS データアクセスポリシーは STS セッション ARN を受け付けない
# assumed-role ARN を IAM role ARN 形式に変換して Principal に追加する
# 例: arn:aws:sts::ACCOUNT:assumed-role/ROLE/SESSION
#   → arn:aws:iam::ACCOUNT:role/ROLE

locals {
  admin_iam_role_arn = (
    strcontains(var.caller_arn, ":assumed-role/")
    ? "arn:aws:iam::${var.account_id}:role/${regex("assumed-role/([^/]+)/", var.caller_arn)[0]}"
    : var.caller_arn
  )
}

# ── S3 バケット（ナレッジドキュメント格納）─────────────────
# Knowledge Base が参照するドキュメントを格納する S3 バケット
# バケット名にアカウント ID を含めてグローバル一意性を確保

resource "aws_s3_bucket" "knowledge" {
  bucket        = "${var.project}-${var.env}-knowledge-${var.account_id}"
  force_destroy = true
  tags          = var.tags
}

resource "aws_s3_bucket_versioning" "knowledge" {
  bucket = aws_s3_bucket.knowledge.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_public_access_block" "knowledge" {
  bucket                  = aws_s3_bucket.knowledge.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ── OpenSearch Serverless 暗号化ポリシー ──────────────────
# コレクションの保存データを AWS マネージドキーで暗号化する

resource "aws_opensearchserverless_security_policy" "encryption" {
  name = "${var.project}-${var.env}-enc"
  type = "encryption"
  policy = jsonencode({
    Rules = [{
      Resource     = ["collection/${var.project}-${var.env}-kb"]
      ResourceType = "collection"
    }]
    AWSOwnedKey = true
  })
}

# ── OpenSearch Serverless ネットワークポリシー ────────────
# パブリックアクセスを許可（Bedrock Knowledge Base からのアクセスに必要）

resource "aws_opensearchserverless_security_policy" "network" {
  name = "${var.project}-${var.env}-net"
  type = "network"
  policy = jsonencode([{
    Rules = [
      {
        Resource     = ["collection/${var.project}-${var.env}-kb"]
        ResourceType = "collection"
      },
      {
        Resource     = ["collection/${var.project}-${var.env}-kb"]
        ResourceType = "dashboard"
      }
    ]
    AllowFromPublic = true
  }])
}

# ── OpenSearch Serverless コレクション ────────────────────
# ベクトル検索用コレクション（埋め込みベクトルを格納・検索する）
# 最低 2 OCU 必要 → $0.24/OCU/時 × 2 OCU = $0.48/時

resource "aws_opensearchserverless_collection" "knowledge" {
  name = "${var.project}-${var.env}-kb"
  type = "VECTORSEARCH"
  tags = var.tags

  depends_on = [
    aws_opensearchserverless_security_policy.encryption,
    aws_opensearchserverless_security_policy.network,
  ]
}

# ── OpenSearch Serverless 起動待機 ───────────────────────
# コレクション作成後、ACTIVE になるまで約 2 分かかる
# この待機なしに Knowledge Base を作ると 404 エラーになる

resource "time_sleep" "wait_collection_active" {
  depends_on      = [aws_opensearchserverless_collection.knowledge]
  create_duration = "120s"
}

# ── OpenSearch インデックス自動作成 ────────────────────────
# Bedrock Knowledge Base が要求する固定スキーマのインデックスを作成する
# 実行タイミング:
#   コレクション起動(120s待機) → IAM権限付与(20s待機) → インデックス作成

resource "null_resource" "create_vector_index" {
  triggers = {
    collection_id = aws_opensearchserverless_collection.knowledge.id
    policy        = aws_opensearchserverless_access_policy.data.policy
  }

  provisioner "local-exec" {
    interpreter = ["python"]
    command     = abspath("${path.module}/scripts/create_index.py")
    environment = {
      COLLECTION_ENDPOINT = aws_opensearchserverless_collection.knowledge.collection_endpoint
      REGION              = var.region
      INDEX_NAME          = "bedrock-knowledge-base-default-index"
    }
  }

  depends_on = [
    time_sleep.wait_collection_active,
    time_sleep.wait_iam_propagation,
    aws_opensearchserverless_access_policy.data,
  ]
}

# ── OpenSearch Serverless データアクセスポリシー ──────────
# Knowledge Base IAM ロールがコレクション・インデックスを操作できるようにする
# ※ Principal に Knowledge Base ロール ARN を指定する必要があるため
#    iam.tf の aws_iam_role.knowledge_base と依存関係がある

resource "aws_opensearchserverless_access_policy" "data" {
  name = "${var.project}-${var.env}-data"
  type = "data"
  policy = jsonencode([{
    Rules = [
      {
        Resource     = ["collection/${var.project}-${var.env}-kb"]
        ResourceType = "collection"
        Permission = [
          "aoss:DescribeCollectionItems",
          "aoss:CreateCollectionItems",
          "aoss:UpdateCollectionItems",
        ]
      },
      {
        Resource     = ["index/${var.project}-${var.env}-kb/*"]
        ResourceType = "index"
        Permission = [
          "aoss:DescribeIndex",
          "aoss:CreateIndex",
          "aoss:UpdateIndex",
          "aoss:ReadDocument",
          "aoss:WriteDocument",
        ]
      }
    ]
    Principal = [
      aws_iam_role.knowledge_base.arn,
      local.admin_iam_role_arn,
    ]
  }])
}
