"""HTTP-роуты сервиса rubezh-sanitizer."""

from __future__ import annotations

from typing import Annotated

from fastapi import APIRouter, Depends, Request

from app.api.schemas import SanitizeRequest, SanitizeResponse
from app.masking.crypto import MappingCipher
from app.masking.pipeline import sanitize

router = APIRouter()


def get_cipher(request: Request) -> MappingCipher:
    """Шифр mapping'ов из состояния приложения (создан в lifespan)."""
    cipher: MappingCipher = request.app.state.cipher
    return cipher


@router.get("/health")
def health() -> dict[str, str]:
    """Liveness/readiness проба сервиса."""
    return {"status": "ok", "service": "rubezh-sanitizer"}


@router.post("/sanitize/preview", response_model=SanitizeResponse)
def sanitize_preview(
    payload: SanitizeRequest,
    cipher: Annotated[MappingCipher, Depends(get_cipher)],
) -> SanitizeResponse:
    """Предпросмотр обезличивания текста.

    Stateless: mapping'и формируются и шифруются, но не персистятся
    (mapping_id = null). Запись в pseudonym_mappings — в оркестрации
    чата (итерация 8).
    """
    result = sanitize(payload.text, cipher)
    return SanitizeResponse.from_result(result)
