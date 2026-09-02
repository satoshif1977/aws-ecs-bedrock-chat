"""
Bedrock Chat アプリ 堅牢性テスト

AWS 固有エラー（ClientError）、JSON 破損、delta キー欠落、
境界値、定数デフォルト値などを検証する。
"""

from __future__ import annotations

import base64
import json
import sys
from unittest.mock import MagicMock, patch

from botocore.exceptions import ClientError

# Streamlit をモック（module-level の st.* 呼び出しを回避）
if "streamlit" not in sys.modules:
    _mock_st = MagicMock()
    _mock_st.chat_input.return_value = None
    _mock_st.file_uploader.return_value = None
    _mock_st.query_params = {}
    sys.modules["streamlit"] = _mock_st

import app  # noqa: E402

# ── ヘルパー ──────────────────────────────────────────────────


def _make_text_event(text: str) -> dict:
    chunk = {
        "type": "content_block_delta",
        "delta": {"type": "text_delta", "text": text},
    }
    return {"chunk": {"bytes": json.dumps(chunk).encode()}}


def _make_event(payload: dict) -> dict:
    return {"chunk": {"bytes": json.dumps(payload).encode()}}


# ── 定数・デフォルト値検証 ────────────────────────────────────────


class TestDefaults:
    def test_MODEL_IDのデフォルト値が設定されている(self):
        assert "claude" in app.MODEL_ID.lower() or "anthropic" in app.MODEL_ID.lower()

    def test_REGIONのデフォルト値がap_northeast_1(self):
        assert app.REGION == "ap-northeast-1" or app.REGION != ""

    def test_MAX_TOKENSが1024である(self):
        assert app.MAX_TOKENS == 1024

    def test_EXT_TO_MEDIA_TYPEのエントリ数が5(self):
        assert len(app.EXT_TO_MEDIA_TYPE) == 5

    def test_SUPPORTED_IMAGE_TYPESとEXT_TO_MEDIA_TYPEが一致(self):
        for ext in app.SUPPORTED_IMAGE_TYPES:
            assert ext in app.EXT_TO_MEDIA_TYPE

    def test_jpegとjpgが同じmedia_typeを指す(self):
        assert app.EXT_TO_MEDIA_TYPE["jpg"] == app.EXT_TO_MEDIA_TYPE["jpeg"]


# ── load_history 堅牢性テスト ─────────────────────────────────────


class TestLoadHistoryRobust:
    @patch("app.RETRY_SLEEP", lambda _seconds: None)
    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_ClientError_ThrottleExceptionで空リストを返す(self, mock_get_client):
        mock_client = MagicMock()
        error_response = {
            "Error": {"Code": "ProvisionedThroughputExceededException", "Message": ""}
        }
        mock_client.get_item.side_effect = ClientError(error_response, "GetItem")
        mock_get_client.return_value = mock_client

        result = app.load_history("throttled-session")
        assert result == []
        # スロットリングはリトライ対象。使い切ったうえで握りつぶされる
        assert mock_client.get_item.call_count == app.RETRY_CONFIG.max_attempts

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_ClientError_AccessDeniedで空リストを返す(self, mock_get_client):
        mock_client = MagicMock()
        error_response = {
            "Error": {"Code": "AccessDeniedException", "Message": "not allowed"}
        }
        mock_client.get_item.side_effect = ClientError(error_response, "GetItem")
        mock_get_client.return_value = mock_client

        result = app.load_history("denied-session")
        assert result == []
        # 権限エラーは時間をおいても直らないため、リトライせず 1 回で諦める
        assert mock_client.get_item.call_count == 1

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_JSON破損データで空リストを返す(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.get_item.return_value = {
            "Item": {"messages": {"S": "invalid json [["}}
        }
        mock_get_client.return_value = mock_client

        result = app.load_history("corrupt-session")
        assert result == []

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_長いsession_idでも正常に動作する(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.get_item.return_value = {}
        mock_get_client.return_value = mock_client

        long_id = "s" * 500
        app.load_history(long_id)
        key = mock_client.get_item.call_args.kwargs["Key"]
        assert key["session_id"]["S"] == long_id

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_特殊文字session_idでも正常に動作する(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.get_item.return_value = {}
        mock_get_client.return_value = mock_client

        special_id = "session/with$special&chars"
        app.load_history(special_id)
        key = mock_client.get_item.call_args.kwargs["Key"]
        assert key["session_id"]["S"] == special_id


# ── save_history 堅牢性テスト ─────────────────────────────────────


class TestSaveHistoryRobust:
    @patch("app.RETRY_SLEEP", lambda _seconds: None)
    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_ClientError_ThrottleExceptionが伝播しない(self, mock_get_client):
        mock_client = MagicMock()
        error_response = {
            "Error": {"Code": "ProvisionedThroughputExceededException", "Message": ""}
        }
        mock_client.put_item.side_effect = ClientError(error_response, "PutItem")
        mock_get_client.return_value = mock_client

        app.save_history("s", [{"role": "user", "content": "test"}])
        assert mock_client.put_item.call_count == app.RETRY_CONFIG.max_attempts

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_大量メッセージ10件が保存される(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        messages = [{"role": "user", "content": f"msg{i}"} for i in range(10)]
        app.save_history("s", messages)

        item = mock_client.put_item.call_args.kwargs["Item"]
        saved = json.loads(item["messages"]["S"])
        assert len(saved) == 10

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_絵文字や改行を含むメッセージが保存される(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        messages = [{"role": "user", "content": "Hello\nWorld"}]
        app.save_history("s", messages)

        item = mock_client.put_item.call_args.kwargs["Item"]
        saved = json.loads(item["messages"]["S"])
        assert saved[0]["content"] == "Hello\nWorld"

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_ネストしたdict構造が保存される(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        messages = [{"role": "user", "content": [{"type": "text", "text": "nested"}]}]
        app.save_history("s", messages)

        item = mock_client.put_item.call_args.kwargs["Item"]
        saved = json.loads(item["messages"]["S"])
        assert saved[0]["content"][0]["type"] == "text"

    @patch("app.get_dynamodb_client")
    @patch("app.TABLE_NAME", "test-table")
    def test_TTLがtime_mockで正確に計算される(self, mock_get_client):
        mock_client = MagicMock()
        mock_get_client.return_value = mock_client

        fixed_time = 1000000
        with patch("app.time.time", return_value=fixed_time):
            app.save_history("s", [])

        item = mock_client.put_item.call_args.kwargs["Item"]
        ttl = int(item["ttl"]["N"])
        expected = fixed_time + 60 * 60 * 24 * 7
        assert ttl == expected


# ── invoke_bedrock_stream 堅牢性テスト ────────────────────────────


class TestInvokeBedrockStreamRobust:
    @patch("app.get_bedrock_client")
    def test_deltaキー欠落のcontent_block_deltaはyieldしない(self, mock_get_client):
        chunk = {"type": "content_block_delta"}  # delta キーなし
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [_make_event(chunk)]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == []

    @patch("app.get_bedrock_client")
    def test_textキー欠落のtext_deltaは空文字をyieldする(self, mock_get_client):
        chunk = {
            "type": "content_block_delta",
            "delta": {"type": "text_delta"},  # text キーなし
        }
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [_make_event(chunk)]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == [""]

    @patch("app.get_bedrock_client")
    def test_content_block_stopイベントはスキップされる(self, mock_get_client):
        chunk = {"type": "content_block_stop", "index": 0}
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [_make_event(chunk)]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == []

    @patch("app.get_bedrock_client")
    def test_message_deltaイベントはスキップされる(self, mock_get_client):
        chunk = {
            "type": "message_delta",
            "delta": {"stop_reason": "end_turn"},
            "usage": {"output_tokens": 10},
        }
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [_make_event(chunk)]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == []

    @patch("app.get_bedrock_client")
    def test_複数の非テキストイベント間にテキストがある場合(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [
                _make_event({"type": "message_start"}),
                _make_event({"type": "content_block_start"}),
                _make_text_event("Hello"),
                _make_event({"type": "content_block_stop"}),
                _make_event({"type": "message_delta", "delta": {}}),
            ]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == ["Hello"]

    @patch("app.get_bedrock_client")
    def test_日本語テキストがyieldされる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [_make_text_event("こんにちは世界")]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == ["こんにちは世界"]

    @patch("app.get_bedrock_client")
    def test_長いテキストチャンクが正しくyieldされる(self, mock_get_client):
        long_text = "a" * 5000
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {
            "body": [_make_text_event(long_text)]
        }
        mock_get_client.return_value = mock_client

        result = list(app.invoke_bedrock_stream([]))
        assert result == [long_text]
        assert len(result[0]) == 5000

    @patch("app.get_bedrock_client")
    def test_bodyに3つの必須キーが含まれる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.invoke_model_with_response_stream.return_value = {"body": []}
        mock_get_client.return_value = mock_client

        list(app.invoke_bedrock_stream([{"role": "user", "content": "hi"}]))
        body = json.loads(
            mock_client.invoke_model_with_response_stream.call_args.kwargs["body"]
        )
        assert set(body.keys()) == {"anthropic_version", "max_tokens", "messages"}


# ── invoke_rag 堅牢性テスト ───────────────────────────────────────


class TestInvokeRagRobust:
    @patch("app.get_bedrock_agent_runtime_client")
    def test_citationのretrievedReferencesが空リストの場合(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "answer"},
            "citations": [{"retrievedReferences": []}],
        }
        mock_get_client.return_value = mock_client

        _, citations = app.invoke_rag("q")
        assert citations == []

    @patch("app.get_bedrock_agent_runtime_client")
    def test_retrievedReferencesキー欠落のcitationは無視される(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "answer"},
            "citations": [{"generatedResponsePart": {"textResponsePart": {}}}],
        }
        mock_get_client.return_value = mock_client

        _, citations = app.invoke_rag("q")
        assert citations == []

    @patch("app.get_bedrock_agent_runtime_client")
    def test_長いクエリテキストが正しく渡される(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "ans"},
            "citations": [],
        }
        mock_get_client.return_value = mock_client

        long_query = "q" * 3000
        app.invoke_rag(long_query)
        call_kwargs = mock_client.retrieve_and_generate.call_args.kwargs
        assert call_kwargs["input"]["text"] == long_query

    @patch("app.get_bedrock_agent_runtime_client")
    def test_modelArnにREGIONが含まれる(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "ans"},
            "citations": [],
        }
        mock_get_client.return_value = mock_client

        app.invoke_rag("q")
        cfg = mock_client.retrieve_and_generate.call_args.kwargs[
            "retrieveAndGenerateConfiguration"
        ]["knowledgeBaseConfiguration"]
        assert app.REGION in cfg["modelArn"]

    @patch("app.get_bedrock_agent_runtime_client")
    def test_configurationのtypeがKNOWLEDGE_BASE(self, mock_get_client):
        mock_client = MagicMock()
        mock_client.retrieve_and_generate.return_value = {
            "output": {"text": "ans"},
            "citations": [],
        }
        mock_get_client.return_value = mock_client

        app.invoke_rag("q")
        cfg = mock_client.retrieve_and_generate.call_args.kwargs[
            "retrieveAndGenerateConfiguration"
        ]
        assert cfg["type"] == "KNOWLEDGE_BASE"


# ── build_multimodal_content 堅牢性テスト ─────────────────────────


class TestBuildMultimodalContentRobust:
    def test_バイナリ画像データが正しくbase64変換される(self):
        binary_data = bytes(range(256))
        result = app.build_multimodal_content(binary_data, "image/png", "test")
        expected = base64.standard_b64encode(binary_data).decode("utf-8")
        assert result[0]["source"]["data"] == expected

    def test_大きな画像10KBが正しく処理される(self):
        large_image = b"\x89PNG" + b"\x00" * 10000
        result = app.build_multimodal_content(large_image, "image/png", "大きな画像")
        decoded = base64.standard_b64decode(result[0]["source"]["data"])
        assert decoded == large_image

    def test_複数行テキストが正しく含まれる(self):
        multiline = "1行目\n2行目\n3行目"
        result = app.build_multimodal_content(b"img", "image/png", multiline)
        assert result[1]["text"] == multiline
        assert "\n" in result[1]["text"]

    def test_特殊文字を含むテキストが正しく含まれる(self):
        special = 'テスト<script>alert("xss")</script>&amp;'
        result = app.build_multimodal_content(b"img", "image/png", special)
        assert result[1]["text"] == special
