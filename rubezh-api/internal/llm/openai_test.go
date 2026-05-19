package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	server := fakeOpenAI(t, http.StatusInternalServerError,
		`{"error":"внутренняя ошибка провайдера"}`)
	defer server.Close()
	_, err := NewOpenAIProvider("p", server.URL, "test-key").Complete(
		context.Background(),
		ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Fatal("HTTP 500 от провайдера должен давать ошибку")
	}
	// тело ответа провайдера должно попадать в текст ошибки (диагностика)
	if !strings.Contains(err.Error(), "внутренняя ошибка провайдера") {
		t.Errorf("ошибка не содержит тело ответа провайдера: %v", err)
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

func TestOpenAIProviderInvalidJSON(t *testing.T) {
	server := fakeOpenAI(t, http.StatusOK, "не json вовсе {{{")
	defer server.Close()
	_, err := NewOpenAIProvider("p", server.URL, "test-key").Complete(
		context.Background(),
		ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Error("некорректный JSON ответа должен давать ошибку")
	}
}

func TestOpenAIProviderUnreachableEndpoint(t *testing.T) {
	_, err := NewOpenAIProvider("p", "http://127.0.0.1:1", "k").Complete(
		context.Background(),
		ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Error("недоступный endpoint должен давать ошибку")
	}
}

func TestOpenAIProviderMalformedURL(t *testing.T) {
	_, err := NewOpenAIProvider("p", "http://ho\x7fst", "k").Complete(
		context.Background(),
		ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Error("некорректный URL должен давать ошибку")
	}
}

func TestOpenAIProviderRequestMethodAndHeaders(t *testing.T) {
	var method, path, contentType, accept, auth string
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			method, path = r.Method, r.URL.Path
			contentType = r.Header.Get("Content-Type")
			accept = r.Header.Get("Accept")
			auth = r.Header.Get("Authorization")
			_, _ = io.WriteString(w, validOpenAIResponse)
		}))
	defer server.Close()
	_, _ = NewOpenAIProvider("p", server.URL, "my-key").Complete(
		context.Background(),
		ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	if method != http.MethodPost || path != "/chat/completions" {
		t.Errorf("метод/путь = %s %s", method, path)
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q", contentType)
	}
	if accept != "application/json" {
		t.Errorf("Accept = %q, ожидалось application/json", accept)
	}
	if auth != "Bearer my-key" {
		t.Errorf("Authorization = %q", auth)
	}
}

func TestOpenAIProviderTrimsTrailingSlash(t *testing.T) {
	server := fakeOpenAI(t, http.StatusOK, validOpenAIResponse)
	defer server.Close()
	// завершающий слэш в endpoint не должен ломать путь /chat/completions
	resp, err := NewOpenAIProvider("p", server.URL+"/", "test-key").Complete(
		context.Background(),
		ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("завершающий слэш в endpoint сломал запрос: %v", err)
	}
	if resp.Content != "ответ модели" {
		t.Errorf("Content = %q", resp.Content)
	}
}

func TestOpenAIProviderRespectsContextDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(300 * time.Millisecond)
			_, _ = io.WriteString(w, validOpenAIResponse)
		}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := NewOpenAIProvider("p", server.URL, "k").Complete(
		ctx, ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "x"}}})
	if err == nil {
		t.Error("истёкший дедлайн контекста должен давать ошибку")
	}
	if time.Since(start) > time.Second {
		t.Error("запрос не прервался по дедлайну вовремя")
	}
}
