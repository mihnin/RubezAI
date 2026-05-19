package sanitizer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// validResponse — корректное тело ответа /sanitize/preview для тестов.
const validResponse = `{"sanitized_text":"Звонил ТЕЛЕФОН_001",` +
	`"entities":[{"type":"PHONE","category":"pii","start":7,"end":10,` +
	`"pseudonym":"ТЕЛЕФОН_001","raw_hash":"abc","confidence":0.9,` +
	`"detector":"regex"}],"risk":{"score":0.5,"level":"medium",` +
	`"classes":["pii"]},"mapping_id":null}`

// fakeSanitizer — поддельный endpoint /sanitize/preview для тестов.
func fakeSanitizer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/sanitize/preview" {
				t.Errorf("путь = %q, ожидался /sanitize/preview", r.URL.Path)
			}
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}))
}

func TestClientPreviewSuccess(t *testing.T) {
	server := fakeSanitizer(t, http.StatusOK, validResponse)
	defer server.Close()

	resp, err := NewClient(server.URL).Preview(context.Background(), PreviewRequest{
		Text: "Звонил +7 900", Context: "chat",
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if resp.SanitizedText != "Звонил ТЕЛЕФОН_001" {
		t.Errorf("SanitizedText = %q", resp.SanitizedText)
	}
	if len(resp.Entities) != 1 || resp.Entities[0].Type != "PHONE" {
		t.Fatalf("entities разобраны некорректно: %+v", resp.Entities)
	}
	if resp.Entities[0].Start != 7 || resp.Entities[0].End != 10 {
		t.Errorf("спан сущности разобран некорректно: %+v", resp.Entities[0])
	}
	if resp.Risk.Level != "medium" || len(resp.Risk.Classes) != 1 {
		t.Errorf("risk разобран некорректно: %+v", resp.Risk)
	}
	if resp.MappingID != nil {
		t.Errorf("mapping_id должен быть nil, получено %v", resp.MappingID)
	}
}

func TestClientPreviewSendsCorrectRequest(t *testing.T) {
	var got PreviewRequest
	var method, contentType, accept string
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			method = r.Method
			contentType = r.Header.Get("Content-Type")
			accept = r.Header.Get("Accept")
			_ = json.NewDecoder(r.Body).Decode(&got)
			_, _ = io.WriteString(w, validResponse)
		}))
	defer server.Close()

	_, _ = NewClient(server.URL).Preview(context.Background(), PreviewRequest{
		Text: "текст", Context: "chat",
	})
	if method != http.MethodPost {
		t.Errorf("метод = %s, ожидался POST", method)
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q", contentType)
	}
	if accept != "application/json" {
		t.Errorf("Accept = %q, ожидался application/json", accept)
	}
	if got.Text != "текст" || got.Context != "chat" {
		t.Errorf("тело запроса некорректно: %+v", got)
	}
}

func TestClientPreviewSendsDocumentID(t *testing.T) {
	var raw map[string]any
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&raw)
			_, _ = io.WriteString(w, validResponse)
		}))
	defer server.Close()

	docID := "11111111-1111-1111-1111-111111111111"
	_, _ = NewClient(server.URL).Preview(context.Background(), PreviewRequest{
		Text: "x", Context: "document", DocumentID: &docID,
	})
	if raw["document_id"] != docID {
		t.Errorf("document_id = %v, ожидался %s", raw["document_id"], docID)
	}
}

func TestClientPreviewNon200(t *testing.T) {
	server := fakeSanitizer(t, http.StatusInternalServerError,
		`{"detail":"внутренняя ошибка sanitizer"}`)
	defer server.Close()

	_, err := NewClient(server.URL).Preview(context.Background(),
		PreviewRequest{Text: "x", Context: "chat"})
	if err == nil {
		t.Fatal("HTTP 500 должен давать ошибку")
	}
	if !strings.Contains(err.Error(), "внутренняя ошибка sanitizer") {
		t.Errorf("ошибка не содержит тело ответа sanitizer: %v", err)
	}
}

func TestClientPreviewTrimsTrailingSlash(t *testing.T) {
	server := fakeSanitizer(t, http.StatusOK, validResponse)
	defer server.Close()
	// завершающий слэш в baseURL не должен ломать путь
	_, err := NewClient(server.URL+"/").Preview(context.Background(),
		PreviewRequest{Text: "x", Context: "chat"})
	if err != nil {
		t.Errorf("завершающий слэш сломал запрос: %v", err)
	}
}

func TestClientPreviewInvalidJSON(t *testing.T) {
	server := fakeSanitizer(t, http.StatusOK, "не json вовсе {{{")
	defer server.Close()
	_, err := NewClient(server.URL).Preview(context.Background(),
		PreviewRequest{Text: "x", Context: "chat"})
	if err == nil {
		t.Error("некорректный JSON ответа должен давать ошибку")
	}
}

func TestClientPreviewCancelledContext(t *testing.T) {
	server := fakeSanitizer(t, http.StatusOK, validResponse)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient(server.URL).Preview(ctx,
		PreviewRequest{Text: "x", Context: "chat"})
	if err == nil {
		t.Error("отменённый контекст должен давать ошибку")
	}
}

func TestClientPreviewUnreachable(t *testing.T) {
	_, err := NewClient("http://127.0.0.1:1").Preview(context.Background(),
		PreviewRequest{Text: "x", Context: "chat"})
	if err == nil {
		t.Error("недоступный sanitizer должен давать ошибку")
	}
}
