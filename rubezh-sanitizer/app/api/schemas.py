"""Pydantic-схемы HTTP-слоя. Согласованы с docs/contracts/sanitize.schema.json."""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from app.domain.entities import Category, EntityType
from app.domain.risk import RiskLevel
from app.domain.sanitization import SanitizationResult


class SanitizeRequest(BaseModel):
    """Запрос обезличивания (контракт SanitizeRequest)."""

    model_config = ConfigDict(extra="forbid")

    text: str = Field(min_length=1)
    document_id: str | None = None
    context: Literal["chat", "document"]


class EntityOut(BaseModel):
    """Сущность в ответе — без raw-значения (контракт Entity)."""

    model_config = ConfigDict(extra="forbid")

    type: EntityType
    category: Category
    start: int = Field(ge=0)
    end: int = Field(ge=0)
    pseudonym: str
    raw_hash: str
    confidence: float = Field(ge=0.0, le=1.0)
    detector: str


class RiskOut(BaseModel):
    """Оценка риска в ответе (контракт Risk)."""

    model_config = ConfigDict(extra="forbid")

    score: float = Field(ge=0.0, le=1.0)
    level: RiskLevel
    classes: list[Category]


class SanitizeResponse(BaseModel):
    """Ответ обезличивания (контракт SanitizeResponse)."""

    model_config = ConfigDict(extra="forbid")

    sanitized_text: str
    entities: list[EntityOut]
    risk: RiskOut
    mapping_id: str | None = None

    @classmethod
    def from_result(cls, result: SanitizationResult) -> SanitizeResponse:
        """Собирает ответ из доменного результата (без raw-значений)."""
        return cls(
            sanitized_text=result.sanitized_text,
            entities=[
                EntityOut(
                    type=entity.type,
                    category=entity.category,
                    start=entity.start,
                    end=entity.end,
                    pseudonym=entity.pseudonym,
                    raw_hash=entity.raw_hash,
                    confidence=entity.confidence,
                    detector=entity.detector,
                )
                for entity in result.entities
            ],
            risk=RiskOut(
                score=result.risk.score,
                level=result.risk.level,
                classes=list(result.risk.classes),
            ),
            mapping_id=None,
        )
