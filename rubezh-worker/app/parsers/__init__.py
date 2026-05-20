"""Парсеры документов: PDF (pypdf) и DOCX (python-docx).

Регистр в `parse_document` — по MIME-типу или расширению. Возвращает
список параграфов (строк). Дальнейший chunking (`app/chunking.py`)
склеивает их до целевого размера ~800 токенов.
"""

from __future__ import annotations

from .docx import parse_docx
from .pdf import parse_pdf

__all__ = ["parse_document", "parse_pdf", "parse_docx"]


def parse_document(content: bytes, *, content_type: str | None,
                   filename: str) -> list[str]:
    """Выбирает парсер по content_type / расширению filename.

    Возвращает список параграфов; не-текстовое содержимое (изображения,
    embedded shapes) пропускается. Пустые параграфы отсеиваются.

    Raises:
        ValueError: для неподдерживаемого формата.
    """
    fname = filename.lower()
    ct = (content_type or "").lower()
    if "pdf" in ct or fname.endswith(".pdf"):
        return parse_pdf(content)
    if ("officedocument.wordprocessingml" in ct or fname.endswith(".docx")):
        return parse_docx(content)
    raise ValueError(
        f"неподдерживаемый формат: content_type={content_type!r}, filename={filename!r}"
    )
