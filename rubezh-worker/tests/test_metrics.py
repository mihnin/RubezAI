"""W4.2: /metrics в rubezh-worker."""

import pytest
from fastapi.testclient import TestClient


@pytest.fixture(autouse=True)
def _env_for_settings(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv(
        "DATABASE_URL", "postgres://test:test@localhost:5432/test",
    )
    monkeypatch.setenv("MINIO_ROOT_USER", "rubezh")
    monkeypatch.setenv("MINIO_ROOT_PASSWORD", "rubezh-minio")


def test_metrics_endpoint_serves_prometheus_text() -> None:
    """/metrics возвращает text-exposition с rubezh_worker_* сериями."""
    from app.main import app

    with TestClient(app) as client:
        r = client.get("/metrics")
        assert r.status_code == 200
        assert r.headers["content-type"].startswith("text/plain")
        body = r.text
        # Кастомные серии присутствуют в выводе (счётчики на 0, gauge тоже).
        for series in (
            "rubezh_worker_documents_processed_total",
            "rubezh_worker_processing_duration_seconds",
            "rubezh_worker_queue_loop_errors_total",
            "rubezh_worker_db_pool_ready",
        ):
            assert series in body, f"missing series {series}\n{body[:500]}"


def test_db_pool_ready_zero_without_db() -> None:
    """В test-окружении (PG недоступен) DB_POOL_READY == 0."""
    from app.main import app

    with TestClient(app) as client:
        body = client.get("/metrics").text
    assert "rubezh_worker_db_pool_ready 0" in body
