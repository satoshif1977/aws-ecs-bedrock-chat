"""scripts テスト共通フィクスチャ"""

from __future__ import annotations

from unittest.mock import MagicMock

import health_check as hc
import pytest


@pytest.fixture(autouse=True)
def reset_ecs_client():
    """各テスト後に hc.ecs をリセットしてテスト間干渉を防ぐ"""
    yield
    hc.ecs = MagicMock()
