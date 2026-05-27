package chat

import (
	"strings"
	"testing"
)

// W2.2: review-loop должен видеть файлы. helpers — основа этой логики.

func TestParseAttachmentBlockExtractsCSV(t *testing.T) {
	// "name;age\n42" — base64 "bmFtZTthZ2UKNDI="
	block := "\n📎 Файлы:\n- [📎 cats.csv](data:text/csv;base64,bmFtZTthZ2UKNDI=)"
	atts := parseAttachmentBlock(block)
	if len(atts) != 1 {
		t.Fatalf("ожидался один файл, получено %d", len(atts))
	}
	if atts[0].Name != "cats.csv" {
		t.Errorf("Name = %q", atts[0].Name)
	}
	if atts[0].Mime != "text/csv" {
		t.Errorf("Mime = %q", atts[0].Mime)
	}
	if !strings.Contains(atts[0].TextPreview, "name") ||
		!strings.Contains(atts[0].TextPreview, "42") {
		t.Errorf("TextPreview не содержит CSV-содержимое: %q",
			atts[0].TextPreview)
	}
}

func TestParseAttachmentBlockBinaryMimeNoPreview(t *testing.T) {
	// xlsx — НЕ должно декодироваться в preview (бинарь).
	block := "📎 Файлы:\n- [📎 r.xlsx]" +
		"(data:application/vnd.openxmlformats-officedocument.spreadsheetml.sheet;base64,UEsDBBQAAAA=)"
	atts := parseAttachmentBlock(block)
	if len(atts) != 1 {
		t.Fatalf("len = %d", len(atts))
	}
	if atts[0].TextPreview != "" {
		t.Errorf("бинарный файл не должен иметь TextPreview: %q",
			atts[0].TextPreview)
	}
	if atts[0].SizeBytes <= 0 {
		t.Errorf("SizeBytes не вычислен: %d", atts[0].SizeBytes)
	}
}

func TestParseAttachmentBlockFallbackByExtension(t *testing.T) {
	// octet-stream + .txt → должен попасть в preview (fallback по ext).
	block := "📎 Файлы:\n- [📎 a.txt](data:application/octet-stream;base64,aGVsbG8=)"
	atts := parseAttachmentBlock(block)
	if len(atts) != 1 || atts[0].TextPreview != "hello" {
		t.Errorf("fallback по расширению не сработал: %+v", atts)
	}
}

// W4.4 (MN-3 ревью W3): два последовательных файла в одном блоке
// разбираются корректно — lazy regex останавливается на первой `)` и
// не съедает разделитель `\n- [📎` следующей ссылки.
func TestParseAttachmentBlockTwoFiles(t *testing.T) {
	block := "📎 Файлы:\n" +
		"- [📎 a.csv](data:text/csv;base64,YQ==)\n" +
		"- [📎 b.txt](data:text/plain;base64,Yg==)"
	atts := parseAttachmentBlock(block)
	if len(atts) != 2 {
		t.Fatalf("ожидалось 2 файла, получено %d: %+v", len(atts), atts)
	}
	if atts[0].Name != "a.csv" || atts[0].TextPreview != "a" {
		t.Errorf("первый файл: %+v", atts[0])
	}
	if atts[1].Name != "b.txt" || atts[1].TextPreview != "b" {
		t.Errorf("второй файл: %+v", atts[1])
	}
}

// W4.4 (MN-2 ревью W3): base64 с переносами строк (RFC 4648 §3.1 —
// типично для крупных artifacts) декодируется корректно.
func TestParseAttachmentBlockBase64Newlines(t *testing.T) {
	// "ab\ncd" — base64 = "YWIKY2Q="; разорвём по \n внутри base64-payload.
	block := "[📎 multi.txt](data:text/plain;base64,YWIK\nY2Q=)"
	atts := parseAttachmentBlock(block)
	if len(atts) != 1 {
		t.Fatalf("ожидался 1 файл, получено %d", len(atts))
	}
	// После cleanup whitespace декодер вернёт "ab\ncd".
	if atts[0].TextPreview != "ab\ncd" {
		t.Errorf("preview = %q, ожидалось %q", atts[0].TextPreview, "ab\ncd")
	}
}

func TestParseAttachmentBlockEmpty(t *testing.T) {
	if got := parseAttachmentBlock(""); got != nil {
		t.Errorf("пустой блок → nil, получено %+v", got)
	}
	if got := parseAttachmentBlock("просто текст без файлов"); len(got) != 0 {
		t.Errorf("блок без файлов → 0 элементов, получено %d", len(got))
	}
}

func TestDescribeAttachmentsForReviewer(t *testing.T) {
	atts := []reviewAttachment{
		{Name: "a.csv", Mime: "text/csv", SizeBytes: 42, TextPreview: "n;v\n1;2"},
		{Name: "b.png", Mime: "image/png", SizeBytes: 1024},
	}
	out := describeAttachmentsForReviewer(atts)
	for _, want := range []string{
		"a.csv", "text/csv", "42 B",
		"b.png", "image/png", "1024 B",
		"n;v", "содержимое",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("описание не содержит %q:\n%s", want, out)
		}
	}
	// Для бинарного файла preview-блока быть НЕ должно.
	if strings.Contains(out, "UEs") {
		t.Errorf("бинарный preview просочился: %s", out)
	}
}

func TestDescribeAttachmentsEmpty(t *testing.T) {
	if got := describeAttachmentsForReviewer(nil); got != "" {
		t.Errorf("пустой список → пустая строка, получено %q", got)
	}
}

func TestLimitRunes(t *testing.T) {
	if got := limitRunes("hi", 10); got != "hi" {
		t.Errorf("короткая строка не должна обрезаться: %q", got)
	}
	if got := limitRunes("привет мир", 6); got != "привет..." {
		t.Errorf("обрезка по рунам: %q", got)
	}
}

func TestApproxBase64BytesLen(t *testing.T) {
	cases := map[string]int{
		"":         0,
		"YQ==":     1, // "a"
		"YWI=":     2, // "ab"
		"YWJj":     3, // "abc"
		"YWJjZA==": 4, // "abcd"
	}
	for in, want := range cases {
		if got := approxBase64BytesLen(in); got != want {
			t.Errorf("approxBase64BytesLen(%q) = %d, ожидалось %d",
				in, got, want)
		}
	}
}
