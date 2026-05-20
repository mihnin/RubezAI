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


def test_health_returns_ok() -> None:
    """Без БД-подключения /health всё равно отвечает 200."""
    from app.main import app

    with TestClient(app) as client:
        resp = client.get("/health")
        assert resp.status_code == 200
        assert resp.json() == {"status": "ok", "service": "rubezh-worker"}
