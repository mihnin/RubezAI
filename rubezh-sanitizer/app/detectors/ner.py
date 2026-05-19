"""Mock-детектор NER — заглушка фильтра 2/3 (малая русскоязычная LLM)."""

from __future__ import annotations

from app.domain.entities import Match


class MockNerDetector:
    """Заглушка NER/LLM-review детектора.

    Реальная малая русскоязычная LLM подключается через тот же интерфейс
    ``Detector`` без изменения конвейера обезличивания. Для MVP возвращает
    заранее заданные сущности (``canned``), по умолчанию — ничего.
    """

    name = "ner_mock"

    def __init__(self, canned: list[Match] | None = None) -> None:
        self._canned = canned or []

    def detect(self, text: str) -> list[Match]:
        return [match for match in self._canned if match.value in text]
