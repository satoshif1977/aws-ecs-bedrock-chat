import json
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
REGION   = "ap-northeast-1"
MAX_TOKENS = 1024

# ── Bedrock クライアント（キャッシュ）────────────────────
@st.cache_resource
def get_bedrock_client():
    return boto3.client("bedrock-runtime", region_name=REGION)

# ── Bedrock に問い合わせる ────────────────────────────────
def invoke_bedrock(messages: list[dict]) -> str:
    client = get_bedrock_client()
    body = {
        "anthropic_version": "bedrock-2023-05-31",
        "max_tokens": MAX_TOKENS,
        "messages": messages,
    }
    response = client.invoke_model(
        modelId=MODEL_ID,
        body=json.dumps(body),
    )
    result = json.loads(response["body"].read())
    return result["content"][0]["text"]

# ── セッション初期化 ──────────────────────────────────────
if "messages" not in st.session_state:
    st.session_state.messages = []

# ── サイドバー ────────────────────────────────────────────
with st.sidebar:
    st.title("⚙️ 設定")
    st.write(f"**モデル:** Claude Haiku 4.5")
    st.write(f"**リージョン:** {REGION}")
    st.divider()
    if st.button("🗑️ 会話をリセット", use_container_width=True):
        st.session_state.messages = []
        st.rerun()
    st.divider()
    st.caption("aws-ecs-bedrock-chat / Phase 1")

# ── メイン画面 ────────────────────────────────────────────
st.title("🤖 Bedrock Chat")
st.caption("Amazon Bedrock（Claude Haiku 4.5）によるチャットアプリ")

# 過去メッセージを表示
for msg in st.session_state.messages:
    with st.chat_message(msg["role"]):
        st.write(msg["content"])

# ユーザー入力
if prompt := st.chat_input("メッセージを入力してください"):
    # ユーザーメッセージを追加・表示
    st.session_state.messages.append({"role": "user", "content": prompt})
    with st.chat_message("user"):
        st.write(prompt)

    # Bedrock に問い合わせ
    with st.chat_message("assistant"):
        with st.spinner("考え中..."):
            try:
                reply = invoke_bedrock(st.session_state.messages)
                st.write(reply)
                st.session_state.messages.append({"role": "assistant", "content": reply})
            except Exception as e:
                st.error(f"エラーが発生しました: {e}")
                st.stop()
