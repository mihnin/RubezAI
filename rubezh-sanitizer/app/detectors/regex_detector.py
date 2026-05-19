"""Базовый детектор сущностей на основе регулярного выражения."""

from __future__ import annotations

import re
from collections.abc import Callable

from app.domain.entities import Category, EntityType, Match


class RegexDetector:
    """Детектор сущности по регулярному выражению.

    Параметры:
    - ``validator`` — опциональная проверка кандидата (например, контрольная
      сумма); кандидат принимается только при её прохождении — это снижает
      ложные срабатывания.
    - ``confidence`` — уверенность детектора; эвристические паттерны < 1.0.
    - ``group`` — номер группы захвата для значения и границ (0 — всё
      совпадение). Группа > 0 позволяет выделить лишь секрет из конструкции
      вида ``password=<секрет>``, не захватывая ключевое слово.
    """

    def __init__(
        self,
        *,
        name: str,
        entity_type: EntityType,
        category: Category,
        pattern: str,
        validator: Callable[[str], bool] | None = None,
        confidence: float = 1.0,
        group: int = 0,
    ) -> None:
        self.name = name
        self.entity_type = entity_type
        self.category = category
        self._regex = re.compile(pattern)
        self._validator = validator
        self._confidence = confidence
        self._group = group

    def detect(self, text: str) -> list[Match]:
        matches: list[Match] = []
        for found in self._regex.finditer(text):
            value = found.group(self._group)
            if not value:
                continue
            if self._validator is not None and not self._validator(value):
                continue
            matches.append(
                Match(
                    type=self.entity_type,
                    category=self.category,
                    start=found.start(self._group),
                    end=found.end(self._group),
                    value=value,
                    detector="regex",
                    confidence=self._confidence,
                )
            )
        return matches
