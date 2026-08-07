"""
ECS ヘルスチェックスクリプト 詳細ユニットテスト

get_service_info / get_running_tasks / health_check の
境界値・フィールド変換・エラーハンドリングを検証する。
"""

from unittest.mock import patch

import pytest

# ── get_service_info ──────────────────────────────────────


class TestGetServiceInfoDetail:
    @patch("health_check.ecs")
    def test_taskDefinitionがARNの末尾に変換される(self, mock_ecs):
        mock_ecs.describe_services.return_value = {
            "services": [
                {
                    "serviceName": "my-service",
                    "status": "ACTIVE",
                    "desiredCount": 1,
                    "runningCount": 1,
                    "pendingCount": 0,
                    "taskDefinition": "arn:aws:ecs:ap-northeast-1:123456789012:task-definition/my-task:5",
                    "launchType": "FARGATE",
                }
            ]
        }

        from health_check import get_service_info

        info = get_service_info("cluster", "service")
        assert info["taskDefinition"] == "my-task:5"

    @patch("health_check.ecs")
    def test_pendingCountがresultに含まれる(self, mock_ecs):
        mock_ecs.describe_services.return_value = {
            "services": [
                {
                    "serviceName": "svc",
                    "status": "ACTIVE",
                    "desiredCount": 2,
                    "runningCount": 1,
                    "pendingCount": 1,
                    "taskDefinition": "arn:aws:ecs:region:id:task-definition/td:1",
                }
            ]
        }

        from health_check import get_service_info

        info = get_service_info("cluster", "service")
        assert info["pendingCount"] == 1

    @patch("health_check.ecs")
    def test_servicesが空のときValueErrorが発生する(self, mock_ecs):
        mock_ecs.describe_services.return_value = {"services": []}

        from health_check import get_service_info

        with pytest.raises(ValueError, match="サービスが見つかりません"):
            get_service_info("cluster", "not-exist-service")

    @patch("health_check.ecs")
    def test_launchTypeが省略された場合FARGATEがデフォルト(self, mock_ecs):
        mock_ecs.describe_services.return_value = {
            "services": [
                {
                    "serviceName": "svc",
                    "status": "ACTIVE",
                    "desiredCount": 1,
                    "runningCount": 1,
                    "pendingCount": 0,
                    "taskDefinition": "arn:aws:ecs:region:id:task-definition/td:1",
                    # launchType なし
                }
            ]
        }

        from health_check import get_service_info

        info = get_service_info("cluster", "service")
        assert info["launchType"] == "FARGATE"


# ── get_running_tasks ─────────────────────────────────────


class TestGetRunningTasksDetail:
    @patch("health_check.ecs")
    def test_taskIdがARNの末尾に変換される(self, mock_ecs):
        mock_ecs.list_tasks.return_value = {
            "taskArns": ["arn:aws:ecs:region:id:task/cluster/abc123def456"]
        }
        mock_ecs.describe_tasks.return_value = {
            "tasks": [
                {
                    "taskArn": "arn:aws:ecs:region:id:task/cluster/abc123def456",
                    "lastStatus": "RUNNING",
                    "containers": [],
                }
            ]
        }

        from health_check import get_running_tasks

        tasks = get_running_tasks("cluster", "service")
        assert tasks[0]["taskId"] == "abc123def456"

    @patch("health_check.ecs")
    def test_taskArnsが空のとき空リストを返す(self, mock_ecs):
        mock_ecs.list_tasks.return_value = {"taskArns": []}

        from health_check import get_running_tasks

        result = get_running_tasks("cluster", "service")
        assert result == []
        mock_ecs.describe_tasks.assert_not_called()

    @patch("health_check.ecs")
    def test_startedAtがない場合はNA文字列(self, mock_ecs):
        mock_ecs.list_tasks.return_value = {
            "taskArns": ["arn:aws:ecs:r:id:task/c/task1"]
        }
        mock_ecs.describe_tasks.return_value = {
            "tasks": [
                {
                    "taskArn": "arn:aws:ecs:r:id:task/c/task1",
                    "lastStatus": "RUNNING",
                    "containers": [],
                    # startedAt なし
                }
            ]
        }

        from health_check import get_running_tasks

        tasks = get_running_tasks("cluster", "service")
        assert tasks[0]["startedAt"] == "N/A"

    @patch("health_check.ecs")
    def test_コンテナのhealthStatusがない場合はUNKNOWN(self, mock_ecs):
        mock_ecs.list_tasks.return_value = {
            "taskArns": ["arn:aws:ecs:r:id:task/c/task1"]
        }
        mock_ecs.describe_tasks.return_value = {
            "tasks": [
                {
                    "taskArn": "arn:aws:ecs:r:id:task/c/task1",
                    "lastStatus": "RUNNING",
                    "containers": [
                        {"name": "app", "lastStatus": "RUNNING"}
                        # healthStatus なし
                    ],
                }
            ]
        }

        from health_check import get_running_tasks

        tasks = get_running_tasks("cluster", "service")
        assert tasks[0]["containers"][0]["healthStatus"] == "UNKNOWN"

    @patch("health_check.ecs")
    def test_複数コンテナが全件取得される(self, mock_ecs):
        mock_ecs.list_tasks.return_value = {
            "taskArns": ["arn:aws:ecs:r:id:task/c/task1"]
        }
        mock_ecs.describe_tasks.return_value = {
            "tasks": [
                {
                    "taskArn": "arn:aws:ecs:r:id:task/c/task1",
                    "lastStatus": "RUNNING",
                    "containers": [
                        {"name": "app", "lastStatus": "RUNNING"},
                        {"name": "sidecar", "lastStatus": "RUNNING"},
                    ],
                }
            ]
        }

        from health_check import get_running_tasks

        tasks = get_running_tasks("cluster", "service")
        assert len(tasks[0]["containers"]) == 2
        assert tasks[0]["containers"][1]["name"] == "sidecar"


# ── health_check ──────────────────────────────────────────


class TestHealthCheckDetail:
    def _normal_service(self):
        return {
            "serviceName": "svc",
            "status": "ACTIVE",
            "desiredCount": 1,
            "runningCount": 1,
            "pendingCount": 0,
            "taskDefinition": "td:1",
            "launchType": "FARGATE",
        }

    def _running_task(self):
        return [
            {
                "taskId": "abc123",
                "lastStatus": "RUNNING",
                "healthStatus": "HEALTHY",
                "startedAt": "2026-07-21 10:00:00",
                "containers": [{"name": "app", "lastStatus": "RUNNING"}],
            }
        ]

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_正常系はTrueを返す(self, mock_info, mock_tasks):
        mock_info.return_value = self._normal_service()
        mock_tasks.return_value = self._running_task()

        from health_check import health_check

        result = health_check("cluster", "service")
        assert result is True

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_statusがACTIVEでないときFalseを返す(self, mock_info, mock_tasks):
        svc = self._normal_service()
        svc["status"] = "INACTIVE"
        mock_info.return_value = svc
        mock_tasks.return_value = self._running_task()

        from health_check import health_check

        result = health_check("cluster", "service")
        assert result is False

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_runningCountがdesiredCount未満でFalseを返す(self, mock_info, mock_tasks):
        svc = self._normal_service()
        svc["desiredCount"] = 2
        svc["runningCount"] = 1
        mock_info.return_value = svc
        mock_tasks.return_value = self._running_task()

        from health_check import health_check

        result = health_check("cluster", "service")
        assert result is False

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_タスクがない場合Falseを返す(self, mock_info, mock_tasks):
        mock_info.return_value = self._normal_service()
        mock_tasks.return_value = []

        from health_check import health_check

        result = health_check("cluster", "service")
        assert result is False

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_タスクがRUNNING以外でFalseを返す(self, mock_info, mock_tasks):
        mock_info.return_value = self._normal_service()
        task = self._running_task()
        task[0]["lastStatus"] = "STOPPED"
        mock_tasks.return_value = task

        from health_check import health_check

        result = health_check("cluster", "service")
        assert result is False

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_pendingCountがあってもrunning_equalsDesiredならTrueを返す(
        self, mock_info, mock_tasks
    ):
        svc = self._normal_service()
        svc["desiredCount"] = 2
        svc["runningCount"] = 2
        svc["pendingCount"] = 1  # pending があっても running==desired なら OK
        mock_info.return_value = svc
        mock_tasks.return_value = self._running_task() + self._running_task()

        from health_check import health_check

        result = health_check("cluster", "service")
        assert result is True


# ── get_service_info 追加エッジケース ──────────────────────


class TestGetServiceInfoAdditional:
    @patch("health_check.ecs")
    def test_複数サービスが返されたとき最初のみ使用する(self, mock_ecs):
        mock_ecs.describe_services.return_value = {
            "services": [
                {
                    "serviceName": "first-service",
                    "status": "ACTIVE",
                    "desiredCount": 1,
                    "runningCount": 1,
                    "pendingCount": 0,
                    "taskDefinition": "arn:aws:ecs:r:id:task-definition/td:1",
                    "launchType": "FARGATE",
                },
                {
                    "serviceName": "second-service",
                    "status": "ACTIVE",
                    "desiredCount": 1,
                    "runningCount": 1,
                    "pendingCount": 0,
                    "taskDefinition": "arn:aws:ecs:r:id:task-definition/td:2",
                },
            ]
        }

        from health_check import get_service_info

        info = get_service_info("cluster", "first-service")
        assert info["serviceName"] == "first-service"

    @patch("health_check.ecs")
    def test_statusフィールドが返される(self, mock_ecs):
        mock_ecs.describe_services.return_value = {
            "services": [
                {
                    "serviceName": "svc",
                    "status": "DRAINING",
                    "desiredCount": 1,
                    "runningCount": 0,
                    "pendingCount": 0,
                    "taskDefinition": "arn:aws:ecs:r:id:task-definition/td:1",
                    "launchType": "FARGATE",
                }
            ]
        }

        from health_check import get_service_info

        info = get_service_info("cluster", "svc")
        assert info["status"] == "DRAINING"

    @patch("health_check.ecs")
    def test_desiredCountとrunningCountが正しく返される(self, mock_ecs):
        mock_ecs.describe_services.return_value = {
            "services": [
                {
                    "serviceName": "svc",
                    "status": "ACTIVE",
                    "desiredCount": 3,
                    "runningCount": 2,
                    "pendingCount": 1,
                    "taskDefinition": "arn:aws:ecs:r:id:task-definition/td:1",
                }
            ]
        }

        from health_check import get_service_info

        info = get_service_info("cluster", "svc")
        assert info["desiredCount"] == 3
        assert info["runningCount"] == 2
        assert info["pendingCount"] == 1


# ── get_running_tasks 追加エッジケース ────────────────────


class TestGetRunningTasksAdditional:
    @patch("health_check.ecs")
    def test_複数タスクが全件返される(self, mock_ecs):
        mock_ecs.list_tasks.return_value = {
            "taskArns": [
                "arn:aws:ecs:r:id:task/c/task1",
                "arn:aws:ecs:r:id:task/c/task2",
            ]
        }
        mock_ecs.describe_tasks.return_value = {
            "tasks": [
                {
                    "taskArn": "arn:aws:ecs:r:id:task/c/task1",
                    "lastStatus": "RUNNING",
                    "containers": [],
                },
                {
                    "taskArn": "arn:aws:ecs:r:id:task/c/task2",
                    "lastStatus": "RUNNING",
                    "containers": [],
                },
            ]
        }

        from health_check import get_running_tasks

        result = get_running_tasks("cluster", "service")
        assert len(result) == 2
        assert result[0]["taskId"] == "task1"
        assert result[1]["taskId"] == "task2"

    @patch("health_check.ecs")
    def test_describe_tasksにtaskArnsが渡される(self, mock_ecs):
        arns = ["arn:aws:ecs:r:id:task/c/task1"]
        mock_ecs.list_tasks.return_value = {"taskArns": arns}
        mock_ecs.describe_tasks.return_value = {
            "tasks": [
                {
                    "taskArn": arns[0],
                    "lastStatus": "RUNNING",
                    "containers": [],
                }
            ]
        }

        from health_check import get_running_tasks

        get_running_tasks("cluster", "service")
        mock_ecs.describe_tasks.assert_called_once_with(cluster="cluster", tasks=arns)

    @patch("health_check.ecs")
    def test_タスクレベルのhealthStatusがない場合UNKNOWN(self, mock_ecs):
        mock_ecs.list_tasks.return_value = {
            "taskArns": ["arn:aws:ecs:r:id:task/c/task1"]
        }
        mock_ecs.describe_tasks.return_value = {
            "tasks": [
                {
                    "taskArn": "arn:aws:ecs:r:id:task/c/task1",
                    "lastStatus": "RUNNING",
                    # healthStatus なし（タスクレベル）
                    "containers": [],
                }
            ]
        }

        from health_check import get_running_tasks

        result = get_running_tasks("cluster", "service")
        assert result[0]["healthStatus"] == "UNKNOWN"


# ── health_check 追加エッジケース ─────────────────────────


class TestHealthCheckAdditional:
    def _normal_service(self):
        return {
            "serviceName": "svc",
            "status": "ACTIVE",
            "desiredCount": 1,
            "runningCount": 1,
            "pendingCount": 0,
            "taskDefinition": "td:1",
            "launchType": "FARGATE",
        }

    def _running_task(self):
        return {
            "taskId": "abc123",
            "lastStatus": "RUNNING",
            "healthStatus": "HEALTHY",
            "startedAt": "2026-07-21 10:00:00",
            "containers": [{"name": "app", "lastStatus": "RUNNING"}],
        }

    def test_get_running_tasksがClientErrorのときFalseを返す(self):
        from unittest.mock import patch as mpatch

        import health_check as hc_mod

        # health_check.py の except ClientError が確実に機能するよう
        # ClientError クラスごと差し替えて一致させる
        class _FakeError(Exception):
            def __init__(self, error_response, operation_name):
                self.response = error_response
                super().__init__(f"{operation_name}: {error_response}")

        with mpatch.object(hc_mod, "ClientError", _FakeError), mpatch.object(
            hc_mod, "get_service_info"
        ) as mock_info, mpatch.object(hc_mod, "get_running_tasks") as mock_tasks:
            mock_info.return_value = self._normal_service()
            mock_tasks.side_effect = _FakeError(
                {"Error": {"Code": "ClusterNotFoundException"}}, "ListTasks"
            )
            result = hc_mod.health_check("cluster", "service")
        assert result is False

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_DRAININGステータスでFalseを返す(self, mock_info, mock_tasks):
        from health_check import health_check

        svc = self._normal_service()
        svc["status"] = "DRAINING"
        mock_info.return_value = svc
        mock_tasks.return_value = [self._running_task()]
        result = health_check("cluster", "service")
        assert result is False

    @patch("health_check.get_running_tasks")
    @patch("health_check.get_service_info")
    def test_複数タスクの一部がSTOPPEDのときFalseを返す(self, mock_info, mock_tasks):
        from health_check import health_check

        svc = self._normal_service()
        svc["desiredCount"] = 2
        svc["runningCount"] = 2
        mock_info.return_value = svc
        stopped_task = self._running_task()
        stopped_task["lastStatus"] = "STOPPED"
        mock_tasks.return_value = [self._running_task(), stopped_task]
        result = health_check("cluster", "service")
        assert result is False
