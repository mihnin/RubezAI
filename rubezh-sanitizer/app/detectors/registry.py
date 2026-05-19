"""Реестр детекторов и общая функция сканирования текста."""

from __future__ import annotations

from app.detectors.base import Detector
from app.detectors.pii import pii_detectors
from app.domain.entities import Match


def default_detectors() -> list[Detector]:
    """Активные детекторы. На итерации 2 — только ПДн.

    Детекторы секретов и коммерческих данных подключаются в итерации 3.
    """
    return list(pii_detectors())


def scan(text: str, detectors: list[Detector] | None = None) -> list[Match]:
    """Прогоняет текст через детекторы и возвращает найденные сущности.

    Детекторы могут давать пересекающиеся кандидаты (например, БИК и КПП —
    оба 9 цифр). Снятие пересечений и приоритизация — в итерации 4 (маскирование).
    """
    active = detectors if detectors is not None else default_detectors()
    matches: list[Match] = []
    for detector in active:
        matches.extend(detector.detect(text))
    matches.sort(key=lambda m: (m.start, m.end))
    return matches
