"""Unit-тесты regex-детекторов ПДн и валидаторов контрольных сумм."""

from __future__ import annotations

import pytest

from app.detectors.pii import validate_inn, validate_ogrn, validate_snils
from app.detectors.registry import scan
from app.domain.entities import Category, EntityType, Match

# --- контрольные суммы ---


@pytest.mark.parametrize("value", ["7707083893", "500100732259"])
def test_validate_inn_valid(value: str) -> None:
    assert validate_inn(value) is True


@pytest.mark.parametrize("value", ["7707083894", "500100732250", "123", "1234567890"])
def test_validate_inn_invalid(value: str) -> None:
    assert validate_inn(value) is False


def test_validate_snils_valid() -> None:
    assert validate_snils("112-233-445 95") is True


@pytest.mark.parametrize("value", ["112-233-445 96", "12345"])
def test_validate_snils_invalid(value: str) -> None:
    assert validate_snils(value) is False


def test_validate_ogrn_valid() -> None:
    assert validate_ogrn("1027700132195") is True


def test_validate_ogrn_invalid() -> None:
    assert validate_ogrn("1027700132190") is False


# --- детекторы: каждый тип находится в тексте ---


def _types(matches: list[Match]) -> set[EntityType]:
    return {m.type for m in matches}


def test_detect_email() -> None:
    matches = scan("Пишите на ivan.petrov@example.ru сегодня")
    assert EntityType.EMAIL in _types(matches)
    email = next(m for m in matches if m.type == EntityType.EMAIL)
    assert email.value == "ivan.petrov@example.ru"
    assert email.category is Category.PII


@pytest.mark.parametrize("raw", ["+7 (495) 123-45-67", "8 916 123-45-67"])
def test_detect_phone(raw: str) -> None:
    assert EntityType.PHONE in _types(scan(f"тел: {raw}"))


def test_detect_inn_valid_only() -> None:
    # валидный ИНН найден; случайное 10-значное число — нет (контрольная сумма)
    assert EntityType.INN in _types(scan("ИНН 7707083893"))
    assert EntityType.INN not in _types(scan("число 1234567890 в тексте"))


def test_detect_snils() -> None:
    assert EntityType.SNILS in _types(scan("СНИЛС 112-233-445 95"))


def test_detect_ogrn() -> None:
    assert EntityType.OGRN in _types(scan("ОГРН 1027700132195"))


def test_detect_passport() -> None:
    assert EntityType.PASSPORT in _types(scan("паспорт 4509 123456 выдан"))


def test_detect_kpp_bik_account() -> None:
    assert EntityType.KPP in _types(scan("КПП 770701001"))
    assert EntityType.BIK in _types(scan("БИК 044525225"))
    assert EntityType.ACCOUNT in _types(scan("счёт 40702810500000000123 банка"))


def test_detect_person() -> None:
    assert EntityType.PERSON in _types(scan("Договор подписал Иванов Иван Иванович"))
    assert EntityType.PERSON in _types(scan("Ответственный: Петров П. С."))


def test_match_spans_are_correct() -> None:
    text = "почта: ivan@example.ru"
    match = next(m for m in scan(text) if m.type == EntityType.EMAIL)
    assert text[match.start : match.end] == match.value == "ivan@example.ru"


def test_clean_text_has_no_matches() -> None:
    assert scan("Сегодня хорошая погода и ничего секретного") == []


def test_pii_detectors_registered() -> None:
    from app.detectors.pii import pii_detectors

    names = {d.name for d in pii_detectors()}
    assert {"email", "phone", "inn", "snils", "ogrn", "passport"} <= names
