"""
health_check.py エッジケーステスト

print_section / get_running_tasks / health_check の
境界値・例外処理・出力形式を補完する。
"""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import health_check as hc

# ── print_section エッジケース ────────────────────────────────


class TestPrintSectionEdge:
    def test_セパレータが50文字の横線を含む(self, capsys):
        hc.print_section("タイトル")
        captured = capsys.readouterr()
        assert "─" * 50 in captured.out

    def test_特殊文字を含むタイトルが出力される(self, capsys):
        hc.print_section("ECS [ACTIVE] チェック")
        captured = capsys.readouterr()
        assert "ECS [ACTIVE] チェック" in captured.out

    def test_連続呼び出しで両タイトルが出力される(self, capsys):
        hc.print_section("セクション1")
        hc.print_section("セクション2")
        captured = capsys.readouterr()
        assert "セクション1" in captured.out
        assert "セクション2" in captured.out


# ── get_running_tasks エッジケース ────────────────────────────


class TestGetRunningTasksEdge:
    def test_containersが空タスクの場合空リストが返される(self):
        hc.ecs = MagicMock()
        hc.ecs.list_tasks.return_value = {"taskArns": ["arn:aws:ecs:r:id:task/c/task1"]}
        hc.ecs.describe_tasks.return_value = {
            "tasks": [
                {
                    "taskArn": "arn:aws:ecs:r:id:task/c/task1",
                    "lastStatus": "RUNNING",
                    "containers": [],
                }
            ]
        }
        result = hc.get_running_tasks("cluster", "svc")
        assert result[0]["containers"] == []

    def test_startedAtが文字列として返される(self):
        hc.ecs = MagicMock()
        hc.ecs.list_tasks.return_value = {"taskArns": ["arn:aws:ecs:r:id:task/c/task1"]}
        hc.ecs.describe_tasks.return_value = {
            "tasks": [
                {
                    "taskArn": "arn:aws:ecs:r:id:task/c/task1",
                    "lastStatus": "RUNNING",
                    "startedAt": "2026-01-01T00:00:00",
                    "containers": [],
                }
            ]
        }
        result = hc.get_running_tasks("cluster", "svc")
        assert isinstance(result[0]["startedAt"], str)


# ── health_check エッジケース ─────────────────────────────────


class TestHealthCheckEdge:
    def _running_task(self):
        return {
            "taskId": "task1",
            "lastStatus": "RUNNING",
            "healthStatus": "HEALTHY",
            "startedAt": "2026-01-01",
            "containers": [{"name": "app", "lastStatus": "RUNNING"}],
        }

    def _active_service(self, desired: int = 1, running: int = 1) -> dict:
        return {
            "serviceName": "svc",
            "status": "ACTIVE",
            "desiredCount": desired,
            "runningCount": running,
            "pendingCount": 0,
            "taskDefinition": "td:1",
            "launchType": "FARGATE",
        }

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_ValueErrorでFalseを返す(self, mock_info, mock_tasks):
        # ClientError がモックのため except 節が TypeError になる
        # → 実際の例外クラスでパッチしてから実行する
        class _FakeClientError(Exception):
            pass

        mock_info.side_effect = ValueError("サービスが見つかりません")
        with patch.object(hc, "ClientError", _FakeClientError):
            result = hc.health_check("cluster", "svc")
        assert result is False

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_desiredCount0でrunningCount0のときタスクなしでFalseを返す(
        self, mock_info, mock_tasks
    ):
        # running(0) >= desired(0) なのでサービスチェックは通過するが
        # タスクが0件なので False になる
        mock_info.return_value = self._active_service(desired=0, running=0)
        mock_tasks.return_value = []
        result = hc.health_check("cluster", "svc")
        assert result is False

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_複数タスクが全てRUNNINGのときTrueを返す(self, mock_info, mock_tasks):
        mock_info.return_value = self._active_service(desired=2, running=2)
        mock_tasks.return_value = [self._running_task(), self._running_task()]
        result = hc.health_check("cluster", "svc")
        assert result is True

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_戻り値がbool型である(self, mock_info, mock_tasks):
        mock_info.return_value = self._active_service()
        mock_tasks.return_value = [self._running_task()]
        result = hc.health_check("cluster", "svc")
        assert isinstance(result, bool)
