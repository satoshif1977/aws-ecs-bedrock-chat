# ── DynamoDB テーブル ───────────────────────────────────────
# チャット会話履歴の永続化ストア
# PAY_PER_REQUEST（オンデマンド）で学習用コストを最小化
# TTL 設定で古いセッションを自動削除

resource "aws_dynamodb_table" "this" {
  name         = "${var.project}-${var.env}-history"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "session_id"

  attribute {
    name = "session_id"
    type = "S"
  }

  # TTL: 期限切れアイテムを DynamoDB が自動削除（7日設定はアプリ側で計算）
  ttl {
    attribute_name = "ttl"
    enabled        = true
  }

  point_in_time_recovery {
    enabled = true
  }

  deletion_protection_enabled = true

  tags = var.tags
}
