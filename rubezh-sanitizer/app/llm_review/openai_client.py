"""OpenAI-совместимый клиент LLM-review (LM Studio, vLLM, Ollama, …).

Обращается к ``POST {url}/chat/completions`` локальной модели. Инварианты:
- **fail-open**: любая ошибка (сеть, таймаут, кривой JSON) → ``[]`` + лог в slog;
  доступность UX важнее полноты фильтра 2 (фильтр 1 уже отработал).
- raw-текст уходит только в **локальную** модель (trusted_local), наружу — нет.
"""

from __future__ import annotations

import json
import logging
import re

import httpx

from app.llm_review.client import LLMCandidate

_logger = logging.getLogger("rubezh-sanitizer")

# Просим модель вернуть строго JSON с дословными подстроками исходного текста.
_SYSTEM_PROMPT = (
    "Ты — анализатор утечек персональных данных и секретов в русских документах. "
    "Просканируй текст построчно и верни ВСЕ найденные чувствительные данные. "
    "Особое внимание удели тому, что часто пропускает regex: ИНН физлица "
    "(12 цифр подряд), номера банковских карт, пароли и секретные ключи "
    "(значение после слов «пароль», «ключ», «password» — даже если между словом "
    "и значением есть другие слова), СНИЛС, серию и номер паспорта.\n"
    "Допустимые значения поля type: PERSON, PHONE, EMAIL, PASSPORT, SNILS, INN, "
    "BANK_CARD, SECRET_PASSWORD, SECRET_API_KEY. Поле value — ДОСЛОВНАЯ подстрока "
    "исходного текста (копируй точь-в-точь, включая цифры и разделители).\n"
    'Пример: для «ИНН 770100100100, СНИЛС 123-456-789 00» верни {"entities":'
    '[{"type":"INN","value":"770100100100"},{"type":"SNILS","value":'
    '"123-456-789 00"}]}; для «пароль доступа: TestPass!2026-Secret-Key» — '
    '{"entities":[{"type":"SECRET_PASSWORD","value":"TestPass!2026-Secret-Key"}]}. '
    'Если ничего не найдено — {"entities":[]}.'
)

# Извлечение JSON-объекта из ответа (модели типа DeepSeek-R1 добавляют <think>…).
_JSON_OBJECT = re.compile(r"\{.*\}", re.DOTALL)

# Структурированный вывод: LM Studio / vLLM / Ollama принимают json_schema и
# грамматически форсят строгий JSON (в т. ч. у reasoning-моделей). Формат
# OpenAI-овского json_object LM Studio отвергает (400).
_RESPONSE_FORMAT = {
    "type": "json_schema",
    "json_schema": {
        "name": "sensitive_entities",
        "strict": True,
        "schema": {
            "type": "object",
            "properties": {
                "entities": {
                    "type": "array",
                    "items": {
                        "type": "object",
                        "properties": {
                            "type": {"type": "string"},
                            "value": {"type": "string"},
                        },
                        "required": ["type", "value"],
                        "additionalProperties": False,
                    },
                }
            },
            "required": ["entities"],
            "additionalProperties": False,
        },
    },
}


def parse_candidates(content: str) -> list[LLMCandidate]:
    """Парсит JSON-ответ модели в список кандидатов (терпимо к мусору вокруг).

    Возвращает ``[]`` при любом несоответствии формату — это часть fail-open.
    """
    match = _JSON_OBJECT.search(content)
    if not match:
        return []
    try:
        data = json.loads(match.group(0))
    except (json.JSONDecodeError, ValueError):
        return []
    raw_entities = data.get("entities") if isinstance(data, dict) else None
    if not isinstance(raw_entities, list):
        return []
    candidates: list[LLMCandidate] = []
    for item in raw_entities:
        if not isinstance(item, dict):
            continue
        type_ = item.get("type")
        value = item.get("value")
        if isinstance(type_, str) and isinstance(value, str) and value:
            candidates.append(LLMCandidate(type=type_, value=value))
    return candidates


class OpenAILLMReviewClient:
    """Клиент LLM-review к OpenAI-совместимому endpoint локальной модели."""

    def __init__(
        self, *, url: str, model: str, api_key: str = "", timeout: float = 5.0
    ) -> None:
        self._endpoint = url.rstrip("/") + "/chat/completions"
        self._model = model
        self._timeout = timeout
        self._headers = {"Content-Type": "application/json"}
        if api_key:
            self._headers["Authorization"] = f"Bearer {api_key}"

    def review(self, text: str) -> list[LLMCandidate]:
        """Запрашивает у модели смысловые кандидаты. Fail-open: ошибка → []."""
        payload = {
            "model": self._model,
            "messages": [
                {"role": "system", "content": _SYSTEM_PROMPT},
                {"role": "user", "content": text},
            ],
            "temperature": 0,
            "response_format": _RESPONSE_FORMAT,
        }
        try:
            response = httpx.post(
                self._endpoint,
                headers=self._headers,
                json=payload,
                timeout=self._timeout,
            )
            response.raise_for_status()
            content = response.json()["choices"][0]["message"]["content"]
        except (httpx.HTTPError, KeyError, ValueError, TypeError) as exc:
            _logger.warning(
                "LLM-review недоступен, fallback на фильтр 1 (fail-open): %s", exc
            )
            return []
        return parse_candidates(content)
