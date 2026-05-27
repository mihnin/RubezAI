"""Prometheus-метрики rubezh-sanitizer (W4.2).

Эндпоинт: GET /metrics, без auth (внутренний scrape за периметром).
Префикс — `rubezh_sanitizer_*`.

Лейблы:
- context: chat | document | system_prompt | review_system_prompt
  (соответствует sanitize.schema.json после W3.1).
- outcome: ok | error
- detector: regex | dictionary | ner | llm_review
- category: pii | secret | commercial
"""

from __future__ import annotations

from fastapi import APIRouter, Response
from prometheus_client import (
    CONTENT_TYPE_LATEST,
    CollectorRegistry,
    Counter,
    Histogram,
    generate_latest,
)


class SanitizerMetrics:
    """Контейнер метрик с изолированным реестром.

    Изолированный реестр (а не глобальный) даёт детерминизм в тестах
    и позволяет запускать N экземпляров приложения в одном процессе
    (например, при integration-тестах TestClient).
    """

    def __init__(self) -> None:
        self.registry = CollectorRegistry()
        self.sanitize_requests = Counter(
            "rubezh_sanitizer_requests_total",
            "Запросы /sanitize/preview по контексту/исходу",
            labelnames=("context", "outcome"),
            registry=self.registry,
        )
        self.sanitize_duration = Histogram(
            "rubezh_sanitizer_duration_seconds",
            "Длительность sanitize по контексту",
            labelnames=("context",),
            registry=self.registry,
        )
        self.detector_matches = Counter(
            "rubezh_sanitizer_detector_matches_total",
            "Сущности, найденные конкретным детектором",
            labelnames=("detector", "category"),
            registry=self.registry,
        )


router = APIRouter()


@router.get("/metrics")
async def metrics_endpoint(response: Response) -> Response:
    """Экспозиция в text-exposition формате Prometheus."""
    from app.main import app  # отложенный импорт, чтобы избежать цикла

    metrics: SanitizerMetrics | None = getattr(app.state, "metrics", None)
    if metrics is None:
        # На раннем старте до lifespan / в degraded-режиме — пустые метрики.
        return Response(content="", media_type=CONTENT_TYPE_LATEST)
    data = generate_latest(metrics.registry)
    return Response(content=data, media_type=CONTENT_TYPE_LATEST)
