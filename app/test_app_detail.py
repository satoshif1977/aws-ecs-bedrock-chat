"""
Bedrock Chat アプリ 詳細ユニットテスト

load_history / save_history / invoke_bedrock_stream /
invoke_rag / build_multimodal_content の境界値・構造を検証する。
"""

import base64
import json
import sys
import time
from unittest.mock import MagicMock, patch

# Streamlit をモック（module-level の st.* 呼び出しを回避）
if "streamlit" not in sys.modules:
    _mock_st = MagicMock()
    _mock_st.chat_input.return_value = None
    _mock_st.file_uploader.return_value = None
    _mock_st.query_params = {}
    sys.modules["streamlit"] = _mock_st

import app  # noqa: E402

# ── load_history ──────────────────────────────────────────


class TestLoadHistoryDetail:
    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_複数メッセージが正しく復元される(self, mock_get_client):
        messages = [
            {"role": "user", "content": "こんにちは"},
            {"role": "assistant", "content": "はい、何でしょう"},
        ]
        mock_client = MagicMock()
        mock_client.get_item.return_value = {
            "Item": {"messages": {"S": json.dumps(messages)}}
        }
        mock_get_client.return_value = mock_client

        result = app.load_history("session-abc")
        assert result == messages
        assert len(result) == 2

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_session_idがKeyに含まれる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.get_item.return_value = {}
        mock_get_client.return_value = mock_client

        app.load_history("my-session-id")
        call_key = mock_client.get_item.call_args.kwargs["Key"]
        assert call_key["session_id"]["S"] == "my-session-id"

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_Itemなしは空リストを返す(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.get_item.return_value = {}  # "Item" キーなし
        mock_get_client.return_value = mock_client

        result = app.load_history("no-item-session")
        assert result == []

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_DynamoDB例外は空リストを返す(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.get_item.side_effect = Exception("DynamoDB error")
        mock_get_client.return_value = mock_client

        result = app.load_history("error-session")
        assert result == []


# ── save_history ──────────────────────────────────────────


class TestSaveHistoryDetail:
    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_TTLが約7日後である(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        before = int(time.time()) + 60 * 60 * 24 * 7
        app.save_history("sid", [{"role": "user", "content": "hi"}])
        after = int(time.time()) + 60 * 60 * 24 * 7

        item = mock_client.put_item.call_args.kwargs["Item"]
        ttl_val = int(item["ttl"]["N"])
        assert before - 5 <= ttl_val <= after + 5

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_messagesがJSON文字列として保存される(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        messages = [{"role": "assistant", "content": "テスト回答"}]
        app.save_history("sid", messages)

        item = mock_client.put_item.call_args.kwargs["Item"]
        parsed = json.loads(item["messages"]["S"])
        assert parsed == messages

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_日本語メッセージがUTF8で保存される(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        messages = [{"role": "user", "content": "日本語テスト"}]
        app.save_history("sid", messages)

        item = mock_client.put_item.call_args.kwargs["Item"]
        # ensure_ascii=False なので日本語がそのまま含まれる
        assert "日本語テスト" in item["messages"]["S"]

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_Itemに3つのフィールドがある(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        app.save_history("sid", [])
        item = mock_client.put_item.call_args.kwargs["Item"]
        assert set(item.keys()) == {"session_id", "messages", "ttl"}


# ── invoke_bedrock_stream ──────────────────────────────────


class TestInvokeBedrockStreamDetail:
    def _make_chunk(self, text: str) -> dict:
        payload = {
            "type": "content_block_delta",
            "delta": {"type": "text_delta", "text": text},
        }
        return {"chunk": {"bytes": json.dumps(payload).encode()}}

    def _make_non_delta_chunk(self) -> dict:
        payload = {"type": "message_start"}
        return {"chunk": {"bytes": json.dumps(payload).encode()}}

    @patch("app.get_bedrock_client")
    def test_複数チャンクが順番に生成される(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [
                self._make_chunk("Hello"),
                self._make_chunk(", "),
                self._make_chunk("world"),
            ]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([{"role": "user", "content": "hi"}]))
        assert result == ["Hello", ", ", "world"]

    @patch("app.get_bedrock_client")
    def test_content_block_delta以外は無視される(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [
                self._make_non_delta_chunk(),
                self._make_chunk("OK"),
            ]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([{"role": "user", "content": "hi"}]))
        assert result == ["OK"]

    @patch("app.get_bedrock_client")
    def test_Bedrockへ送るbodyにmax_tokensが含まれる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {"body": []}
        mock_get_client.return_value = mock_client

        list(app.invoke_bedrock_stream([{"role": "user", "content": "test"}]))
        call_body = json.loads(
            mock_client.invoke_model_with_response_stream.call_args.kwargs["body"]
        )
        assert "max_tokens" in call_body
        assert call_body["max_tokens"] == app.MAX_TOKENS

    @patch("app.get_bedrock_client")
    def test_Bedrockへ送るbodyにanthropic_versionが含まれる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {"body": []}
        mock_get_client.return_value = mock_client

        messages = [{"role": "user", "content": "test"}]
        list(app.invoke_bedrock_stream(messages))
        call_body = json.loads(
            mock_client.invoke_model_with_response_stream.call_args.kwargs["body"]
        )
        assert call_body["anthropic_version"] == "bedrock-2023-05-31"
        assert call_body["messages"] == messages


# ── invoke_rag ────────────────────────────────────────────


class TestInvokeRagDetail:
    @patch("app.get_bedrock_agent_runtime_client")
    def test_answerが正しく取得される(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "RAG の回答です"},
            "citations": [],
        }
        mock_get_client.return_value = mock_client

        answer, citations = app.invoke_rag("質問テキスト")
        assert answer == "RAG の回答です"
        assert citations == []

    @patch("app.get_bedrock_agent_runtime_client")
    def test_複数citationsが平坦化される(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "answer"},
            "citations": [
                {
                    "retrievedReferences": [
                        {"content": {"text": "ref1"}},
                        {"content": {"text": "ref2"}},
                    ]
                },
                {
                    "retrievedReferences": [
                        {"content": {"text": "ref3"}},
                    ]
                },
            ],
        }
        mock_get_client.return_value = mock_client

        _, citations = app.invoke_rag("query")
        assert citations == ["ref1", "ref2", "ref3"]

    @patch("app.get_bedrock_agent_runtime_client")
    def test_citationsキーなしは空リスト(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "answer"},
        }
        mock_get_client.return_value = mock_client

        _, citations = app.invoke_rag("query")
        assert citations == []


# ── build_multimodal_content ──────────────────────────────


class TestBuildMultimodalContentDetail:
    def test_リストの要素数は2(self):
        result = app.build_multimodal_content(b"img", "image/png", "説明して")
        assert len(result) == 2

    def test_最初の要素はimage型(self):
        result = app.build_multimodal_content(b"img", "image/png", "テキスト")
        assert result[0]["type"] == "image"

    def test_2番目の要素はtext型でテキストが含まれる(self):
        result = app.build_multimodal_content(b"img", "image/jpeg", "この画像は？")
        assert result[1]["type"] == "text"
        assert result[1]["text"] == "この画像は？"

    def test_imageのmedia_typeが正しく設定される(self):
        result = app.build_multimodal_content(b"data", "image/gif", "txt")
        source = result[0]["source"]
        assert source["media_type"] == "image/gif"
        assert source["type"] == "base64"

    def test_base64エンコードが正しい(self):
        image_bytes = b"test-image-data"
        result = app.build_multimodal_content(image_bytes, "image/png", "text")
        expected_b64 = base64.standard_b64encode(image_bytes).decode("utf-8")
        assert result[0]["source"]["data"] == expected_b64

    def test_空のbytesでも動作する(self):
        result = app.build_multimodal_content(b"", "image/png", "empty")
        assert result[0]["source"]["data"] == ""
