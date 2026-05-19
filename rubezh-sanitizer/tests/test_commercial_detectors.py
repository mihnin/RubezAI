"""Unit-тесты детекторов коммерчески чувствительных данных."""

from __future__ import annotations

import pytest

from app.detectors.registry import scan
from app.domain.entities import Category, EntityType, Match


def _types(matches: list[Match]) -> set[EntityType]:
    return {m.type for m in matches}


def test_detect_amount_rub() -> None:
    assert EntityType.COMMERCIAL_AMOUNT in _types(scan("сумма сделки 1 500 000 руб."))


def test_detect_amount_million() -> None:
    assert EntityType.COMMERCIAL_AMOUNT in _types(scan("бюджет проекта 5 млн рублей"))


def test_detect_contract_number() -> None:
    assert EntityType.COMMERCIAL_CONTRACT in _types(scan("по договору № 14/2025 от марта"))


def test_contract_word_without_number_is_not_match() -> None:
    # «договор поставки» без номера — не номер договора
    assert EntityType.COMMERCIAL_CONTRACT not in _types(scan("заключили договор поставки"))


def test_detect_supplier_org() -> None:
    assert EntityType.COMMERCIAL_SUPPLIER in _types(scan('поставщик ООО «Ромашка» из Твери'))


def test_detect_commercial_terms() -> None:
    assert EntityType.COMMERCIAL_TERMS in _types(scan("маржа по сделке составила 18 процентов"))


def test_clean_text_has_no_commercial() -> None:
    matches = scan("Сегодня солнечно и тепло, отличная погода")
    assert not any(m.category is Category.COMMERCIAL for m in matches)


@pytest.mark.parametrize(
    "amount",
    ["100 000 руб.", "5 млн рублей", "200 USD", "300 евро", "1,5 млрд рублей"],
)
def test_amount_currency_variants(amount: str) -> None:
    assert EntityType.COMMERCIAL_AMOUNT in _types(scan(f"стоимость {amount}"))


def test_amount_with_kopecks() -> None:
    assert EntityType.COMMERCIAL_AMOUNT in _types(scan("итого 1 500 000,50 руб."))


def test_contract_match_includes_keyword() -> None:
    # в отличие от пароля, номер договора матчится вместе с ключевым словом
    found = next(
        m for m in scan("по договору №14/2025-А") if m.type == EntityType.COMMERCIAL_CONTRACT
    )
    assert "14/2025" in found.value
    assert found.value.lower().startswith("договор")


@pytest.mark.parametrize(
    "org",
    ['ООО «Ромашка»', 'АО "Вектор"', 'ПАО «Газпром»', 'ИП "Сидоров"'],
)
def test_supplier_org_form_variants(org: str) -> None:
    assert EntityType.COMMERCIAL_SUPPLIER in _types(scan(f"контрагент {org}"))
