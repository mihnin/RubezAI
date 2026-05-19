package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

const validOpenAIResponse = `{"choices":[{"message":` +
	`{"role":"assistant","content":"ответ модели"}}]}`

// fakeOpenAI — поддельный OpenAI-совместимый endpoint для тестов.
func fakeOpenAI(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/chat/completions" {
				t.Errorf("путь = %q, ожидался /chat/completions", r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
				t.Errorf("Authorization = %q", got)
			}
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}))
}

func TestOpenAIProviderComplete(t *testing.T) {
	server := fakeOpenAI(t, http.StatusOK, validOpenAIResponse)
	defer server.Close()

	resp, err := NewOpenAIProvider("deepseek", server.URL, "test-key").Complete(
		context.Background(), ChatRequest{
			Model:    "deepseek-chat",
			Messages: []ChatMessage{{Role: "user", Content: "вопрос"}},
		})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "ответ модели" {
		t.Errorf("Content = %q, ожидалось «ответ модели»", resp.Content)
	}
}

func TestOpenAIProviderSendsCorrectRequest(t *testing.T) {
	var got openAIRequest
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&got)
			_, _ = io.WriteString(w, validOpenAIResponse)
		}))
	defer server.Close()

	_, _ = NewOpenAIProvider("p", server.URL, "k").Complete(
		context.Background(), ChatRequest{
			Model:    "m-1",
			Messages: []ChatMessage{{Role: "user", Content: "hi"}},
		})
	if got.Model != "m-1" {
		t.Errorf("model в запросе = %q", got.Model)
	}
	if got.Stream {
		t.Error("stream должен быть false для Complete")
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "hi" {
		t.Errorf("messages некорректны: %+v", got.Messages)
	}
}

func TestOpenAIProviderNon200(t *testing.T) {
	server := fakeOpenAI(t, http.StatusInternalServerError, "{}")
	defer server.Close()
	_, err := NewOpenAIProvider("p", server.URL, "test-key").Complete(
		context.Background(),
		ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Error("HTTP 500 от провайдера должен давать ошибку")
	}
}

func TestOpenAIProviderEmptyChoices(t *testing.T) {
	server := fakeOpenAI(t, http.StatusOK, `{"choices":[]}`)
	defer server.Close()
	_, err := NewOpenAIProvider("p", server.URL, "test-key").Complete(
		context.Background(),
		ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Error("пустой choices должен давать ошибку")
	}
}

func TestOpenAIProviderCancelledContext(t *testing.T) {
	server := fakeOpenAI(t, http.StatusOK, validOpenAIResponse)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewOpenAIProvider("p", server.URL, "test-key").Complete(
		ctx, ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Error("отменённый контекст должен приводить к ошибке")
	}
}
