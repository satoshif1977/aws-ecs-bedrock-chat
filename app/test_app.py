"""
aws-ecs-bedrock-chat アプリ ユニットテスト

Streamlit・boto3 をモックし、AWS 接続なしでビジネスロジックを検証する。
"""

import json
import sys
from unittest.mock import MagicMock, patch

# Streamlit をモック（module-level の st.* 呼び出しを回避）
# chat_input と file_uploader は None を返すよう設定しモジュールレベルの分岐をスキップ
_mock_st = MagicMock()
_mock_st.chat_input.return_value = None
_mock_st.file_uploader.return_value = None
_mock_st.query_params = {}
sys.modules["streamlit"] = _mock_st

import app  # noqa: E402


# ── load_history テスト ───────────────────────────────────
class TestLoadHistory:
    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_正常系_履歴を取得できる(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client
        messages = [{"role": "user", "content": "こんにちは"}]
        mock_client.get_item.return_value = {
            "Item": {"messages": {"S": json.dumps(messages)}}
        }
        result = app.load_history("test-session")
        assert result == messages

    @patch("app.TABLE_NAME", "")
    def test_TABLE_NAME未設定の場合は空リストを返す(self):
        result = app.load_history("test-session")
        assert result == []

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_Itemなしの場合は空リストを返す(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client
        mock_client.get_item.return_value = {}
        result = app.load_history("test-session")
        assert result == []

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_例外発生時は空リストを返す(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client
        mock_client.get_item.side_effect = Exception("DynamoDB error")
        result = app.load_history("test-session")
        assert result == []


# ── save_history テスト ───────────────────────────────────
class TestSaveHistory:
    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_正常系_DynamoDBにputされる(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client
        messages = [{"role": "user", "content": "テスト"}]
        app.save_history("test-session", messages)
        mock_client.put_item.assert_called_once()
        call_kwargs = mock_client.put_item.call_args.kwargs
        assert call_kwargs["TableName"] == "test-table"
        assert call_kwargs["Item"]["session_id"]["S"] == "test-session"

    @patch("app.TABLE_NAME", "")
    def test_TABLE_NAME未設定の場合はスキップされる(self):
        # DynamoDB 呼び出しなしで正常終了することを確認
        app.save_history("test-session", [])


# ── invoke_bedrock_stream テスト ──────────────────────────
class TestInvokeBedrockStream:
    @patch("app.get_bedrock_client")
    def test_正常系_テキストをyieldする(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client
        chunk = {
            "type": "content_block_delta",
            "delta": {"type": "text_delta", "text": "Hello"},
        }
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [{"chunk": {"bytes": json.dumps(chunk).encode()}}]
        }
        messages = [{"role": "user", "content": "Hi"}]
        result = list(app.invoke_bedrock_stream(messages))
        assert result == ["Hello"]

    @patch("app.get_bedrock_client")
    def test_非textデルタはスキップされる(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client
        chunk = {"type": "message_start", "delta": {}}
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [{"chunk": {"bytes": json.dumps(chunk).encode()}}]
        }
        result = list(app.invoke_bedrock_stream([]))
        assert result == []


# ── invoke_rag テスト ─────────────────────────────────────
class TestInvokeRag:
    @patch("app.get_bedrock_agent_runtime_client")
    def test_正常系_回答と引用元を返す(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "テスト回答"},
            "citations": [
                {"retrievedReferences": [{"content": {"text": "引用テキスト"}}]}
            ],
        }
        answer, citations = app.invoke_rag("質問")
        assert answer == "テスト回答"
        assert citations == ["引用テキスト"]

    @patch("app.get_bedrock_agent_runtime_client")
    def test_引用元なしの場合は空リストを返す(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "回答"},
            "citations": [],
        }
        answer, citations = app.invoke_rag("質問")
        assert answer == "回答"
        assert citations == []


# ── build_multimodal_content テスト ──────────────────────
class TestBuildMultimodalContent:
    def test_正常系_画像とテキストのリストを返す(self):
        image_bytes = b"fake_image_bytes"
        result = app.build_multimodal_content(image_bytes, "image/png", "この画像は？")
        assert len(result) == 2
        assert result[0]["type"] == "image"
        assert result[0]["source"]["type"] == "base64"
        assert result[0]["source"]["media_type"] == "image/png"
        assert result[1]["type"] == "text"
        assert result[1]["text"] == "この画像は？"

    def test_base64エンコードが正しい(self):
        import base64

        image_bytes = b"test"
        result = app.build_multimodal_content(image_bytes, "image/jpeg", "test")
        expected_b64 = base64.standard_b64encode(b"test").decode("utf-8")
        assert result[0]["source"]["data"] == expected_b64

    def test_各メディアタイプで動作する(self):
        for media_type in ["image/jpeg", "image/png", "image/gif", "image/webp"]:
            result = app.build_multimodal_content(b"img", media_type, "質問")
            assert result[0]["source"]["media_type"] == media_type
