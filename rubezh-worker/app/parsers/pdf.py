"""PDF-парсер на pypdf. Извлекает текст постранично, формирует параграфы.

Ограничения MVP:
- Без OCR (сканированные PDF дадут пустые параграфы; mitigation —
  возвращаем пустой список, worker помечает документ failed).
- Без extract тегов/формы (form fields, аннотации игнорируются).
- pypdf терпим к битым PDF, но падает на encrypted-без-пароля → ValueError.
"""

from __future__ import annotations

from io import BytesIO

from pypdf import PdfReader


def parse_pdf(content: bytes) -> list[str]:
    """Извлекает параграфы из PDF.

    Алгоритм:
    1. PdfReader на BytesIO буфере.
    2. Для каждой страницы: extract_text() → split по двойному
       переносу (типичная heuristic для параграфов).
    3. Trim+отфильтровать пустые.

    Returns: list[str] непустых параграфов в порядке появления.

    Raises:
        ValueError: PDF encrypted (с requires_password=True).
    """
    try:
        reader = PdfReader(BytesIO(content))
    except Exception as e:
        raise ValueError(f"PDF не читается: {e}") from e

    if reader.is_encrypted:
        # Попытка пустым паролем — некоторые "encrypted-no-password"
        # PDF открываются. Если нет — ValueError.
        try:
            if not reader.decrypt(""):
                raise ValueError("PDF зашифрован паролем")
        except Exception as e:
            raise ValueError(f"PDF зашифрован паролем: {e}") from e

    paragraphs: list[str] = []
    for page in reader.pages:
        text = page.extract_text() or ""
        for raw in text.split("\n\n"):
            trimmed = raw.strip()
            if trimmed:
                paragraphs.append(trimmed)
    return paragraphs
