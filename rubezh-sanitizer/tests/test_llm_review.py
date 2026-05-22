"""Unit-тесты LLM-review (фильтр 2/3): детектор-адаптер и OpenAI-клиент."""

from __future__ import annotations

import httpx
import pytest

from app.domain.entities import Category, EntityType
from app.llm_review.client import LLMCandidate, MockLLMReviewClient
from app.llm_review.detector import LLMReviewDetector
from app.llm_review.openai_client import OpenAILLMReviewClient, parse_candidates


class _StubClient:
    """Клиент LLM-review с заранее заданными кандидатами."""

    def __init__(self, candidates: list[LLMCandidate]) -> None:
        self._candidates = candidates

    def review(self, text: str) -> list[LLMCandidate]:
        return self._candidates


# --- детектор-адаптер ---


def test_mock_client_yields_no_matches() -> None:
    detector = LLMReviewDetector(MockLLMReviewClient())
    assert detector.detect("Иванов Иван, пароль: secret") == []


def test_detector_maps_candidate_to_match_with_correct_span() -> None:
    text = "паспорт серии 4501 № 234567 выдан"
    detector = LLMReviewDetector(
        _StubClient([LLMCandidate(type="PASSPORT", value="4501 № 234567")])
    )
    matches = detector.detect(text)
    assert len(matches) == 1
    match = matches[0]
    assert match.type is EntityType.PASSPORT
    assert match.category is Category.PII
    assert match.detector == "llm_review"
    assert text[match.start : match.end] == "4501 № 234567"
    assert match.confidence < 1.0


def test_detector_classifies_secret_category() -> None:
    detector = LLMReviewDetector(
        _StubClient([LLMCandidate(type="SECRET_PASSWORD", value="TestPass!2026")])
    )
    match = detector.detect("пароль: TestPass!2026 (NDA)")[0]
    assert match.category is Category.SECRET


def test_detector_skips_unknown_type() -> None:
    detector = LLMReviewDetector(
        _StubClient([LLMCandidate(type="MADE_UP", value="что-то")])
    )
    assert detector.detect("что-то здесь") == []


def test_detector_skips_value_absent_from_text() -> None:
    detector = LLMReviewDetector(
        _StubClient([LLMCandidate(type="EMAIL", value="ghost@example.com")])
    )
    assert detector.detect("здесь нет такого адреса") == []


def test_detector_emits_match_per_occurrence() -> None:
    detector = LLMReviewDetector(
        _StubClient([LLMCandidate(type="PHONE", value="+79161234567")])
    )
    matches = detector.detect("звоните +79161234567 или +79161234567")
    assert len(matches) == 2
    assert matches[0].start != matches[1].start


# --- парсер ответа модели ---


def test_parse_candidates_plain_json() -> None:
    content = '{"entities":[{"type":"INN","value":"770100100100"}]}'
    candidates = parse_candidates(content)
    assert candidates == [LLMCandidate(type="INN", value="770100100100")]


def test_parse_candidates_strips_reasoning_wrapper() -> None:
    # модели семейства R1 добавляют <think>…</think> вокруг ответа
    content = (
        "<think>вижу ИНН физлица</think>\n"
        '{"entities":[{"type":"INN","value":"770100100100"}]}'
    )
    assert parse_candidates(content) == [LLMCandidate(type="INN", value="770100100100")]


@pytest.mark.parametrize("content", ["не json вовсе", "{сломанный json}", '{"x":1}'])
def test_parse_candidates_garbage_returns_empty(content: str) -> None:
    assert parse_candidates(content) == []


def test_parse_candidates_skips_malformed_items() -> None:
    content = '{"entities":[{"type":"INN"},{"type":"EMAIL","value":"a@b.ru"}]}'
    assert parse_candidates(content) == [LLMCandidate(type="EMAIL", value="a@b.ru")]


# --- OpenAI-совместимый клиент: fail-open и успешный путь ---


def test_openai_client_fail_open_on_network_error(monkeypatch: pytest.MonkeyPatch) -> None:
    def _raise(*args: object, **kwargs: object) -> object:
        raise httpx.ConnectError("LM Studio недоступна")

    monkeypatch.setattr("app.llm_review.openai_client.httpx.post", _raise)
    client = OpenAILLMReviewClient(url="http://localhost:1234/v1", model="m")
    assert client.review("любой текст") == []


def test_openai_client_parses_successful_response(monkeypatch: pytest.MonkeyPatch) -> None:
    class _Resp:
        def raise_for_status(self) -> None:
            return None

        def json(self) -> dict:
            body = '{"entities":[{"type":"SNILS","value":"123-456-789 00"}]}'
            return {"choices": [{"message": {"content": body}}]}

    captured: dict[str, object] = {}

    def _post(endpoint: str, **kwargs: object) -> _Resp:
        captured["endpoint"] = endpoint
        captured["json"] = kwargs.get("json")
        return _Resp()

    monkeypatch.setattr("app.llm_review.openai_client.httpx.post", _post)
    client = OpenAILLMReviewClient(url="http://localhost:1234/v1/", model="deepseek")
    candidates = client.review("СНИЛС 123-456-789 00")
    assert candidates == [LLMCandidate(type="SNILS", value="123-456-789 00")]
    # url нормализуется (без двойного слэша) и бьёт в /chat/completions
    assert captured["endpoint"] == "http://localhost:1234/v1/chat/completions"
