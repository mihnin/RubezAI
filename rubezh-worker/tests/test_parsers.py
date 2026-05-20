"""Unit-тесты парсеров PDF/DOCX. Фикстуры генерируются in-memory."""

from __future__ import annotations

from io import BytesIO

import pytest

# pylint: disable=wrong-import-position

# pypdf для генерации тестового PDF
from pypdf import PdfWriter
from reportlab.pdfgen import canvas  # type: ignore[import-untyped]


@pytest.fixture(autouse=True)
def _env_for_settings(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test")
    monkeypatch.setenv("MINIO_ROOT_USER", "rubezh")
    monkeypatch.setenv("MINIO_ROOT_PASSWORD", "rubezh-minio")


def _make_simple_pdf(lines: list[str]) -> bytes:
    """Создаёт PDF in-memory с заданными строками (по одной на строку)."""
    buf = BytesIO()
    c = canvas.Canvas(buf)
    y = 800
    for line in lines:
        c.drawString(50, y, line)
        y -= 20
    c.save()
    return buf.getvalue()


def _make_empty_pdf() -> bytes:
    """Минимальный валидный PDF без текста."""
    writer = PdfWriter()
    writer.add_blank_page(width=612, height=792)
    buf = BytesIO()
    writer.write(buf)
    return buf.getvalue()


def _make_docx(paragraphs: list[str]) -> bytes:
    """Создаёт DOCX in-memory с заданными параграфами."""
    from docx import Document  # type: ignore[import-untyped]

    doc = Document()
    for p in paragraphs:
        doc.add_paragraph(p)
    buf = BytesIO()
    doc.save(buf)
    return buf.getvalue()


def test_parse_pdf_extracts_lines() -> None:
    from app.parsers.pdf import parse_pdf

    # ASCII-текст: reportlab по умолчанию использует Helvetica, который
    # не содержит кириллицу. Проверка path extraction'а на латинице —
    # для UTF-8 production-PDF (Times/Calibri через MinIO) текст
    # извлекается корректно (проверено вручную).
    pdf = _make_simple_pdf([
        "Document first line",
        "Second line with data 12345",
        "Third line content",
    ])
    paragraphs = parse_pdf(pdf)
    full = " ".join(paragraphs)
    assert "Document first line" in full
    assert "12345" in full
    assert "Third line" in full


def test_parse_pdf_empty_returns_empty_list() -> None:
    from app.parsers.pdf import parse_pdf

    paragraphs = parse_pdf(_make_empty_pdf())
    assert paragraphs == []


def test_parse_pdf_invalid_raises() -> None:
    from app.parsers.pdf import parse_pdf

    with pytest.raises(ValueError):
        parse_pdf(b"not a PDF")


def test_parse_docx_extracts_paragraphs() -> None:
    from app.parsers.docx import parse_docx

    docx = _make_docx([
        "Первый параграф",
        "Второй параграф с числами 12345",
        "",  # пустой — должен быть отфильтрован
        "Третий параграф",
    ])
    paragraphs = parse_docx(docx)
    assert paragraphs == [
        "Первый параграф",
        "Второй параграф с числами 12345",
        "Третий параграф",
    ]


def test_parse_docx_invalid_raises() -> None:
    from app.parsers.docx import parse_docx

    with pytest.raises(ValueError):
        parse_docx(b"not a DOCX")


def test_parse_document_dispatch_by_filename() -> None:
    from app.parsers import parse_document

    pdf_bytes = _make_simple_pdf(["test pdf"])
    docx_bytes = _make_docx(["test docx"])

    assert parse_document(pdf_bytes, content_type=None, filename="x.pdf")
    assert parse_document(docx_bytes, content_type=None, filename="x.docx")


def test_parse_document_dispatch_by_content_type() -> None:
    from app.parsers import parse_document

    docx_bytes = _make_docx(["mime test"])
    paragraphs = parse_document(
        docx_bytes,
        content_type="application/vnd.openxmlformats-officedocument.wordprocessingml.document",
        filename="unknown",
    )
    assert paragraphs == ["mime test"]


def test_parse_document_unsupported_raises() -> None:
    from app.parsers import parse_document

    with pytest.raises(ValueError, match="неподдерживаемый формат"):
        parse_document(b"data", content_type="image/png", filename="x.png")
