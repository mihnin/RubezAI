"""HTTP-роуты сервиса rubezh-sanitizer."""

from __future__ import annotations

from fastapi import APIRouter

from app.api.schemas import SanitizeRequest, SanitizeResponse
from app.deps import cipher
from app.masking.pipeline import sanitize

router = APIRouter()


@router.get("/health")
def health() -> dict[str, str]:
    """Liveness/readiness проба сервиса."""
    return {"status": "ok", "service": "rubezh-sanitizer"}


@router.post("/sanitize/preview", response_model=SanitizeResponse)
def sanitize_preview(request: SanitizeRequest) -> SanitizeResponse:
    """Предпросмотр обезличивания текста.

    Stateless: mapping'и формируются и шифруются, но не персистятся
    (mapping_id = null). Запись в pseudonym_mappings — в оркестрации
    чата (итерация 8).
    """
    result = sanitize(request.text, cipher)
    return SanitizeResponse.from_result(result)
