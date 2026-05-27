package chat

import (
	"encoding/base64"
	"regexp"
	"strings"
)

// review_attachments.go — helpers для W2.2 (review-loop видит файлы):
// разбор `[📎 имя](data:mime;base64,...)`-блока, mime-классификация для
// решения нужен ли preview, base64-декод и обрезка по рунам.
//
// Изолировано в отдельный файл, чтобы держать orchestrator_review.go
// компактным и упростить unit-тесты helper'ов.

// MN-3 ревью W2: верхний лимит размера блока attachments перед regex-
// проходом. Защита от CPU-spike на запросе с гигантскими artifacts
// (мы всё равно обрезаем preview до 2KB; данные сверх лимита
// просто игнорируются на review-стороне).
const _maxAttachmentBlockBytes = 8 * 1024 * 1024 // 8 MB raw markdown

// Regex с поддержкой переносов строк внутри base64 (RFC 4648 §3.1 —
// типично для крупных artifacts из codex/claude). Whitespace в base64-
// группе вычищается через base64.StdEncoding.DecodeString (он
// разрешает '\n' внутри), здесь же — захватываем \s в charset.
var attachmentLinkRE = regexp.MustCompile(
	`\[📎\s*([^\]]+)\]\(data:([^;]+);base64,([A-Za-z0-9+/=\s]+?)\)`)

func attachmentLinkRegexp() *regexp.Regexp { return attachmentLinkRE }

// approxBase64BytesLen — длина декодированных байт без аллокации
// (3 байта на каждые 4 base64-символа, минус padding).
func approxBase64BytesLen(b64 string) int {
	n := len(b64)
	if n == 0 {
		return 0
	}
	pad := 0
	if strings.HasSuffix(b64, "==") {
		pad = 2
	} else if strings.HasSuffix(b64, "=") {
		pad = 1
	}
	return n/4*3 - pad
}

// textMimePrefixes / textMimeExact — белый список mime-типов, для
// которых safe декодировать base64 в string и показывать ревизорам
// как plain-text preview. Бинарные форматы (xlsx, pdf, png, …)
// проходят как метаданные без раскрытия содержимого.
var textMimePrefixes = []string{
	"text/",
	"application/json",
	"application/xml",
	"application/yaml",
	"application/x-yaml",
	"application/sql",
}

// textExtensions — fallback, когда mime пришёл как
// application/octet-stream (codex/claude иногда не угадывают тип).
var textExtensions = map[string]bool{
	".csv": true, ".tsv": true, ".txt": true, ".md": true,
	".json": true, ".yaml": true, ".yml": true, ".xml": true,
	".html": true, ".htm": true, ".sql": true,
	".py": true, ".js": true, ".ts": true, ".go": true,
	".sh": true, ".env": true, ".log": true,
}

func isTextMimeForReview(mime, filename string) bool {
	m := strings.ToLower(mime)
	for _, p := range textMimePrefixes {
		if strings.HasPrefix(m, p) {
			return true
		}
	}
	// Fallback по расширению (octet-stream и т.п.).
	lower := strings.ToLower(filename)
	if i := strings.LastIndex(lower, "."); i >= 0 {
		return textExtensions[lower[i:]]
	}
	return false
}

func base64DecodeForReview(s string) ([]byte, error) {
	// MN-2: убираем переносы/пробелы из base64 перед декодом (RFC 4648
	// §3.1: декодер допускает, но не для каждого варианта Encoding).
	clean := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	return base64.StdEncoding.DecodeString(clean)
}

func limitRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
