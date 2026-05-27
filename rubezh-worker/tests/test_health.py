"""Тест /health — компилируется + возвращает ok."""

import os

import pytest
from fastapi.testclient import TestClient


@pytest.fixture(autouse=True)
def _env_for_settings(monkeypatch: pytest.MonkeyPatch) -> None:
    """Минимальный env для Settings (обязательные поля)."""
    monkeypatch.setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test")
    monkeypatch.setenv("MINIO_ROOT_USER", "rubezh")
    monkeypatch.setenv("MINIO_ROOT_PASSWORD", "rubezh-minio")


def test_live_returns_ok_without_db() -> None:
    """/live — liveness probe, не требует БД (W2.4)."""
    from app.main import app

    with TestClient(app) as client:
        resp = client.get("/live")
        assert resp.status_code == 200
        assert resp.json() == {"status": "ok", "service": "rubezh-worker"}


def test_health_alias_returns_ok_without_db() -> None:
    """/health — backward-compat alias /live."""
    from app.main import app

    with TestClient(app) as client:
        resp = client.get("/health")
        assert resp.status_code == 200
        assert resp.json() == {"status": "ok", "service": "rubezh-worker"}


def test_ready_returns_503_when_db_unreachable() -> None:
    """/ready — readiness probe; без БД отвечает 503."""
    from app.main import app

    with TestClient(app) as client:
        resp = client.get("/ready")
        assert resp.status_code == 503
        body = resp.json()
        assert body["status"] == "not_ready"
        assert body["reason"] in ("db_pool_not_initialized", "db_unreachable")
