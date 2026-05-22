package chat

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
)

// _previewTTL — время жизни закэшированного результата предпросмотра.
const _previewTTL = 10 * time.Minute

// previewResult — закэшированный результат ЕДИНСТВЕННОГО sanitize для гейта
// предпросмотра (J.0). Переиспользуется в Prepare → «подтверждён ровно тот
// текст, что уйдёт» (детерминизм preview↔chat: фильтр 2 fail-open и
// недетерминирован, повторный sanitize мог бы сдвинуть нумерацию псевдонимов).
// Содержит pmap с raw-значениями в памяти — НЕ логировать и НЕ персистить.
type previewResult struct {
	preview sanitizer.PreviewResponse
	pmap    PseudonymMap
}

type previewEntry struct {
	result    previewResult
	userID    string
	sessionID string
	expires   time.Time
}

// PreviewCache — RAM-кэш результатов sanitize с TTL, привязкой к владельцу и
// одноразовым consume. Только в памяти процесса.
type PreviewCache struct {
	mu      sync.Mutex
	entries map[string]previewEntry
	ttl     time.Duration
}

// NewPreviewCache создаёт пустой кэш с заданным TTL.
func NewPreviewCache(ttl time.Duration) *PreviewCache {
	return &PreviewCache{entries: map[string]previewEntry{}, ttl: ttl}
}

func newPreviewToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("chat: генерация preview_token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// put кэширует результат и возвращает непрозрачный одноразовый токен.
func (c *PreviewCache) put(
	res previewResult, userID, sessionID string,
) (string, error) {
	token, err := newPreviewToken()
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
	c.entries[token] = previewEntry{
		result: res, userID: userID, sessionID: sessionID,
		expires: time.Now().Add(c.ttl),
	}
	return token, nil
}

// consume извлекает и УДАЛЯЕТ запись (одноразово); запись отдаётся, только
// если токен принадлежит тому же пользователю и не истёк (иначе ok=false).
func (c *PreviewCache) consume(token, userID string) (previewResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[token]
	if !ok {
		return previewResult{}, false
	}
	delete(c.entries, token)
	if e.userID != userID || time.Now().After(e.expires) {
		return previewResult{}, false
	}
	return e.result, true
}

// pruneLocked удаляет истёкшие записи (вызывается под mu).
func (c *PreviewCache) pruneLocked() {
	now := time.Now()
	for k, e := range c.entries {
		if now.After(e.expires) {
			delete(c.entries, k)
		}
	}
}
