"""Unit-тесты regex-детекторов ПДн и валидаторов контрольных сумм."""

from __future__ import annotations

import pytest

from app.detectors.pii import (
    validate_card_luhn,
    validate_inn,
    validate_ogrn,
    validate_snils,
)
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


def test_validate_ogrnip_15_valid() -> None:
    assert validate_ogrn("304500116000157") is True


@pytest.mark.parametrize("value", ["0000000000", "000000000000", "1111111111"])
def test_validate_inn_rejects_degenerate(value: str) -> None:
    assert validate_inn(value) is False


def test_validate_snils_rejects_degenerate() -> None:
    assert validate_snils("000-000-000 00") is False


def test_validate_snils_payload_100() -> None:
    # взвешенная сумма == 100 → контрольное число 00
    assert validate_snils("920-000-100 00") is True


# --- контрольная сумма банковской карты (Luhn) ---


@pytest.mark.parametrize("value", ["4539 1488 0343 6467", "4111111111111111", "5500005555555559"])
def test_validate_card_luhn_valid(value: str) -> None:
    assert validate_card_luhn(value) is True


@pytest.mark.parametrize(
    "value",
    ["4539 1488 0343 6468", "1234567812345678", "123", "0000000000000000"],
)
def test_validate_card_luhn_invalid(value: str) -> None:
    assert validate_card_luhn(value) is False


# --- детекторы: каждый тип находится в тексте ---


def test_detect_bank_card_grouped() -> None:
    # Карта в формате 4-4-4-4 ловится даже без валидной Luhn-суммы (как в
    # реальном договоре): «карточное» форматирование само по себе — сильный признак.
    matches = scan("Реквизиты карты для возврата: 4276 1500 1234 5678.")
    card = next((m for m in matches if m.type == EntityType.BANK_CARD), None)
    assert card is not None
    assert card.value == "4276 1500 1234 5678"
    assert card.category is Category.PII


def test_detect_bank_card_continuous_luhn() -> None:
    # Слитная 16-значная последовательность распознаётся только при валидной Luhn.
    assert EntityType.BANK_CARD in _types(scan("карта 4539148803436467 действует"))
    assert EntityType.BANK_CARD not in _types(scan("код 1234567812345678 в системе"))


def test_bank_card_does_not_match_20_digit_account() -> None:
    # 20-значный расчётный счёт не должен распознаваться как карта.
    matches = scan("р/с 40702810700000123456 в банке")
    assert EntityType.BANK_CARD not in _types(matches)
    assert EntityType.ACCOUNT in _types(matches)


def test_detect_labeled_inn_without_valid_checksum() -> None:
    # Число, явно помеченное «ИНН», ловится по контексту даже при невалидной
    # контрольной сумме (синтетические данные договора). Маскируется само число.
    matches = scan("ИНН 770100100100, прочее")
    inn = next((m for m in matches if m.type == EntityType.INN), None)
    assert inn is not None
    assert inn.value == "770100100100"


def test_detect_labeled_snils_without_valid_checksum() -> None:
    matches = scan("СНИЛС 123-456-789 00 в анкете")
    snils = next((m for m in matches if m.type == EntityType.SNILS), None)
    assert snils is not None
    assert snils.value == "123-456-789 00"


def test_unlabeled_invalid_inn_still_not_detected() -> None:
    # Без метки и без валидной контрольной суммы 12-значное число не ИНН —
    # точность контекстного детектора не размывается.
    assert EntityType.INN not in _types(scan("число 770100100100 в журнале"))


def test_detect_passport_with_number_sign() -> None:
    # Паспорт в формате «серия № номер» (как в договоре) ловится со знаком №.
    matches = scan("паспорт серии 4501 № 234567 выдан ОВД")
    passport = next((m for m in matches if m.type == EntityType.PASSPORT), None)
    assert passport is not None
    assert "4501" in passport.value and "234567" in passport.value


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


def test_long_number_does_not_yield_phone() -> None:
    # 20-значный счёт не порождает ложный телефон
    matches = scan("счёт 40702810500000000123 в банке")
    assert EntityType.PHONE not in _types(matches)
    assert EntityType.ACCOUNT in _types(matches)


def test_bik_kpp_overlap_is_known() -> None:
    # БИК и КПП оба 9-значные; БИК с префиксом 04 совпадает с форматом КПП.
    # Это осознанное пересечение — снимается дизамбигуацией в итерации 4.
    assert EntityType.BIK in _types(scan("БИК 044525225"))


def test_region_04_kpp_value_is_detected() -> None:
    # КПП региона 04 (Республика Алтай, формат 04…) не теряется:
    # фрагмент детектируется как минимум одним детектором — реквизит не утекает.
    assert scan("КПП 041101001") != []


def test_bare_10_digit_with_inn_checksum_is_inn() -> None:
    # Документированное поведение: «голое» 10-значное число с валидной
    # контрольной суммой ИНН классифицируется как ИНН, а не телефон.
    assert EntityType.INN in _types(scan("реквизит 9161234567"))


def test_person_detector_has_lower_confidence() -> None:
    # ФИО по regex — эвристика, уверенность ниже валидируемых типов
    person = next(m for m in scan("Иванов Иван Иванович") if m.type == EntityType.PERSON)
    assert person.confidence < 1.0


@pytest.mark.parametrize(
    "raw",
    ["+7 (495) 123-45-67", "8-916-123-45-67", "+79161234567", "8 916 123 45 67"],
)
def test_phone_format_matrix(raw: str) -> None:
    assert EntityType.PHONE in _types(scan(f"телефон {raw}"))


def test_phone_not_detected_inside_long_digit_run() -> None:
    # «8…» в середине длинной цифровой строки не должно давать телефон
    assert EntityType.PHONE not in _types(scan("идентификатор 880123456789012 принят"))


def test_inn_11_digits_not_detected() -> None:
    # 11 цифр — ни ИНН ЮЛ (10), ни ИНН ФЛ (12)
    assert EntityType.INN not in _types(scan("номер 12345678901 в системе"))


def test_snils_accepts_space_and_dash_separator() -> None:
    assert EntityType.SNILS in _types(scan("СНИЛС 112-233-445 95"))
    assert EntityType.SNILS in _types(scan("СНИЛС 112-233-445-95"))


def test_scan_results_sorted_by_position() -> None:
    matches = scan("счёт 40702810500000000123, почта a@b.ru, ИНН 7707083893")
    assert matches == sorted(matches, key=lambda m: (m.start, m.end))


def test_duplicate_value_yields_separate_matches() -> None:
    emails = [m for m in scan("ivan@x.ru и снова ivan@x.ru") if m.type == EntityType.EMAIL]
    assert len(emails) == 2
    assert emails[0].start != emails[1].start


def test_passport_detector_is_broad_known_limitation() -> None:
    # Документированное ограничение regex-слоя: паспорт = \d{4}\s\d{6};
    # произвольная пара чисел тоже совпадает. Уточнение — NER (итерация 4).
    assert EntityType.PASSPORT in _types(scan("период 2024 123456 закрыт"))


def test_account_detector_is_broad_known_limitation() -> None:
    # Документированное ограничение: счёт = любые 20 цифр (контрольная сумма
    # счёта зависит от БИК; точная валидация — после MVP).
    assert EntityType.ACCOUNT in _types(scan("идентификатор 12345678901234567890"))


def test_inn_checksum_collision_known_limitation() -> None:
    # «Голое» число с валидной контрольной суммой ИНН помечается как ИНН,
    # даже если по смыслу им не является. Контекст уточняет NER (итерация 4).
    assert EntityType.INN in _types(scan("код 7707083893 в реестре"))


def test_person_false_positive_known_limitation() -> None:
    # Три слова с заглавной — эвристика ФИО; возможны ложные срабатывания,
    # снимаемые NER (итерация 4). confidence такого детектора понижен.
    assert EntityType.PERSON in _types(scan("Купить Молоко Хлеб"))


def test_pii_detectors_registered() -> None:
    from app.detectors.pii import pii_detectors

    names = {d.name for d in pii_detectors()}
    assert {"email", "phone", "inn", "snils", "ogrn", "passport"} <= names
