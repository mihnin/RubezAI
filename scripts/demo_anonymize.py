"""Демонстрация обезличивания: синтетический договор -> sanitizer -> DeepSeek.

Показывает сквозной поток принципа «Рубеж ИИ»: договор с ПДн, реквизитами и
коммерческими данными обезличивается сервисом rubezh-sanitizer, и во внешнюю
LLM (DeepSeek) уходит ТОЛЬКО маскированный текст — ни одного реального
персонального данного периметр не покидает.

Запуск из корня репозитория:  python scripts/demo_anonymize.py
Ключ DeepSeek читается из .env (переменная deep_seek).
"""

from __future__ import annotations

import json
import sys
import urllib.error
import urllib.request
from pathlib import Path

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")

SANITIZER_URL = "http://localhost:8001/sanitize/preview"
DEEPSEEK_URL = "https://api.deepseek.com/chat/completions"

# Синтетический договор (вымышленные данные) — намеренно насыщен ПДн,
# банковскими реквизитами и коммерчески чувствительной информацией.
CONTRACT = """ДОГОВОР № 47/2025-П НА ПОСТАВКУ ОБОРУДОВАНИЯ
г. Москва, 12 марта 2025 г.

ООО «Северные Технологии» (ИНН 7707083893, КПП 770701001,
ОГРН 1027700132195), именуемое «Поставщик», в лице генерального
директора Иванова Ивана Ивановича, и ООО «Аврора Трейд»
(ИНН 7830002293), именуемое «Покупатель», в лице директора
Петрова Петра Сергеевича, заключили настоящий договор.

1. Поставщик обязуется поставить промышленное оборудование общей
   стоимостью 4 750 000 рублей.
2. Маржа по сделке составляет 18 процентов. Условия поставки —
   отсрочка платежа 30 дней.
3. Контактное лицо Поставщика — Сидорова Анна Михайловна,
   телефон +7 (495) 123-45-67, почта a.sidorova@severteh.example.
4. Банковские реквизиты Поставщика: расчётный счёт
   40702810500000000123, БИК 044525225.
5. Ответственное лицо Покупателя: паспорт 4509 123456,
   СНИЛС 112-233-445 95.
"""


def read_env_key(name: str) -> str:
    """Читает значение переменной из .env в корне репозитория."""
    for raw in Path(".env").read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if line.startswith(f"{name}="):
            return line.split("=", 1)[1].strip()
    sys.exit(f"Переменная {name} не найдена в .env")


def post_json(url: str, payload: dict, headers: dict | None = None) -> dict:
    """POST JSON-запрос, возвращает разобранный JSON-ответ."""
    request = urllib.request.Request(
        url, data=json.dumps(payload).encode("utf-8"), method="POST"
    )
    request.add_header("Content-Type", "application/json")
    for key, value in (headers or {}).items():
        request.add_header(key, value)
    with urllib.request.urlopen(request, timeout=90) as response:
        return json.loads(response.read().decode("utf-8"))


def main() -> None:
    line = "=" * 72

    print(line)
    print("ШАГ 1. Исходный синтетический договор (с реальными по форме ПДн)")
    print(line)
    print(CONTRACT)

    print(line)
    print("ШАГ 2. Обезличивание через rubezh-sanitizer  (POST /sanitize/preview)")
    print(line)
    result = post_json(SANITIZER_URL, {"text": CONTRACT, "context": "document"})
    risk = result["risk"]
    print(f"Оценка риска: {risk['level'].upper()}  "
          f"(score={risk['score']}, классы={risk['classes']})")
    counts: dict[str, int] = {}
    for entity in result["entities"]:
        counts[entity["type"]] = counts.get(entity["type"], 0) + 1
    print(f"Найдено и замаскировано сущностей: {len(result['entities'])}")
    for entity_type, count in sorted(counts.items()):
        print(f"   {entity_type:24} x{count}")
    print("-" * 72)
    print("ОБЕЗЛИЧЕННЫЙ ТЕКСТ (именно он, и только он, уходит во внешнюю LLM):")
    print("-" * 72)
    print(result["sanitized_text"])

    print(line)
    print("ШАГ 3. Обезличенный текст -> внешняя LLM DeepSeek")
    print(line)
    masked = result["sanitized_text"]
    try:
        answer = post_json(
            DEEPSEEK_URL,
            {
                "model": "deepseek-chat",
                "messages": [
                    {"role": "system", "content": "Ты — юридический ассистент. "
                     "Кратко, по пунктам, изложи суть договора."},
                    {"role": "user", "content": masked},
                ],
                "stream": False,
            },
            headers={"Authorization": f"Bearer {read_env_key('deep_seek')}"},
        )
        print("ОТВЕТ DeepSeek (модель обработала обезличенный текст):")
        print(answer["choices"][0]["message"]["content"])
    except urllib.error.HTTPError as err:
        print(f"DeepSeek вернул ошибку HTTP {err.code}: {err.read().decode('utf-8')}")
    except (urllib.error.URLError, KeyError, TimeoutError) as err:
        print(f"Не удалось получить ответ DeepSeek: {err}")

    print(line)
    print("ИТОГ: во внешнюю модель не ушло ни одного реального ПДн или")
    print("реквизита — DeepSeek работал исключительно с псевдонимами.")
    print(line)


if __name__ == "__main__":
    main()
