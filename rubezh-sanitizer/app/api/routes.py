"""HTTP-роуты сервиса rubezh-sanitizer."""

from __future__ import annotations

from fastapi import APIRouter

router = APIRouter()


@router.get("/health")
def health() -> dict[str, str]:
    """Liveness/readiness проба сервиса."""
    return {"status": "ok", "service": "rubezh-sanitizer"}
