"""Детектор-адаптер: оборачивает клиент LLM-review в интерфейс ``Detector``.

LLM возвращает дословные подстроки; детектор сам вычисляет их спаны в тексте.
Кандидаты с неизвестным типом или не найденные в тексте отбрасываются —
без спана сущность нельзя обезличить. Найденные пересечения с детерминированными
матчами снимаются на этапе ``resolve_overlaps`` в пайплайне.
"""

from __future__ import annotations

from app.domain.entities import Category, EntityType, Match
from app.llm_review.client import LLMReviewClient

# LLM-кандидаты — эвристика, уверенность ниже валидируемых regex-детекторов.
_LLM_CONFIDENCE = 0.7


def _category_for(entity_type: EntityType) -> Category:
    """Категория по типу: производится от соглашения об именовании EntityType."""
    if entity_type.value.startswith("SECRET_"):
        return Category.SECRET
    if entity_type.value.startswith("COMMERCIAL_"):
        return Category.COMMERCIAL
    return Category.PII


def _iter_spans(text: str, value: str) -> list[tuple[int, int]]:
    """Все непересекающиеся вхождения ``value`` в ``text``."""
    spans: list[tuple[int, int]] = []
    start = text.find(value)
    while start != -1:
        end = start + len(value)
        spans.append((start, end))
        start = text.find(value, end)
    return spans


class LLMReviewDetector:
    """Адаптер клиента LLM-review к контракту ``Detector`` (фильтр 2/3)."""

    name = "llm_review"

    def __init__(self, client: LLMReviewClient) -> None:
        self._client = client

    def detect(self, text: str) -> list[Match]:
        matches: list[Match] = []
        for candidate in self._client.review(text):
            try:
                entity_type = EntityType(candidate.type.strip().upper())
            except ValueError:
                continue  # неизвестный тип — модель не должна расширять контракт
            category = _category_for(entity_type)
            for start, end in _iter_spans(text, candidate.value):
                matches.append(
                    Match(
                        type=entity_type,
                        category=category,
                        start=start,
                        end=end,
                        value=candidate.value,
                        detector="llm_review",
                        confidence=_LLM_CONFIDENCE,
                    )
                )
        return matches
