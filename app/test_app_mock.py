"""
Bedrock Chat アプリ モック・追加テスト

invoke_bedrock_stream の複数チャンク・イベントタイプ別 yield 動作、
invoke_rag の citations 詳細、save_history の TTL 検証を補完する。
"""

from __future__ import annotations

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

# ── ヘルパー ──────────────────────────────────────────────────


def _make_stream_event(text: str) -> dict:
    """content_block_delta / text_delta イベントを生成する"""
    chunk = {
        "type": "content_block_delta",
        "delta": {"type": "text_delta", "text": text},
    }
    return {"chunk": {"bytes": json.dumps(chunk).encode()}}


# ── invoke_bedrock_stream 追加テスト ──────────────────────────


class TestInvokeBedrockStreamAdditional:
    @patch("app.get_bedrock_client")
    def test_複数チャンクが全てyieldされる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [_make_stream_event("Hello"), _make_stream_event(" World")]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == ["Hello", " World"]

    @patch("app.get_bedrock_client")
    def test_content_block_delta以外のイベントはyieldしない(self, mock_get_client):
        other_event = {
            "chunk": {"bytes": json.dumps({"type": "message_start"}).encode()}
        }
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [other_event]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == []

    @patch("app.get_bedrock_client")
    def test_text_delta以外のdeltaはyieldしない(self, mock_get_client):
        chunk = {
            "type": "content_block_delta",
            "delta": {"type": "input_json_delta", "partial_json": "{}"},
        }
        event = {"chunk": {"bytes": json.dumps(chunk).encode()}}
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {"body": [event]}
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == []

    @patch("app.get_bedrock_client")
    def test_MAX_TOKENSがbodyに含まれる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {"body": []}
        mock_get_client.return_value = mock_client

        list(app.invoke_bedrock_stream([]))
        body = json.loads(
            mock_client.invoke_model_with_response_stream.call_args.kwargs["body"]
        )
        assert body["max_tokens"] == app.MAX_TOKENS

    @patch("app.get_bedrock_client")
    def test_anthropic_versionがbodyに含まれる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {"body": []}
        mock_get_client.return_value = mock_client

        list(app.invoke_bedrock_stream([]))
        body = json.loads(
            mock_client.invoke_model_with_response_stream.call_args.kwargs["body"]
        )
        assert "anthropic_version" in body

    @patch("app.get_bedrock_client")
    def test_1チャンクだけyieldされる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [_make_stream_event("single")]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == ["single"]

    @patch("app.get_bedrock_client")
    def test_空テキストのdeltaは空文字をyieldする(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [_make_stream_event("")]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == [""]

    @patch("app.get_bedrock_client")
    def test_混在イベントのときtextのみyieldされる(self, mock_get_client):
        other = {"chunk": {"bytes": json.dumps({"type": "message_stop"}).encode()}}
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [
                _make_stream_event("A"),
                other,
                _make_stream_event("B"),
            ]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == ["A", "B"]


# ── invoke_rag 追加テスト ──────────────────────────────────────


class TestInvokeRagAdditional:
    @patch("app.get_bedrock_agent_runtime_client")
    def test_複数citations複数referencesが全て含まれる(self, mock_get_client):
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
                {"retrievedReferences": [{"content": {"text": "ref3"}}]},
            ],
        }
        mock_get_client.return_value = mock_client

        _, citations = app.invoke_rag("question")
        assert len(citations) == 3
        assert "ref1" in citations
        assert "ref3" in citations

    @patch("app.get_bedrock_agent_runtime_client")
    def test_answerが空文字のとき空文字が返る(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": ""},
            "citations": [],
        }
        mock_get_client.return_value = mock_client

        answer, _ = app.invoke_rag("q")
        assert answer == ""

    @patch("app.get_bedrock_agent_runtime_client")
    def test_KNOWLEDGE_BASE_IDがkbConfigに含まれる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "ok"},
            "citations": [],
        }
        mock_get_client.return_value = mock_client

        with patch("app.KNOWLEDGE_BASE_ID", "kb-test-id"):
            app.invoke_rag("q")
        cfg = mock_client.retrieve_and_generate.call_args.kwargs[
            "retrieveAndGenerateConfiguration"
        ]["knowledgeBaseConfiguration"]
        assert cfg["knowledgeBaseId"] == "kb-test-id"

    @patch("app.get_bedrock_agent_runtime_client")
    def test_citationsキーなしのとき空リストが返る(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {"output": {"text": "answer"}}
        mock_get_client.return_value = mock_client

        _, citations = app.invoke_rag("q")
        assert citations == []

    @patch("app.get_bedrock_agent_runtime_client")
    def test_answerがstrである(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "text answer"},
            "citations": [],
        }
        mock_get_client.return_value = mock_client

        answer, _ = app.invoke_rag("q")
        assert isinstance(answer, str)

    @patch("app.get_bedrock_agent_runtime_client")
    def test_citations1件のとき1件が返る(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "ans"},
            "citations": [
                {"retrievedReferences": [{"content": {"text": "single ref"}}]}
            ],
        }
        mock_get_client.return_value = mock_client

        _, citations = app.invoke_rag("q")
        assert len(citations) == 1
        assert citations[0] == "single ref"


# ── save_history 追加テスト ────────────────────────────────────


class TestSaveHistoryAdditional:
    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "tbl")
    def test_TTLが現在時刻より未来である(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        before = int(time.time())
        app.save_history("s", [])

        item = mock_client.put_item.call_args.kwargs["Item"]
        ttl = int(item["ttl"]["N"])
        assert ttl > before

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "tbl")
    def test_TTLが7日後を超えない(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        app.save_history("s", [])
        max_ttl = int(time.time()) + 60 * 60 * 24 * 7 + 5  # 5秒の余裕

        item = mock_client.put_item.call_args.kwargs["Item"]
        ttl = int(item["ttl"]["N"])
        assert ttl <= max_ttl

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "tbl")
    def test_複数メッセージがJSONとして保存される(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        messages = [
            {"role": "user", "content": "こんにちは"},
            {"role": "assistant", "content": "どうぞ"},
        ]
        app.save_history("s", messages)

        item = mock_client.put_item.call_args.kwargs["Item"]
        saved = json.loads(item["messages"]["S"])
        assert saved == messages

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "tbl")
    def test_DynamoDB例外でも伝播しない(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.put_item.side_effect = Exception("DynamoDB error")
        mock_get_client.return_value = mock_client

        app.save_history("s", [])  # 例外が外に出ないこと

    @patch("app.TABLE_NAME", "")
    def test_TABLE_NAME空のとき何も保存しない(self):
        with patch("app.get_dynamodb_client") as mock_get:
            app.save_history("s", [])
            mock_get.assert_not_called()


# ── build_multimodal_content 追加テスト ───────────────────────


class TestBuildMultimodalContentAdditional:
    def test_結果がリストである(self):
        result = app.build_multimodal_content(b"data", "image/png", "text")
        assert isinstance(result, list)

    def test_結果の要素数が2である(self):
        result = app.build_multimodal_content(b"data", "image/png", "text")
        assert len(result) == 2

    def test_result_0のtypeがimage(self):
        result = app.build_multimodal_content(b"data", "image/png", "text")
        assert result[0]["type"] == "image"

    def test_result_1のtypeがtext(self):
        result = app.build_multimodal_content(b"data", "image/png", "text")
        assert result[1]["type"] == "text"

    def test_空バイトでも動作する(self):
        result = app.build_multimodal_content(b"", "image/png", "text")
        expected = base64.standard_b64encode(b"").decode()
        assert result[0]["source"]["data"] == expected

    def test_gifのmedia_typeが正しく設定される(self):
        result = app.build_multimodal_content(b"data", "image/gif", "text")
        assert result[0]["source"]["media_type"] == "image/gif"

    def test_Unicodeテキストが正しく保存される(self):
        result = app.build_multimodal_content(b"data", "image/png", "日本語テキスト")
        assert result[1]["text"] == "日本語テキスト"

    def test_jpegのmedia_typeが正しく設定される(self):
        result = app.build_multimodal_content(b"data", "image/jpeg", "text")
        assert result[0]["source"]["media_type"] == "image/jpeg"
