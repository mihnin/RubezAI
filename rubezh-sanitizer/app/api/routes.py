"""HTTP-роуты сервиса rubezh-sanitizer."""

from __future__ import annotations

from typing import Annotated

from fastapi import APIRouter, Depends, Request

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


@router.post("/sanitize/preview", response_model=SanitizeResponse)
def sanitize_preview(
    payload: SanitizeRequest,
    cipher: Annotated[MappingCipher, Depends(get_cipher)],
    llm_detector: Annotated[Detector | None, Depends(get_llm_detector)],
) -> SanitizeResponse:
    """Предпросмотр обезличивания текста.

    Фильтр 1 (regex) дополняется фильтром 2/3 (LLM-review) при наличии. LLM
    fail-open: его сбой не влияет на ответ. Stateless: mapping'и формируются и
    шифруются, но не персистятся (mapping_id = null) — запись в
    pseudonym_mappings выполняется в оркестрации чата (итерация 8).
    """
    ner = [llm_detector] if llm_detector is not None else None
    result = sanitize(payload.text, cipher, ner=ner)
    return SanitizeResponse.from_result(result)
