"""Конвейер обезличивания: детекция → снятие пересечений → риск + маскирование."""

from __future__ import annotations

from app.detectors.base import Detector
from app.detectors.registry import scan
from app.domain.risk import score_risk
from app.domain.sanitization import SanitizationResult
from app.masking.crypto import MappingCipher
from app.masking.overlap import resolve_overlaps
from app.masking.pseudonymizer import pseudonymize


def sanitize(
    text: str, cipher: MappingCipher, detectors: list[Detector] | None = None
) -> SanitizationResult:
    """Полный конвейер обезличивания текста.

    Фильтр 1 (regex/словари) → снятие пересечений → оценка риска и
    псевдонимизация. Фильтр 2/3 (NER/LLM) подключается через ``detectors``.
    """
    matches = resolve_overlaps(scan(text, detectors))
    risk = score_risk(matches)
    sanitized_text, entities, mappings = pseudonymize(text, matches, cipher)
    return SanitizationResult(
        sanitized_text=sanitized_text,
        entities=tuple(entities),
        risk=risk,
        mappings=tuple(mappings),
    )
