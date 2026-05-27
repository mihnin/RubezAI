"""W4.2: /metrics в Prometheus-формате на rubezh-sanitizer."""

from __future__ import annotations

import pytest
from fastapi.testclient import TestClient


def test_metrics_endpoint_returns_prometheus_format() -> None:
    """GET /metrics возвращает text-exposition без auth."""
    from app.main import app

    # `with TestClient(app)` запускает lifespan (создаёт state.cipher/metrics).
    with TestClient(app) as client:
        payload = {"text": "Иван Иванов 79991234567", "context": "chat"}
        r = client.post("/sanitize/preview", json=payload)
        assert r.status_code == 200, r.text

        r = client.get("/metrics")
        assert r.status_code == 200
        assert r.headers["content-type"].startswith("text/plain"), r.headers
        body = r.text
        assert "rubezh_sanitizer_requests_total" in body
        assert 'context="chat"' in body
        assert 'outcome="ok"' in body
        assert "rubezh_sanitizer_duration_seconds" in body


def test_metrics_endpoint_detector_matches() -> None:
    """Каждая распознанная сущность увеличивает detector_matches."""
    from app.main import app

    with TestClient(app) as client:
        payload = {"text": "Иван Петров", "context": "chat"}
        client.post("/sanitize/preview", json=payload)
        r = client.get("/metrics")
        assert "rubezh_sanitizer_detector_matches_total" in r.text
