"""
Bedrock Chat アプリ エッジケーステスト

定数・load_history / save_history / invoke_bedrock_stream /
invoke_rag / build_multimodal_content のエッジケース・境界値を補完する。
"""

from __future__ import annotations

import base64
import json
import sys
from unittest.mock import MagicMock, patch

# Streamlit をモック（module-level の st.* 呼び出しを回避）
if "streamlit" not in sys.modules:
    _mock_st = MagicMock()
    _mock_st.chat_input.return_value = None
    _mock_st.file_uploader.return_value = None
    _mock_st.query_params = {}
    sys.modules["streamlit"] = _mock_st

import app  # noqa: E402

# ── 定数検証 ──────────────────────────────────────────────────


class TestConstants:
    def test_MAX_TOKENSが正の整数である(self):
        assert isinstance(app.MAX_TOKENS, int)
        assert app.MAX_TOKENS > 0

    def test_HISTORY_TTL_DAYSが7である(self):
        assert app.HISTORY_TTL_DAYS == 7

    def test_EXT_TO_MEDIA_TYPEにjpgが含まれる(self):
        assert "jpg" in app.EXT_TO_MEDIA_TYPE
        assert app.EXT_TO_MEDIA_TYPE["jpg"] == "image/jpeg"

    def test_EXT_TO_MEDIA_TYPEにpngが含まれる(self):
        assert "png" in app.EXT_TO_MEDIA_TYPE
        assert app.EXT_TO_MEDIA_TYPE["png"] == "image/png"

    def test_EXT_TO_MEDIA_TYPEにgifとwebpが含まれる(self):
        assert app.EXT_TO_MEDIA_TYPE["gif"] == "image/gif"
        assert app.EXT_TO_MEDIA_TYPE["webp"] == "image/webp"

    def test_SUPPORTED_IMAGE_TYPESが空でない(self):
        assert len(app.SUPPORTED_IMAGE_TYPES) > 0

    def test_SUPPORTED_IMAGE_TYPESにpngとjpgが含まれる(self):
        assert "png" in app.SUPPORTED_IMAGE_TYPES
        assert "jpg" in app.SUPPORTED_IMAGE_TYPES


# ── load_history エッジケース ─────────────────────────────────


class TestLoadHistoryEdge:
    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_空のメッセージリストを復元できる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.get_item.return_value = {"Item": {"messages": {"S": "[]"}}}
        mock_get_client.return_value = mock_client

        result = app.load_history("empty-session")
        assert result == []

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "my-table")
    def test_TableNameが正しく渡される(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.get_item.return_value = {}
        mock_get_client.return_value = mock_client

        app.load_history("sess")
        assert mock_client.get_item.call_args.kwargs["TableName"] == "my-table"

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_get_itemが1回だけ呼ばれる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.get_item.return_value = {}
        mock_get_client.return_value = mock_client

        app.load_history("sess")
        mock_client.get_item.assert_called_once()


# ── save_history エッジケース ─────────────────────────────────


class TestSaveHistoryEdge:
    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_put_itemが1回だけ呼ばれる(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        app.save_history("sid", [])
        mock_client.put_item.assert_called_once()

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_session_idがSフォーマットで保存される(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        app.save_history("my-session", [])
        item = mock_client.put_item.call_args.kwargs["Item"]
        assert item["session_id"] == {"S": "my-session"}

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_空メッセージリストが保存できる(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        app.save_history("sid", [])
        item = mock_client.put_item.call_args.kwargs["Item"]
        assert json.loads(item["messages"]["S"]) == []


# ── invoke_bedrock_stream エッジケース ────────────────────────


class TestInvokeBedrockStreamEdge:
    @patch("app.get_bedrock_client")
    def test_MODEL_IDがmodelIdに渡される(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {"body": []}
        mock_get_client.return_value = mock_client

        list(app.invoke_bedrock_stream([{"role": "user", "content": "test"}]))
        call_kwargs = mock_client.invoke_model_with_response_stream.call_args.kwargs
        assert call_kwargs["modelId"] == app.MODEL_ID

    @patch("app.get_bedrock_client")
    def test_bodyが有効なJSONである(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {"body": []}
        mock_get_client.return_value = mock_client

        list(app.invoke_bedrock_stream([]))
        raw_body = mock_client.invoke_model_with_response_stream.call_args.kwargs[
            "body"
        ]
        parsed = json.loads(raw_body)
        assert isinstance(parsed, dict)

    @patch("app.get_bedrock_client")
    def test_messages引数がbodyのmessagesに含まれる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {"body": []}
        mock_get_client.return_value = mock_client

        messages = [{"role": "user", "content": "こんにちは"}]
        list(app.invoke_bedrock_stream(messages))
        body = json.loads(
            mock_client.invoke_model_with_response_stream.call_args.kwargs["body"]
        )
        assert body["messages"] == messages

    @patch("app.get_bedrock_client")
    def test_空のbody応答は何もyieldしない(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {"body": []}
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == []


# ── invoke_rag エッジケース ───────────────────────────────────


class TestInvokeRagEdge:
    @patch("app.get_bedrock_agent_runtime_client")
    def test_queryがinputのtextに含まれる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "回答"},
            "citations": [],
        }
        mock_get_client.return_value = mock_client

        app.invoke_rag("テスト質問")
        call_kwargs = mock_client.retrieve_and_generate.call_args.kwargs
        assert call_kwargs["input"]["text"] == "テスト質問"

    @patch("app.get_bedrock_agent_runtime_client")
    def test_retrieve_and_generateが1回だけ呼ばれる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "ok"},
            "citations": [],
        }
        mock_get_client.return_value = mock_client

        app.invoke_rag("question")
        mock_client.retrieve_and_generate.assert_called_once()

    @patch("app.get_bedrock_agent_runtime_client")
    def test_戻り値はtupleである(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "answer"},
            "citations": [],
        }
        mock_get_client.return_value = mock_client

        result = app.invoke_rag("query")
        assert isinstance(result, tuple)
        assert len(result) == 2


# ── build_multimodal_content エッジケース ─────────────────────


class TestBuildMultimodalContentEdge:
    def test_result_0のsourceにdataキーがある(self):
        result = app.build_multimodal_content(b"data", "image/png", "text")
        assert "data" in result[0]["source"]

    def test_大きな画像バイトでも動作する(self):
        large_bytes = b"x" * 1000
        result = app.build_multimodal_content(large_bytes, "image/png", "説明")
        expected = base64.standard_b64encode(large_bytes).decode("utf-8")
        assert result[0]["source"]["data"] == expected

    def test_空のテキストでも動作する(self):
        result = app.build_multimodal_content(b"img", "image/jpeg", "")
        assert result[1]["text"] == ""

    def test_webpのmedia_typeが正しく設定される(self):
        result = app.build_multimodal_content(b"img", "image/webp", "test")
        assert result[0]["source"]["media_type"] == "image/webp"

    def test_sourceのtypeがbase64である(self):
        result = app.build_multimodal_content(b"data", "image/png", "txt")
        assert result[0]["source"]["type"] == "base64"
