"""Интерфейс детектора."""

from __future__ import annotations

from typing import Protocol, runtime_checkable

from app.domain.entities import Match


@runtime_checkable
class Detector(Protocol):
    """Контракт детектора сущностей.

    Реализации взаимозаменяемы: regex, словарь, NER, LLM-review. Это позволяет
    подключить реальную модель без изменения вызывающего кода (ТЗ).
    """

    name: str

    def detect(self, text: str) -> list[Match]:
        """Возвращает сущности, найденные в тексте."""
        ...
