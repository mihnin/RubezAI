package chat

import (
	"context"
	"testing"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
)

func TestPreviewCachePutConsumeOnce(t *testing.T) {
	c := NewPreviewCache(time.Minute)
	token, err := c.put(previewResult{preview: maskedPreview()}, "u-1", "s-1")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	res, ok := c.consume(token, "u-1")
	if !ok {
		t.Fatal("первый consume должен вернуть запись")
	}
	if res.preview.SanitizedText != "Звонил ФИО_001" {
		t.Errorf("неверный закэшированный текст: %q", res.preview.SanitizedText)
	}
	if _, ok := c.consume(token, "u-1"); ok {
		t.Error("повторный consume должен провалиться (одноразовость)")
	}
}

func TestPreviewCacheOwnerMismatch(t *testing.T) {
	c := NewPreviewCache(time.Minute)
	token, _ := c.put(previewResult{preview: maskedPreview()}, "u-1", "s-1")
	if _, ok := c.consume(token, "u-2"); ok {
		t.Error("consume чужим пользователем должен провалиться")
	}
}

func TestPreviewCacheExpired(t *testing.T) {
	c := NewPreviewCache(-time.Second) // истекает в прошлом
	token, _ := c.put(previewResult{preview: maskedPreview()}, "u-1", "s-1")
	if _, ok := c.consume(token, "u-1"); ok {
		t.Error("просроченный токен не должен потребляться")
	}
}

func TestOrchestratorPreviewReturnsTokenAndMasked(t *testing.T) {
	san := &fakeSanitizer{resp: maskedPreview()}
	orch := NewOrchestrator(san, &fakeLLM{}, &fakeStore{}, nil)
	preview, token, err := orch.Preview(context.Background(), baseRequest())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if token == "" {
		t.Error("Preview должен вернуть непустой preview_token")
	}
	if preview.SanitizedText != "Звонил ФИО_001" {
		t.Errorf("неверный обезличенный текст: %q", preview.SanitizedText)
	}
}

// TestPrepareReusesPreviewTokenIsDeterministic — ключевой инвариант J.0:
// при наличии токена Prepare использует ЗАКЭШИРОВАННЫЙ sanitize, а не новый.
// Доказательство: после Preview подменяем ответ санитайзера на другой — Prepare
// с токеном обязан записать ИСХОДНЫЙ обезличенный текст (что подтвердил юзер).
func TestPrepareReusesPreviewTokenIsDeterministic(t *testing.T) {
	san := &fakeSanitizer{resp: maskedPreview()}
	store := &fakeStore{}
	orch := NewOrchestrator(san, &fakeLLM{}, store, nil)
	req := baseRequest()

	_, token, err := orch.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}

	// Имитируем недетерминизм фильтра 2: санитайзер теперь вернул бы ДРУГОЕ.
	san.resp = sanitizer.PreviewResponse{
		SanitizedText: "СОВСЕМ ДРУГОЙ ТЕКСТ",
		Risk:          sanitizer.Risk{Level: "low", Score: 0.1, Classes: []string{"pii"}},
	}

	req.PreviewToken = token
	if _, err := orch.Prepare(context.Background(), req); err != nil {
		t.Fatalf("Prepare с токеном: %v", err)
	}
	if len(store.requests) != 1 {
		t.Fatalf("ожидалась 1 запись chat_request, получено %d", len(store.requests))
	}
	if got := store.requests[0].UserContent; got != "Звонил ФИО_001" {
		t.Errorf("Prepare использовал не закэшированный sanitize: UserContent=%q", got)
	}
}

// TestPrepareWithoutTokenSanitizesFresh — без токена Prepare делает свежий
// sanitize (обычный путь без гейта).
func TestPrepareWithoutTokenSanitizesFresh(t *testing.T) {
	san := &fakeSanitizer{resp: maskedPreview()}
	store := &fakeStore{}
	orch := NewOrchestrator(san, &fakeLLM{}, store, nil)
	if _, err := orch.Prepare(context.Background(), baseRequest()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(store.requests) != 1 || store.requests[0].UserContent != "Звонил ФИО_001" {
		t.Error("свежий sanitize-путь сломан")
	}
}
