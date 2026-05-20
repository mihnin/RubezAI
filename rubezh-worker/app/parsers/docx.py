"""DOCX-парсер на python-docx. Извлекает параграфы по docx-структуре.

Ограничения MVP:
- Только основной текст (заголовки + параграфы); таблицы пропускаются.
- Без headers/footers (typically boilerplate).
- Embedded objects (изображения, shapes) игнорируются.
"""

from __future__ import annotations

from io import BytesIO

from docx import Document  # type: ignore[import-untyped]


def parse_docx(content: bytes) -> list[str]:
    """Извлекает параграфы из DOCX.

    Returns: list[str] непустых параграфов в порядке появления.

    Raises:
        ValueError: повреждённый DOCX (не распознан как ZIP).
    """
    try:
        doc = Document(BytesIO(content))
    except Exception as e:
        raise ValueError(f"DOCX не читается: {e}") from e

    paragraphs: list[str] = []
    for para in doc.paragraphs:
        text = (para.text or "").strip()
        if text:
            paragraphs.append(text)
    return paragraphs
