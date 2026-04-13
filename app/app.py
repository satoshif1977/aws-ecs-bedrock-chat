import json
import os
import time
import uuid

import boto3
import streamlit as st

# ── ページ設定 ────────────────────────────────────────────
st.set_page_config(
    page_title="Bedrock Chat",
    page_icon="🤖",
    layout="centered",
)

# ── 定数 ─────────────────────────────────────────────────
MODEL_ID          = "jp.anthropic.claude-haiku-4-5-20251001-v1:0"
REGION            = "ap-northeast-1"
MAX_TOKENS        = 1024
HISTORY_TTL_DAYS  = 7
TABLE_NAME        = os.environ.get("DYNAMODB_TABLE_NAME", "")
KNOWLEDGE_BASE_ID = os.environ.get("KNOWLEDGE_BASE_ID", "")

# ── AWS クライアント（キャッシュ）────────────────────────
@st.cache_resource
def get_bedrock_client():
    return boto3.client("bedrock-runtime", region_name=REGION)

@st.cache_resource
def get_dynamodb_client():
    return boto3.client("dynamodb", region_name=REGION)

@st.cache_resource
def get_bedrock_agent_runtime_client():
    return boto3.client("bedrock-agent-runtime", region_name=REGION)

# ── DynamoDB: 会話履歴を読み込む ──────────────────────────
def load_history(session_id: str) -> list[dict]:
    """DynamoDB からセッションの会話履歴を取得する。"""
    if not TABLE_NAME:
        return []
    try:
        response = get_dynamodb_client().get_item(
            TableName=TABLE_NAME,
            Key={"session_id": {"S": session_id}},
        )
        if "Item" in response:
            return json.loads(response["Item"]["messages"]["S"])
    except Exception:
        pass
    return []

# ── DynamoDB: 会話履歴を保存する ──────────────────────────
def save_history(session_id: str, messages: list[dict]) -> None:
    """DynamoDB にセッションの会話履歴を保存する（TTL: 7日）。"""
    if not TABLE_NAME:
        return
    try:
        ttl = int(time.time()) + 60 * 60 * 24 * HISTORY_TTL_DAYS
        get_dynamodb_client().put_item(
            TableName=TABLE_NAME,
            Item={
                "session_id": {"S": session_id},
                "messages":   {"S": json.dumps(messages, ensure_ascii=False)},
                "ttl":        {"N": str(ttl)},
            },
        )
    except Exception:
        pass

# ── Knowledge Base RAG 回答生成 ───────────────────────────
# RetrieveAndGenerate API でドキュメント検索 + 回答生成を一括実行
# ストリーミング非対応のため、回答テキストをそのまま返す
def invoke_rag(query: str) -> tuple[str, list[str]]:
    """Knowledge Base に問い合わせて RAG 回答と引用元を返す。"""
    response = get_bedrock_agent_runtime_client().retrieve_and_generate(
        input={"text": query},
        retrieveAndGenerateConfiguration={
            "type": "KNOWLEDGE_BASE",
            "knowledgeBaseConfiguration": {
                "knowledgeBaseId": KNOWLEDGE_BASE_ID,
                "modelArn": f"arn:aws:bedrock:{REGION}::foundation-model/anthropic.claude-haiku-4-5-20251001-v1:0",
            },
        },
    )
    answer = response["output"]["text"]
    citations = [
        ref["content"]["text"]
        for citation in response.get("citations", [])
        for ref in citation.get("retrievedReferences", [])
    ]
    return answer, citations

# ── Bedrock にストリーミングで問い合わせる ────────────────
# invoke_model_with_response_stream でチャンクを逐次 yield する
# st.write_stream() がこのジェネレータを受け取りリアルタイム表示する
def invoke_bedrock_stream(messages: list[dict]):
    body = {
        "anthropic_version": "bedrock-2023-05-31",
        "max_tokens": MAX_TOKENS,
        "messages": messages,
    }
    response = get_bedrock_client().invoke_model_with_response_stream(
        modelId=MODEL_ID,
        body=json.dumps(body),
    )
    for event in response["body"]:
        chunk = json.loads(event["chunk"]["bytes"])
        if chunk.get("type") == "content_block_delta":
            delta = chunk.get("delta", {})
            if delta.get("type") == "text_delta":
                yield delta.get("text", "")

# ── セッション ID 管理（URL クエリパラメータで永続化）────
# ブラウザをリロードしても同じ session_id が URL に残るため
# DynamoDB から過去の会話を復元できる
if "session_id" not in st.session_state:
    params = st.query_params
    if "session_id" in params:
        st.session_state.session_id = params["session_id"]
    else:
        st.session_state.session_id = str(uuid.uuid4())
        st.query_params["session_id"] = st.session_state.session_id

# ── 初回ロード時に DynamoDB から履歴を復元 ───────────────
if "messages" not in st.session_state:
    st.session_state.messages = load_history(st.session_state.session_id)

# ── サイドバー ────────────────────────────────────────────
with st.sidebar:
    st.title("⚙️ 設定")
    st.write(f"**モデル:** Claude Haiku 4.5")
    st.write(f"**リージョン:** {REGION}")
    st.divider()

    # RAG モードトグル（Knowledge Base が設定されている場合のみ表示）
    if KNOWLEDGE_BASE_ID:
        rag_mode = st.toggle(
            "📚 RAG モード（社内規定を参照）",
            value=False,
            help="オンにすると Knowledge Base から関連ドキュメントを検索して回答します",
        )
    else:
        rag_mode = False
        st.caption("※ KNOWLEDGE_BASE_ID 未設定のため RAG モード無効")

    st.divider()
    if st.button("🗑️ 会話をリセット", use_container_width=True):
        st.session_state.messages = []
        save_history(st.session_state.session_id, [])
        st.rerun()
    st.divider()
    st.caption("aws-ecs-bedrock-chat / Phase 9")

# ── メイン画面 ────────────────────────────────────────────
st.title("🤖 Bedrock Chat")
st.caption("Amazon Bedrock（Claude Haiku 4.5）によるチャットアプリ")

# 過去メッセージを表示
for msg in st.session_state.messages:
    with st.chat_message(msg["role"]):
        st.write(msg["content"])

# ユーザー入力
if prompt := st.chat_input("メッセージを入力してください"):
    st.session_state.messages.append({"role": "user", "content": prompt})
    with st.chat_message("user"):
        st.write(prompt)

    with st.chat_message("assistant"):
        try:
            if rag_mode:
                # RAG モード: Knowledge Base から関連チャンクを検索して回答生成
                with st.spinner("ナレッジベースを検索中..."):
                    reply, citations = invoke_rag(prompt)
                st.write(reply)
                # 引用元ドキュメントを折りたたみ表示
                if citations:
                    with st.expander("📎 参照元ドキュメント"):
                        for i, chunk in enumerate(citations, 1):
                            st.markdown(f"**[{i}]** {chunk}")
            else:
                # 通常モード: ストリーミングで回答生成
                reply = st.write_stream(invoke_bedrock_stream(st.session_state.messages))
            st.session_state.messages.append({"role": "assistant", "content": reply})
            save_history(st.session_state.session_id, st.session_state.messages)
        except Exception as e:
            st.error(f"エラーが発生しました: {e}")
            st.stop()
