"""Реестр детекторов и общая функция сканирования текста."""

from __future__ import annotations

from app.detectors.base import Detector
from app.detectors.commercial import commercial_detectors
from app.detectors.pii import pii_detectors
from app.detectors.secrets import secret_detectors
from app.domain.entities import Match


def default_detectors() -> list[Detector]:
    """Активные детекторы: ПДн, секреты, коммерчески чувствительные данные."""
    detectors: list[Detector] = [
        *pii_detectors(),
        *secret_detectors(),
        *commercial_detectors(),
    ]
    return detectors


def scan(text: str, detectors: list[Detector] | None = None) -> list[Match]:
    """Прогоняет текст через детекторы и возвращает найденные сущности.

    Особенности классификации (итерация 2, только regex):
    - Телефон детектируется лишь с явным префиксом +7/8; «голое» 10-значное
      число, прошедшее контрольную сумму ИНН, помечается как ИНН — контрольная
      сумма служит положительным признаком ИНН.
    - Детекторы могут давать пересекающиеся кандидаты по одному фрагменту:
      БИК и КПП оба 9-значные, а БИК (префикс 04) совпадает с форматом КПП —
      такой фрагмент помечается обоими типами (КПП региона 04 при этом не
      теряется). Это осознанное пересечение.
    Контекстная дизамбигуация и снятие пересечений — задача NER и маскирования
    (итерация 4).
    """
    active = detectors if detectors is not None else default_detectors()
    matches: list[Match] = []
    for detector in active:
        matches.extend(detector.detect(text))
    matches.sort(key=lambda m: (m.start, m.end))
    return matches
