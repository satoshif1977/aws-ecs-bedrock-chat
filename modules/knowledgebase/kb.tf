# ── Bedrock Knowledge Base ─────────────────────────────────
# ドキュメントをベクトル化して OpenSearch Serverless に格納する
# 埋め込みモデル: Amazon Titan Embed Text v2

resource "aws_bedrockagent_knowledge_base" "main" {
  name     = "${var.project}-${var.env}-kb"
  role_arn = aws_iam_role.knowledge_base.arn

  knowledge_base_configuration {
    type = "VECTOR"
    vector_knowledge_base_configuration {
      embedding_model_arn = "arn:aws:bedrock:${var.region}::foundation-model/amazon.titan-embed-text-v2:0"
    }
  }

  storage_configuration {
    type = "OPENSEARCH_SERVERLESS"
    opensearch_serverless_configuration {
      collection_arn    = aws_opensearchserverless_collection.knowledge.arn
      vector_index_name = "bedrock-knowledge-base-default-index"
      field_mapping {
        vector_field   = "bedrock-knowledge-base-default-vector"
        text_field     = "AMAZON_BEDROCK_TEXT_CHUNK"
        metadata_field = "AMAZON_BEDROCK_METADATA"
      }
    }
  }

  tags = var.tags

  depends_on = [
    null_resource.create_vector_index,
    aws_opensearchserverless_access_policy.data,
    aws_iam_role_policy.kb_aoss,
    aws_iam_role_policy.kb_s3,
    aws_iam_role_policy.kb_bedrock_embed,
  ]
}

# ── Bedrock Knowledge Base データソース（S3）───────────────
# S3 バケット内のドキュメントを取り込む設定
# チャンキング: 固定サイズ 300 トークン / オーバーラップ 20%

resource "aws_bedrockagent_data_source" "s3" {
  knowledge_base_id = aws_bedrockagent_knowledge_base.main.id
  name              = "${var.project}-${var.env}-docs"

  data_source_configuration {
    type = "S3"
    s3_configuration {
      bucket_arn = aws_s3_bucket.knowledge.arn
    }
  }

  vector_ingestion_configuration {
    chunking_configuration {
      chunking_strategy = "FIXED_SIZE"
      fixed_size_chunking_configuration {
        max_tokens         = 300
        overlap_percentage = 20
      }
    }
  }
}
