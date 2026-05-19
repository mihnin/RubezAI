"""Тесты mock-детектора NER — интерфейса фильтра 2/3."""

from __future__ import annotations

from app.detectors.base import Detector
from app.detectors.ner import MockNerDetector
from app.domain.entities import Category, EntityType, Match


def test_mock_ner_conforms_to_detector_protocol() -> None:
    # реальная русскоязычная LLM/NER подключается через тот же интерфейс
    assert isinstance(MockNerDetector(), Detector)


def test_mock_ner_returns_empty_by_default() -> None:
    assert MockNerDetector().detect("любой текст") == []


def test_mock_ner_returns_canned_entities_found_in_text() -> None:
    canned = [
        Match(
            type=EntityType.PERSON,
            category=Category.PII,
            start=0,
            end=6,
            value="Иванов",
            detector="ner",
            confidence=0.8,
        )
    ]
    ner = MockNerDetector(canned=canned)
    assert ner.detect("документ Иванов подписал") != []
    assert ner.detect("текст без имён") == []
