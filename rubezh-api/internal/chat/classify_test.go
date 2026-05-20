package chat

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyLLMError(t *testing.T) {
	cases := []struct {
		name, errStr, content, wantContains string
	}{
		{"empty content, no err", "", "", "пустой"},
		{"401 unauthorized", "openai: HTTP 401: unauthorized", "", "API-ключ"},
		{"429 rate limit", "openai: HTTP 429: too many requests", "", "лимит"},
		{"timeout", "context deadline exceeded", "", "таймаут"},
		{"no such host", "Get https://api.x: dial tcp: lookup api.x: no such host", "", "недоступен"},
		{"404 model", "openai: HTTP 404: model not found", "", "не найдена"},
		{"tls", "tls: handshake failure", "", "TLS"},
		{"http 503", "openai: HTTP 503: service unavailable", "", "5xx"},
		{"generic", "что-то странное", "", "ошибка вызова"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var err error
			if c.errStr != "" {
				err = errors.New(c.errStr)
			}
			got := classifyLLMError(err, c.content)
			if got == "" {
				t.Fatalf("пустой результат")
			}
			if !strings.Contains(got, c.wantContains) {
				t.Errorf("got %q, ожидалась подстрока %q", got, c.wantContains)
			}
		})
	}
}
