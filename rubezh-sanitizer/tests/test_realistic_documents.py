"""Функциональные тесты на реалистичных документах и инвариант спанов."""

from __future__ import annotations

from app.detectors.registry import scan
from app.domain.entities import EntityType, Match
from app.domain.risk import score_risk

_REQUISITES = (
    "ООО «Ромашка», ИНН 7707083893, КПП 770701001, "
    "ОГРН 1027700132195, р/с 40702810500000000123, БИК 044525225"
)


def _types(matches: list[Match]) -> set[EntityType]:
    return {m.type for m in matches}


def test_requisites_block_detects_all_entities() -> None:
    found = _types(scan(_REQUISITES))
    expected = {
        EntityType.COMMERCIAL_SUPPLIER,
        EntityType.INN,
        EntityType.KPP,
        EntityType.OGRN,
        EntityType.ACCOUNT,
        EntityType.BIK,
    }
    assert expected <= found


def test_spans_exact_in_requisites_block() -> None:
    for match in scan(_REQUISITES):
        assert _REQUISITES[match.start : match.end] == match.value
        assert 0 <= match.start < match.end <= len(_REQUISITES)


def test_spans_exact_in_mixed_document() -> None:
    text = (
        "Договор № 14/2025, контакт ivan@example.ru, тел +7 (495) 123-45-67, "
        "сумма 1 500 000 руб., ключ AKIAIOSFODNN7EXAMPLE, password=Qwerty12345"
    )
    matches = scan(text)
    assert matches
    for match in matches:
        # инвариант: границы спана точно соответствуют значению (в т. ч. group=1)
        assert text[match.start : match.end] == match.value


def test_classes_invariant_on_realistic_document() -> None:
    text = (
        "Реквизиты ООО «Вектор», ИНН 7707083893. "
        "Ключ доступа AKIAIOSFODNN7EXAMPLE в коде. Бюджет 5 млн рублей."
    )
    matches = scan(text)
    risk = score_risk(matches)
    # инвариант контракта: classes = объединение категорий всех сущностей
    assert set(risk.classes) == {m.category for m in matches}
