"""HTTP-роуты сервиса rubezh-sanitizer."""

from __future__ import annotations

import time
from typing import Annotated

from fastapi import APIRouter, Depends, Request

from app.api.metrics import SanitizerMetrics
from app.api.schemas import SanitizeRequest, SanitizeResponse
from app.detectors.base import Detector
from app.masking.crypto import MappingCipher
from app.masking.pipeline import sanitize

router = APIRouter()


def get_cipher(request: Request) -> MappingCipher:
    """Шифр mapping'ов из состояния приложения (создан в lifespan)."""
    cipher: MappingCipher = request.app.state.cipher
    return cipher


def get_llm_detector(request: Request) -> Detector | None:
    """Детектор LLM-review (фильтр 2/3) из состояния приложения; может быть None."""
    detector: Detector | None = getattr(request.app.state, "llm_detector", None)
    return detector


@router.get("/health")
def health() -> dict[str, str]:
    """Liveness/readiness проба сервиса."""
    return {"status": "ok", "service": "rubezh-sanitizer"}


def get_metrics(request: Request) -> SanitizerMetrics | None:
    """Метрики Prometheus из app.state (W4.2). None в degraded-режиме."""
    return getattr(request.app.state, "metrics", None)


def _enum_value(v: object) -> str:
    """Безопасное извлечение string-метки из enum-like значения.

    Match.detector/category могут быть либо StrEnum (есть .value),
    либо уже строкой (legacy-детекторы). Унифицируем.
    """
    value = getattr(v, "value", None)
    return str(value) if value is not None else str(v)


@router.post("/sanitize/preview", response_model=SanitizeResponse)
def sanitize_preview(
    payload: SanitizeRequest,
    cipher: Annotated[MappingCipher, Depends(get_cipher)],
    llm_detector: Annotated[Detector | None, Depends(get_llm_detector)],
    metrics: Annotated[SanitizerMetrics | None, Depends(get_metrics)],
) -> SanitizeResponse:
    """Предпросмотр обезличивания текста.

    Фильтр 1 (regex) дополняется фильтром 2/3 (LLM-review) при наличии. LLM
    fail-open: его сбой не влияет на ответ. Stateless: mapping'и формируются и
    шифруются, но не персистятся (mapping_id = null) — запись в
    pseudonym_mappings выполняется в оркестрации чата (итерация 8).
    """
    ctx = payload.context
    start = time.monotonic()
    outcome = "ok"
    try:
        ner = [llm_detector] if llm_detector is not None else None
        result = sanitize(payload.text, cipher, ner=ner)
        # W4.2: счётчик match'ей по детектору/категории.
        # detector/category в Match — enum-объекты (см. detectors/base.py),
        # но в SanitizeResponse сериализуются в string; на стороне Match
        # может быть string-alias из старых детекторов — поэтому
        # str(...) безопасен для обоих случаев.
        if metrics is not None:
            for ent in result.entities:
                metrics.detector_matches.labels(
                    detector=_enum_value(ent.detector),
                    category=_enum_value(ent.category),
                ).inc()
        return SanitizeResponse.from_result(result)
    except Exception:
        outcome = "error"
        raise
    finally:
        if metrics is not None:
            metrics.sanitize_requests.labels(context=ctx, outcome=outcome).inc()
            metrics.sanitize_duration.labels(context=ctx).observe(
                time.monotonic() - start,
            )
