"""Тесты mock-детектора NER — интерфейса фильтра 2/3."""

from __future__ import annotations

import base64

from app.detectors.base import Detector
from app.detectors.ner import MockNerDetector
from app.domain.entities import Category, EntityType, Match
from app.masking.crypto import MappingCipher
from app.masking.pipeline import sanitize


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


def test_pipeline_composes_ner_with_regex() -> None:
    # NER-фильтр (2/3) дополняет regex-фильтр (1), а не заменяет его
    cipher = MappingCipher.from_base64_key(base64.b64encode(b"k" * 32).decode())
    canned = [
        Match(
            type=EntityType.PERSON,
            category=Category.PII,
            start=0,
            end=5,
            value="Гость",
            detector="ner",
            confidence=0.7,
        )
    ]
    result = sanitize(
        "Гость, почта a@b.ru", cipher, ner=[MockNerDetector(canned=canned)]
    )
    types = {entity.type for entity in result.entities}
    assert EntityType.PERSON in types  # сущность найдена NER-фильтром
    assert EntityType.EMAIL in types  # сущность найдена regex-фильтром
