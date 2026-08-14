"""
health_check.py 高度テスト

get_service_info / get_running_tasks の詳細ケース、
health_check の出力・ClientError ハンドリング・main の sys.exit を補完する。
"""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import health_check as hc
import pytest
from botocore.exceptions import ClientError


# ── 各テスト後に hc.ecs をリセット（テスト間干渉を防止）────────
@pytest.fixture(autouse=True)
def reset_ecs_client():
    yield
    hc.ecs = MagicMock()


# ── get_service_info 詳細テスト ───────────────────────────────


class TestGetServiceInfoDetail:
    def _svc(self, **kwargs) -> dict:
        base = {
            "serviceName": "test-svc",
            "status": "ACTIVE",
            "desiredCount": 2,
            "runningCount": 2,
            "pendingCount": 0,
            "taskDefinition": (
                "arn:aws:ecs:ap-northeast-1:123456789012:task-definition/td:5"
            ),
            "launchType": "FARGATE",
        }
        base.update(kwargs)
        return base

    def test_taskDefinitionの末尾だけが返る(self):
        hc.ecs = MagicMock()
        hc.ecs.describe_services.return_value = {"services": [self._svc()]}

        result = hc.get_service_info("cluster", "svc")
        assert result["taskDefinition"] == "td:5"

    def test_desiredCountが正しく返る(self):
        hc.ecs = MagicMock()
        hc.ecs.describe_services.return_value = {
            "services": [self._svc(desiredCount=3, runningCount=3)]
        }
        result = hc.get_service_info("cluster", "svc")
        assert result["desiredCount"] == 3

    def test_launchTypeキーなしのときデフォルトFARGATEが返る(self):
        svc = self._svc()
        del svc["launchType"]
        hc.ecs = MagicMock()
        hc.ecs.describe_services.return_value = {"services": [svc]}

        result = hc.get_service_info("cluster", "svc")
        assert result["launchType"] == "FARGATE"

    def test_servicesが空のときValueErrorを投げる(self):
        hc.ecs = MagicMock()
        hc.ecs.describe_services.return_value = {"services": []}

        with pytest.raises(ValueError):
            hc.get_service_info("cluster", "svc")

    def test_statusがACTIVEのとき正しく返る(self):
        hc.ecs = MagicMock()
        hc.ecs.describe_services.return_value = {
            "services": [self._svc(status="ACTIVE")]
        }
        result = hc.get_service_info("cluster", "svc")
        assert result["status"] == "ACTIVE"

    def test_pendingCountが返る(self):
        hc.ecs = MagicMock()
        hc.ecs.describe_services.return_value = {
            "services": [self._svc(pendingCount=2)]
        }
        result = hc.get_service_info("cluster", "svc")
        assert result["pendingCount"] == 2

    def test_serviceNameが返る(self):
        hc.ecs = MagicMock()
        hc.ecs.describe_services.return_value = {
            "services": [self._svc(serviceName="my-svc")]
        }
        result = hc.get_service_info("cluster", "svc")
        assert result["serviceName"] == "my-svc"

    def test_runningCountが返る(self):
        hc.ecs = MagicMock()
        hc.ecs.describe_services.return_value = {
            "services": [self._svc(runningCount=5)]
        }
        result = hc.get_service_info("cluster", "svc")
        assert result["runningCount"] == 5


# ── get_running_tasks 詳細テスト ──────────────────────────────


class TestGetRunningTasksDetail:
    def _task(
        self,
        task_id: str = "task1",
        status: str = "RUNNING",
        health: str | None = "HEALTHY",
    ) -> dict:
        task: dict = {
            "taskArn": f"arn:aws:ecs:r:id:task/c/{task_id}",
            "lastStatus": status,
            "containers": [{"name": "app", "lastStatus": "RUNNING"}],
        }
        if health:
            task["healthStatus"] = health
        return task

    def test_複数タスクが全て返る(self):
        hc.ecs = MagicMock()
        hc.ecs.list_tasks.return_value = {
            "taskArns": [
                "arn:aws:ecs:r:id:task/c/task1",
                "arn:aws:ecs:r:id:task/c/task2",
            ]
        }
        hc.ecs.describe_tasks.return_value = {
            "tasks": [self._task("task1"), self._task("task2")]
        }
        result = hc.get_running_tasks("cluster", "svc")
        assert len(result) == 2

    def test_taskIdがARNの末尾部分である(self):
        hc.ecs = MagicMock()
        hc.ecs.list_tasks.return_value = {
            "taskArns": ["arn:aws:ecs:r:id:task/c/abc123"]
        }
        hc.ecs.describe_tasks.return_value = {"tasks": [self._task("abc123")]}

        result = hc.get_running_tasks("cluster", "svc")
        assert result[0]["taskId"] == "abc123"

    def test_healthStatusキーなしのとき_UNKNOWNが返る(self):
        hc.ecs = MagicMock()
        hc.ecs.list_tasks.return_value = {"taskArns": ["arn:aws:ecs:r:id:task/c/t1"]}
        task = self._task("t1", health=None)
        hc.ecs.describe_tasks.return_value = {"tasks": [task]}

        result = hc.get_running_tasks("cluster", "svc")
        assert result[0]["healthStatus"] == "UNKNOWN"

    def test_containers内のhealthStatusデフォルトがUNKNOWN(self):
        hc.ecs = MagicMock()
        hc.ecs.list_tasks.return_value = {"taskArns": ["arn:aws:ecs:r:id:task/c/t1"]}
        hc.ecs.describe_tasks.return_value = {
            "tasks": [
                {
                    "taskArn": "arn:aws:ecs:r:id:task/c/t1",
                    "lastStatus": "RUNNING",
                    "containers": [{"name": "app", "lastStatus": "RUNNING"}],
                }
            ]
        }
        result = hc.get_running_tasks("cluster", "svc")
        assert result[0]["containers"][0]["healthStatus"] == "UNKNOWN"

    def test_lastStatusが正しく返る(self):
        hc.ecs = MagicMock()
        hc.ecs.list_tasks.return_value = {"taskArns": ["arn:aws:ecs:r:id:task/c/t1"]}
        hc.ecs.describe_tasks.return_value = {
            "tasks": [self._task("t1", status="STOPPED")]
        }
        result = hc.get_running_tasks("cluster", "svc")
        assert result[0]["lastStatus"] == "STOPPED"

    def test_startedAtが文字列で返る(self):
        hc.ecs = MagicMock()
        hc.ecs.list_tasks.return_value = {"taskArns": ["arn:aws:ecs:r:id:task/c/t1"]}
        hc.ecs.describe_tasks.return_value = {
            "tasks": [
                {
                    "taskArn": "arn:aws:ecs:r:id:task/c/t1",
                    "lastStatus": "RUNNING",
                    "startedAt": "2026-01-01T00:00:00",
                    "containers": [],
                }
            ]
        }
        result = hc.get_running_tasks("cluster", "svc")
        assert isinstance(result[0]["startedAt"], str)


# ── health_check 出力・ClientError テスト ─────────────────────


class TestHealthCheckOutput:
    def _active_svc(self, desired=1, running=1, status="ACTIVE") -> dict:
        return {
            "serviceName": "svc",
            "status": status,
            "desiredCount": desired,
            "runningCount": running,
            "pendingCount": 0,
            "taskDefinition": "td:1",
            "launchType": "FARGATE",
        }

    def _running_task(self) -> dict:
        return {
            "taskId": "t1",
            "lastStatus": "RUNNING",
            "healthStatus": "HEALTHY",
            "startedAt": "2026-01-01",
            "containers": [{"name": "app", "lastStatus": "RUNNING"}],
        }

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_PASSが出力される(self, mock_info, mock_tasks, capsys):
        mock_info.return_value = self._active_svc()
        mock_tasks.return_value = [self._running_task()]

        hc.health_check("cluster", "svc")
        out = capsys.readouterr().out
        assert "PASS" in out

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_runningCount不足でFAILが出力される(self, mock_info, mock_tasks, capsys):
        mock_info.return_value = self._active_svc(desired=3, running=1)
        mock_tasks.return_value = [self._running_task()]

        hc.health_check("cluster", "svc")
        out = capsys.readouterr().out
        assert "FAIL" in out or "WARN" in out

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_ACTIVEでないサービスはFalseを返す(self, mock_info, mock_tasks):
        mock_info.return_value = self._active_svc(status="DRAINING")
        mock_tasks.return_value = []

        result = hc.health_check("cluster", "svc")
        assert result is False

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_runningCountがdesiredCount未満でFalseを返す(self, mock_info, mock_tasks):
        mock_info.return_value = self._active_svc(desired=3, running=1)
        mock_tasks.return_value = [self._running_task()]

        result = hc.health_check("cluster", "svc")
        assert result is False

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_タスクのlastStatusがRUNNING以外でFalseを返す(self, mock_info, mock_tasks):
        mock_info.return_value = self._active_svc()
        task = self._running_task()
        task["lastStatus"] = "STOPPED"
        mock_tasks.return_value = [task]

        result = hc.health_check("cluster", "svc")
        assert result is False

    def test_get_running_tasks側のClientErrorでFalseを返す(self):
        """ecs.list_tasks が ClientError を投げたとき health_check が False を返す"""
        hc.ecs = MagicMock()
        hc.ecs.list_tasks.side_effect = ClientError(
            {"Error": {"Code": "ListTasksException", "Message": "err"}},
            "ListTasks",
        )
        with patch.object(hc, "get_service_info", return_value=self._active_svc()):
            result = hc.health_check("cluster", "svc")
        assert result is False

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_serviceNameが出力に含まれる(self, mock_info, mock_tasks, capsys):
        mock_info.return_value = self._active_svc()
        mock_tasks.return_value = [self._running_task()]

        hc.health_check("cluster", "svc")
        out = capsys.readouterr().out
        assert "svc" in out

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_戻り値がbool型である(self, mock_info, mock_tasks):
        mock_info.return_value = self._active_svc()
        mock_tasks.return_value = [self._running_task()]

        result = hc.health_check("cluster", "svc")
        assert isinstance(result, bool)


# ── main テスト ───────────────────────────────────────────────


class TestMain:
    @patch("health_check.health_check")
    @patch("sys.exit")
    def test_passedがTrueのときsys_exit_0が呼ばれる(self, mock_exit, mock_hc):
        mock_hc.return_value = True
        hc.main()
        mock_exit.assert_called_once_with(0)

    @patch("health_check.health_check")
    @patch("sys.exit")
    def test_passedがFalseのときsys_exit_1が呼ばれる(self, mock_exit, mock_hc):
        mock_hc.return_value = False
        hc.main()
        mock_exit.assert_called_once_with(1)

    @patch("health_check.health_check")
    @patch("sys.exit")
    def test_mainが開始メッセージを出力する(self, mock_exit, mock_hc, capsys):
        mock_hc.return_value = True
        hc.main()
        out = capsys.readouterr().out
        assert "ECS" in out

    @patch("health_check.health_check")
    @patch("sys.exit")
    def test_mainがCLUSTER名を出力する(self, mock_exit, mock_hc, capsys):
        mock_hc.return_value = True
        with patch("health_check.CLUSTER", "test-cluster"):
            hc.main()
        out = capsys.readouterr().out
        assert "test-cluster" in out
