#!/usr/bin/env python3
"""
ECS サービス ヘルスチェックスクリプト

aws-ecs-bedrock-chat の ECS サービス・タスク状態を確認し、
正常に稼働しているかを検証する。

使い方:
    aws-vault exec personal-dev-source -- python scripts/health_check.py

環境変数:
    ECS_CLUSTER  : ECS クラスター名（デフォルト: bedrock-chat-dev-cluster）
    ECS_SERVICE  : ECS サービス名（デフォルト: bedrock-chat-dev-service）
    AWS_REGION   : リージョン（デフォルト: ap-northeast-1）
"""

from __future__ import annotations

import os
import sys
from typing import Any

import boto3
from botocore.exceptions import ClientError

# ── 設定 ─────────────────────────────────────────────────────

REGION: str = os.environ.get("AWS_REGION", "ap-northeast-1")
CLUSTER: str = os.environ.get("ECS_CLUSTER", "bedrock-chat-dev-cluster")
SERVICE: str = os.environ.get("ECS_SERVICE", "bedrock-chat-dev-service")

# ── クライアント ──────────────────────────────────────────────

ecs = boto3.client("ecs", region_name=REGION)


# ── ヘルパー関数 ──────────────────────────────────────────────

def get_service_info(cluster: str, service: str) -> dict[str, Any]:
    """ECS サービスの状態を取得する。"""
    response = ecs.describe_services(cluster=cluster, services=[service])
    services = response.get("services", [])
    if not services:
        raise ValueError(f"サービスが見つかりません: {service}")
    svc = services[0]
    return {
        "serviceName": svc["serviceName"],
        "status": svc["status"],
        "desiredCount": svc["desiredCount"],
        "runningCount": svc["runningCount"],
        "pendingCount": svc["pendingCount"],
        "taskDefinition": svc["taskDefinition"].split("/")[-1],
        "launchType": svc.get("launchType", "FARGATE"),
    }


def get_running_tasks(cluster: str, service: str) -> list[dict[str, Any]]:
    """実行中タスクの一覧と状態を取得する。"""
    response = ecs.list_tasks(cluster=cluster, serviceName=service, desiredStatus="RUNNING")
    task_arns = response.get("taskArns", [])
    if not task_arns:
        return []

    detail = ecs.describe_tasks(cluster=cluster, tasks=task_arns)
    tasks = []
    for t in detail.get("tasks", []):
        containers = [
            {
                "name": c["name"],
                "lastStatus": c["lastStatus"],
                "healthStatus": c.get("healthStatus", "UNKNOWN"),
            }
            for c in t.get("containers", [])
        ]
        tasks.append({
            "taskId": t["taskArn"].split("/")[-1],
            "lastStatus": t["lastStatus"],
            "healthStatus": t.get("healthStatus", "UNKNOWN"),
            "startedAt": str(t.get("startedAt", "N/A")),
            "containers": containers,
        })
    return tasks


def print_section(title: str) -> None:
    print(f"\n{'─' * 50}")
    print(f"  {title}")
    print(f"{'─' * 50}")


def health_check(cluster: str, service: str) -> bool:
    """
    ECS サービスのヘルスチェックを実行し、結果を出力する。

    Returns:
        True: すべてのチェックが通過
        False: 1つ以上のチェックが失敗
    """
    ok = True

    # ── サービス状態 ──────────────────────────────────────────
    print_section("ECS サービス状態")
    try:
        info = get_service_info(cluster, service)
        for key, val in info.items():
            print(f"  {key:<20}: {val}")

        if info["status"] != "ACTIVE":
            print(f"\n  [WARN] サービスが ACTIVE ではありません: {info['status']}")
            ok = False
        elif info["runningCount"] < info["desiredCount"]:
            print(
                f"\n  [WARN] 起動数が不足: running={info['runningCount']} / desired={info['desiredCount']}"
            )
            ok = False
        else:
            print(f"\n  [OK] running={info['runningCount']} / desired={info['desiredCount']}")

    except (ClientError, ValueError) as e:
        print(f"  [ERROR] サービス取得失敗: {e}")
        return False

    # ── タスク詳細 ────────────────────────────────────────────
    print_section("実行中タスク")
    try:
        tasks = get_running_tasks(cluster, service)
        if not tasks:
            print("  [WARN] 実行中のタスクが見つかりません")
            ok = False
        else:
            for task in tasks:
                print(f"  TaskID    : {task['taskId']}")
                print(f"  Status    : {task['lastStatus']}")
                print(f"  Health    : {task['healthStatus']}")
                print(f"  StartedAt : {task['startedAt']}")
                for c in task["containers"]:
                    mark = "[OK]  " if c["lastStatus"] == "RUNNING" else "[WARN]"
                    print(f"  {mark} Container: {c['name']} ({c['lastStatus']})")
                print()

                if task["lastStatus"] != "RUNNING":
                    ok = False

    except ClientError as e:
        print(f"  [ERROR] タスク取得失敗: {e}")
        ok = False

    # ── 総合結果 ──────────────────────────────────────────────
    print_section("総合結果")
    if ok:
        print("  [PASS] サービスは正常に稼働しています")
    else:
        print("  [FAIL] 異常を検出しました（上記 WARN / ERROR を確認）")

    return ok


# ── エントリーポイント ────────────────────────────────────────

def main() -> None:
    print(f"ECS ヘルスチェック開始")
    print(f"  クラスター : {CLUSTER}")
    print(f"  サービス   : {SERVICE}")
    print(f"  リージョン : {REGION}")

    passed = health_check(CLUSTER, SERVICE)
    sys.exit(0 if passed else 1)


if __name__ == "__main__":
    main()
