package llm

import (
	"context"
	"strings"
	"testing"
)

func TestMockProviderName(t *testing.T) {
	if NewMockProvider("local-mock").Name() != "local-mock" {
		t.Error("Name провайдера не совпадает")
	}
}

func TestMockProviderComplete(t *testing.T) {
	resp, err := NewMockProvider("mock").Complete(context.Background(), ChatRequest{
		Model: "mock-1",
		Messages: []ChatMessage{
			{Role: "system", Content: "ты ассистент"},
			{Role: "user", Content: "привет"},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(resp.Content, "привет") {
		t.Errorf("ответ не отражает запрос пользователя: %q", resp.Content)
	}
	if resp.Model != "mock-1" {
		t.Errorf("Model = %q, ожидалось mock-1", resp.Model)
	}
}

func TestMockProviderDeterministic(t *testing.T) {
	req := ChatRequest{
		Model:    "m",
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	}
	provider := NewMockProvider("mock")
	first, _ := provider.Complete(context.Background(), req)
	second, _ := provider.Complete(context.Background(), req)
	if first.Content != second.Content {
		t.Error("mock-провайдер должен быть детерминированным")
	}
}

func TestMockProviderRespectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewMockProvider("mock").Complete(ctx, ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "x"}},
	})
	if err == nil {
		t.Error("отменённый контекст должен приводить к ошибке")
	}
}

func TestMockProviderNoUserMessage(t *testing.T) {
	resp, err := NewMockProvider("mock").Complete(context.Background(), ChatRequest{
		Model:    "m",
		Messages: []ChatMessage{{Role: "system", Content: "только система"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "[mock] запрос без пользовательского сообщения" {
		t.Errorf("Content = %q (ожидался маркер пустого запроса)", resp.Content)
	}
}

func TestMockProviderUsesLastUserMessage(t *testing.T) {
	resp, _ := NewMockProvider("mock").Complete(context.Background(), ChatRequest{
		Messages: []ChatMessage{
			{Role: "user", Content: "первый"},
			{Role: "assistant", Content: "ответ"},
			{Role: "user", Content: "второй"},
		},
	})
	if !strings.Contains(resp.Content, "второй") ||
		strings.Contains(resp.Content, "первый") {
		t.Errorf("mock должен брать последнее user-сообщение: %q", resp.Content)
	}
}
