"""
OpenSearch Serverless に Bedrock Knowledge Base 用インデックスを作成するスクリプト。
Terraform の null_resource local-exec / 手動実行 の両方に対応。

認証: requests-aws4auth（OpenSearch Serverless 公式推奨ライブラリ）
  pip install requests-aws4auth

必要な環境変数:
  COLLECTION_ENDPOINT - OpenSearch Serverless コレクションの HTTPS エンドポイント
  REGION              - AWS リージョン（例: ap-northeast-1）
  INDEX_NAME          - 作成するインデックス名
"""

import json
import os
import sys

import boto3
import requests
from requests_aws4auth import AWS4Auth

# ── 環境変数から設定を読み込む ─────────────────────────────────
ENDPOINT = os.environ["COLLECTION_ENDPOINT"].rstrip("/")
REGION = os.environ["REGION"]
INDEX_NAME = os.environ["INDEX_NAME"]

# ── インデックスのマッピング定義 ────────────────────────────────
# Bedrock Knowledge Base が要求する固定スキーマ
# - bedrock-knowledge-base-default-vector: 1024 次元ベクトル（Titan Embed v2 の出力次元）
# - AMAZON_BEDROCK_TEXT_CHUNK: チャンク化されたテキスト本文
# - AMAZON_BEDROCK_METADATA: ドキュメントメタデータ（インデックス対象外）
INDEX_BODY = {
    "settings": {
        "index": {
            "knn": True,
            "knn.algo_param.ef_search": 512,
        }
    },
    "mappings": {
        "properties": {
            "bedrock-knowledge-base-default-vector": {
                "type": "knn_vector",
                "dimension": 1024,
                "method": {
                    "name": "hnsw",
                    "engine": "faiss",
                    "parameters": {
                        "ef_construction": 512,
                        "m": 16,
                    },
                    "space_type": "l2",
                },
            },
            "AMAZON_BEDROCK_TEXT_CHUNK": {"type": "text"},
            "AMAZON_BEDROCK_METADATA": {"type": "text", "index": False},
        }
    },
}


def get_auth() -> AWS4Auth:
    """AWS4Auth オブジェクトを返す（requests-aws4auth 推奨方式）。"""
    session = boto3.Session()
    credentials = session.get_credentials().get_frozen_credentials()
    return AWS4Auth(
        credentials.access_key,
        credentials.secret_key,
        REGION,
        "aoss",
        session_token=credentials.token,
    )


def index_exists(auth: AWS4Auth) -> bool:
    """インデックスが既に存在するか確認する（冪等性のため）。"""
    url = f"{ENDPOINT}/{INDEX_NAME}"
    resp = requests.head(url, auth=auth, timeout=30)
    if resp.status_code == 200:
        return True
    if resp.status_code == 404:
        return False
    resp.raise_for_status()
    return False


def create_index(auth: AWS4Auth) -> None:
    """インデックスを作成する。"""
    url = f"{ENDPOINT}/{INDEX_NAME}"
    resp = requests.put(
        url,
        auth=auth,
        json=INDEX_BODY,
        headers={"Content-Type": "application/json"},
        timeout=30,
    )
    if resp.status_code not in (200, 201):
        print(f"エラー: HTTP {resp.status_code} - {resp.text}", file=sys.stderr)
        sys.exit(1)
    print(f"インデックス作成成功: {resp.json()}")


if __name__ == "__main__":
    print(f"対象エンドポイント: {ENDPOINT}")
    print(f"インデックス名: {INDEX_NAME}")

    auth = get_auth()

    if index_exists(auth):
        print("インデックスは既に存在します（スキップ）。")
        sys.exit(0)

    create_index(auth)
