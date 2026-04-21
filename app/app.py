import base64
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
MODEL_ID = "jp.anthropic.claude-haiku-4-5-20251001-v1:0"
REGION = "ap-northeast-1"
MAX_TOKENS = 1024
HISTORY_TTL_DAYS = 7
TABLE_NAME = os.environ.get("DYNAMODB_TABLE_NAME", "")
KNOWLEDGE_BASE_ID = os.environ.get("KNOWLEDGE_BASE_ID", "")

SUPPORTED_IMAGE_TYPES = ["jpg", "jpeg", "png", "gif", "webp"]
EXT_TO_MEDIA_TYPE: dict[str, str] = {
    "jpg": "image/jpeg",
    "jpeg": "image/jpeg",
    "png": "image/png",
    "gif": "image/gif",
    "webp": "image/webp",
}


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
                "messages": {"S": json.dumps(messages, ensure_ascii=False)},
                "ttl": {"N": str(ttl)},
            },
        )
    except Exception:
        pass


# ── Knowledge Base RAG 回答生成 ───────────────────────────
def invoke_rag(query: str) -> tuple[str, list[str]]:
    """Knowledge Base に問い合わせて RAG 回答と引用元を返す。"""
    response = get_bedrock_agent_runtime_client().retrieve_and_generate(
        input={"text": query},
        retrieveAndGenerateConfiguration={
            "type": "KNOWLEDGE_BASE",
            "knowledgeBaseConfiguration": {
                "knowledgeBaseId": KNOWLEDGE_BASE_ID,
                "modelArn": f"arn:aws:bedrock:{REGION}::foundation-model/anthropic.claude-3-haiku-20240307-v1:0",
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
# messages にはテキストのみ / 画像+テキスト（マルチモーダル）どちらも対応
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


# ── 画像を base64 変換してマルチモーダルコンテンツを生成 ──
def build_multimodal_content(image_bytes: bytes, media_type: str, text: str) -> list[dict]:
    """画像 + テキストの Bedrock マルチモーダルコンテンツリストを返す。"""
    image_b64 = base64.standard_b64encode(image_bytes).decode("utf-8")
    return [
        {
            "type": "image",
            "source": {
                "type": "base64",
                "media_type": media_type,
                "data": image_b64,
            },
        },
        {"type": "text", "text": text},
    ]


# ── セッション ID 管理（URL クエリパラメータで永続化）────
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
    st.write("**モデル:** Claude Haiku 4.5")
    st.write(f"**リージョン:** {REGION}")
    st.divider()

    # 画像アップロード（マルチモーダル）
    st.write("**🖼️ 画像アップロード**")
    uploaded_file = st.file_uploader(
        "PNG / JPG / GIF / WebP",
        type=SUPPORTED_IMAGE_TYPES,
        help="画像をアップロードして「この画像について教えて」などと質問できます",
    )
    if uploaded_file:
        st.image(uploaded_file, caption=uploaded_file.name, use_container_width=True)

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
    st.caption("aws-ecs-bedrock-chat / Phase 10")

# ── メイン画面 ────────────────────────────────────────────
st.title("🤖 Bedrock Chat")
st.caption("Amazon Bedrock（Claude Haiku 4.5）によるチャットアプリ")

# 過去メッセージを表示（DynamoDB から復元したテキスト履歴）
for msg in st.session_state.messages:
    with st.chat_message(msg["role"]):
        st.write(msg["content"])

# ユーザー入力
if prompt := st.chat_input("メッセージを入力してください"):

    # ── ユーザーメッセージを画面に表示 ───────────────────
    with st.chat_message("user"):
        if uploaded_file:
            st.image(uploaded_file.getvalue(), use_container_width=True)
        st.write(prompt)

    # ── Bedrock 送信用メッセージを構築 ───────────────────
    # 画像あり → マルチモーダルコンテンツを最新ターンに追加
    # 画像なし → テキストのみ（従来動作）
    if uploaded_file:
        ext = uploaded_file.name.rsplit(".", 1)[-1].lower()
        media_type = EXT_TO_MEDIA_TYPE.get(ext, "image/png")
        multimodal_content = build_multimodal_content(
            uploaded_file.getvalue(), media_type, prompt
        )
        bedrock_messages = st.session_state.messages + [
            {"role": "user", "content": multimodal_content}
        ]
        # DynamoDB には画像を除いたテキストのみ保存（400KB 制限対策）
        display_text = f"[🖼️ {uploaded_file.name}] {prompt}"
        st.session_state.messages.append({"role": "user", "content": display_text})
    else:
        st.session_state.messages.append({"role": "user", "content": prompt})
        bedrock_messages = st.session_state.messages

    # ── アシスタント回答を生成 ────────────────────────────
    with st.chat_message("assistant"):
        try:
            if rag_mode:
                # RAG モード: Knowledge Base から関連チャンクを検索して回答生成
                with st.spinner("ナレッジベースを検索中..."):
                    reply, citations = invoke_rag(prompt)
                st.write(reply)
                if citations:
                    with st.expander("📎 参照元ドキュメント"):
                        for i, chunk in enumerate(citations, 1):
                            st.markdown(f"**[{i}]** {chunk}")
            else:
                # 通常モード: ストリーミングで回答生成（マルチモーダル対応）
                reply = st.write_stream(invoke_bedrock_stream(bedrock_messages))

            st.session_state.messages.append({"role": "assistant", "content": reply})
            save_history(st.session_state.session_id, st.session_state.messages)
        except Exception as e:
            st.error(f"エラーが発生しました: {e}")
            st.stop()
