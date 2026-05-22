package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// TestUserProviderKeyAADSwapProtection — ключ, зашифрованный для (user,provider),
// не расшифровывается под другим user или другим provider (защита от подмены).
func TestUserProviderKeyAADSwapProtection(t *testing.T) {
	c := testCipher(t)
	u1, p1 := "user-1", "prov-1"
	ct, err := c.Encrypt([]byte("sk-secret"), userProviderKeyAAD(u1, p1))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if got, err := c.Decrypt(ct, userProviderKeyAAD(u1, p1)); err != nil ||
		string(got) != "sk-secret" {
		t.Fatalf("свой AAD должен расшифровывать: %v %q", err, got)
	}
	if _, err := c.Decrypt(ct, userProviderKeyAAD("user-2", p1)); err == nil {
		t.Error("чужой user не должен расшифровывать ключ")
	}
	if _, err := c.Decrypt(ct, userProviderKeyAAD(u1, "prov-2")); err == nil {
		t.Error("другой provider не должен расшифровывать ключ")
	}
}

// TestMyCredentialLifecycle — create → list (без ключа) → delete через API.
func TestMyCredentialLifecycle(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()

	// провайдер для привязки
	prov := createTestProvider(t, router)

	body, _ := json.Marshal(createUserCredentialRequest{
		ProviderID: prov.ID, APIKey: "sk-personal-" + strconv.FormatInt(
			time.Now().UnixNano(), 36),
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/me/credentials",
		bytes.NewReader(body))
	req.Header.Set("Authorization", userToken())
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST credential: code=%d (%s)", rec.Code, rec.Body)
	}

	// list — ключ присутствует (has_key), но сам ключ не отдаётся
	lr := httptest.NewRecorder()
	lreq := httptest.NewRequest(http.MethodGet, "/api/me/credentials", nil)
	lreq.Header.Set("Authorization", userToken())
	router.ServeHTTP(lr, lreq)
	if lr.Code != http.StatusOK {
		t.Fatalf("GET credentials: code=%d", lr.Code)
	}
	var list []userCredentialDTO
	if err := json.Unmarshal(lr.Body.Bytes(), &list); err != nil {
		t.Fatalf("ответ не JSON: %v", err)
	}
	var found *userCredentialDTO
	for i := range list {
		if list[i].ProviderID == prov.ID {
			found = &list[i]
		}
	}
	if found == nil || !found.HasKey {
		t.Fatalf("персональный ключ не найден в списке: %+v", list)
	}
	if bytes.Contains(lr.Body.Bytes(), []byte("sk-personal")) {
		t.Error("ключ НЕ должен возвращаться в API")
	}

	// delete
	dr := httptest.NewRecorder()
	dreq := httptest.NewRequest(http.MethodDelete,
		"/api/me/credentials/"+found.ID, nil)
	dreq.Header.Set("Authorization", userToken())
	router.ServeHTTP(dr, dreq)
	if dr.Code != http.StatusNoContent {
		t.Fatalf("DELETE: code=%d", dr.Code)
	}
}
