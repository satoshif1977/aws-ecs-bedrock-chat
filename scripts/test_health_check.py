"""
scripts/health_check.py ユニットテスト

boto3 をモックして AWS 接続なしで各関数を検証する。
"""

from __future__ import annotations

import sys
from unittest.mock import MagicMock, patch

# ── boto3 をモック（モジュールレベルの boto3.client 呼び出しを回避） ──────
_mock_boto3 = MagicMock()
sys.modules["boto3"] = _mock_boto3
sys.modules["botocore"] = MagicMock()
sys.modules["botocore.exceptions"] = MagicMock()


# botocore.exceptions.ClientError を本物の例外クラスとして定義
class _ClientError(Exception):
    def __init__(self, error_response: dict, operation_name: str) -> None:
        self.response = error_response
        super().__init__(f"{operation_name}: {error_response}")


import botocore.exceptions  # noqa: E402

botocore.exceptions.ClientError = _ClientError  # type: ignore[attr-defined]

import scripts.health_check as hc  # noqa: E402

# ── get_service_info テスト ────────────────────────────────────────────────


class TestGetServiceInfo:
    def test_正常系_サービス情報を取得できる(self) -> None:
        hc.ecs = MagicMock()
        hc.ecs.describe_services.return_value = {
            "services": [
                {
                    "serviceName": "bedrock-chat-dev-service",
                    "status": "ACTIVE",
                    "desiredCount": 2,
                    "runningCount": 2,
                    "pendingCount": 0,
                    "taskDefinition": "arn:aws:ecs:ap-northeast-1:123:task-definition/mydef:5",
                    "launchType": "FARGATE",
                }
            ]
        }
        result = hc.get_service_info("my-cluster", "bedrock-chat-dev-service")
        assert result["serviceName"] == "bedrock-chat-dev-service"
        assert result["status"] == "ACTIVE"
        assert result["desiredCount"] == 2
        assert result["runningCount"] == 2
        assert result["taskDefinition"] == "mydef:5"

    def test_サービスが見つからない場合はValueError(self) -> None:
        hc.ecs = MagicMock()
        hc.ecs.describe_services.return_value = {"services": []}
        try:
            hc.get_service_info("my-cluster", "nonexistent")
            raise AssertionError("ValueError が発生すべき")
        except ValueError as e:
            assert "見つかりません" in str(e)

    def test_launchTypeがない場合はFARGATEをデフォルト値として返す(self) -> None:
        hc.ecs = MagicMock()
        hc.ecs.describe_services.return_value = {
            "services": [
                {
                    "serviceName": "svc",
                    "status": "ACTIVE",
                    "desiredCount": 1,
                    "runningCount": 1,
                    "pendingCount": 0,
                    "taskDefinition": "arn:aws:ecs:ap-northeast-1:123:task-definition/def:1",
                }
            ]
        }
        result = hc.get_service_info("cluster", "svc")
        assert result["launchType"] == "FARGATE"


# ── get_running_tasks テスト ──────────────────────────────────────────────


class TestGetRunningTasks:
    def test_正常系_実行中タスクを取得できる(self) -> None:
        hc.ecs = MagicMock()
        hc.ecs.list_tasks.return_value = {
            "taskArns": ["arn:aws:ecs:ap-northeast-1:123:task/cluster/abc123"]
        }
        hc.ecs.describe_tasks.return_value = {
            "tasks": [
                {
                    "taskArn": "arn:aws:ecs:ap-northeast-1:123:task/cluster/abc123",
                    "lastStatus": "RUNNING",
                    "healthStatus": "HEALTHY",
                    "startedAt": "2026-07-01T00:00:00",
                    "containers": [
                        {
                            "name": "app",
                            "lastStatus": "RUNNING",
                            "healthStatus": "HEALTHY",
                        }
                    ],
                }
            ]
        }
        result = hc.get_running_tasks("cluster", "svc")
        assert len(result) == 1
        assert result[0]["taskId"] == "abc123"
        assert result[0]["lastStatus"] == "RUNNING"
        assert result[0]["containers"][0]["name"] == "app"

    def test_タスクがない場合は空リストを返す(self) -> None:
        hc.ecs = MagicMock()
        hc.ecs.list_tasks.return_value = {"taskArns": []}
        result = hc.get_running_tasks("cluster", "svc")
        assert result == []

    def test_healthStatusがない場合はUNKNOWNを返す(self) -> None:
        hc.ecs = MagicMock()
        hc.ecs.list_tasks.return_value = {
            "taskArns": ["arn:aws:ecs:ap-northeast-1:123:task/cluster/xyz"]
        }
        hc.ecs.describe_tasks.return_value = {
            "tasks": [
                {
                    "taskArn": "arn:aws:ecs:ap-northeast-1:123:task/cluster/xyz",
                    "lastStatus": "RUNNING",
                    "startedAt": "2026-07-01T00:00:00",
                    "containers": [{"name": "app", "lastStatus": "RUNNING"}],
                }
            ]
        }
        result = hc.get_running_tasks("cluster", "svc")
        assert result[0]["healthStatus"] == "UNKNOWN"
        assert result[0]["containers"][0]["healthStatus"] == "UNKNOWN"


# ── health_check テスト ──────────────────────────────────────────────────


class TestHealthCheck:
    def test_正常系_全チェック通過でTrueを返す(self) -> None:
        with (
            patch.object(hc, "get_service_info") as mock_svc,
            patch.object(hc, "get_running_tasks") as mock_tasks,
        ):
            mock_svc.return_value = {
                "serviceName": "svc",
                "status": "ACTIVE",
                "desiredCount": 1,
                "runningCount": 1,
                "pendingCount": 0,
                "taskDefinition": "def:1",
                "launchType": "FARGATE",
            }
            mock_tasks.return_value = [
                {
                    "taskId": "abc123",
                    "lastStatus": "RUNNING",
                    "healthStatus": "HEALTHY",
                    "startedAt": "2026-07-01T00:00:00",
                    "containers": [
                        {
                            "name": "app",
                            "lastStatus": "RUNNING",
                            "healthStatus": "HEALTHY",
                        }
                    ],
                }
            ]
            result = hc.health_check("cluster", "svc")
        assert result is True

    def test_サービスがACTIVEでない場合はFalseを返す(self) -> None:
        with (
            patch.object(hc, "get_service_info") as mock_svc,
            patch.object(hc, "get_running_tasks") as mock_tasks,
        ):
            mock_svc.return_value = {
                "serviceName": "svc",
                "status": "INACTIVE",
                "desiredCount": 1,
                "runningCount": 0,
                "pendingCount": 0,
                "taskDefinition": "def:1",
                "launchType": "FARGATE",
            }
            mock_tasks.return_value = []
            result = hc.health_check("cluster", "svc")
        assert result is False

    def test_runningCountがdesiredCountを下回る場合はFalseを返す(self) -> None:
        with (
            patch.object(hc, "get_service_info") as mock_svc,
            patch.object(hc, "get_running_tasks") as mock_tasks,
        ):
            mock_svc.return_value = {
                "serviceName": "svc",
                "status": "ACTIVE",
                "desiredCount": 2,
                "runningCount": 1,
                "pendingCount": 1,
                "taskDefinition": "def:1",
                "launchType": "FARGATE",
            }
            mock_tasks.return_value = [
                {
                    "taskId": "abc",
                    "lastStatus": "RUNNING",
                    "healthStatus": "HEALTHY",
                    "startedAt": "2026-07-01",
                    "containers": [
                        {
                            "name": "app",
                            "lastStatus": "RUNNING",
                            "healthStatus": "HEALTHY",
                        }
                    ],
                }
            ]
            result = hc.health_check("cluster", "svc")
        assert result is False

    def test_実行中タスクがない場合はFalseを返す(self) -> None:
        with (
            patch.object(hc, "get_service_info") as mock_svc,
            patch.object(hc, "get_running_tasks") as mock_tasks,
        ):
            mock_svc.return_value = {
                "serviceName": "svc",
                "status": "ACTIVE",
                "desiredCount": 1,
                "runningCount": 1,
                "pendingCount": 0,
                "taskDefinition": "def:1",
                "launchType": "FARGATE",
            }
            mock_tasks.return_value = []
            result = hc.health_check("cluster", "svc")
        assert result is False

    def test_タスクのステータスがRUNNINGでない場合はFalseを返す(self) -> None:
        with (
            patch.object(hc, "get_service_info") as mock_svc,
            patch.object(hc, "get_running_tasks") as mock_tasks,
        ):
            mock_svc.return_value = {
                "serviceName": "svc",
                "status": "ACTIVE",
                "desiredCount": 1,
                "runningCount": 1,
                "pendingCount": 0,
                "taskDefinition": "def:1",
                "launchType": "FARGATE",
            }
            mock_tasks.return_value = [
                {
                    "taskId": "abc",
                    "lastStatus": "PENDING",
                    "healthStatus": "UNKNOWN",
                    "startedAt": "2026-07-01",
                    "containers": [
                        {
                            "name": "app",
                            "lastStatus": "PENDING",
                            "healthStatus": "UNKNOWN",
                        }
                    ],
                }
            ]
            result = hc.health_check("cluster", "svc")
        assert result is False

    def test_get_service_infoが例外を出した場合はFalseを返す(self) -> None:
        with (
            patch.object(hc, "ClientError", _ClientError),
            patch.object(hc, "get_service_info") as mock_svc,
        ):
            mock_svc.side_effect = _ClientError(
                {"Error": {"Code": "ClusterNotFoundException"}}, "DescribeServices"
            )
            result = hc.health_check("cluster", "svc")
        assert result is False


# ── print_section テスト ─────────────────────────────────────────────────


class TestPrintSection:
    def test_セクションタイトルを出力できる(self, capsys) -> None:
        hc.print_section("テストセクション")
        captured = capsys.readouterr()
        assert "テストセクション" in captured.out
        assert "─" in captured.out
