package llm

import (
	"context"
	"sync"
	"testing"
)

func TestRouterRoutesToRegisteredProvider(t *testing.T) {
	router := NewRouter()
	router.Register(NewMockProvider("mock-a"))

	resp, err := router.Complete(context.Background(), "mock-a", ChatRequest{
		Messages: []ChatMessage{{Role: "user", Content: "тест"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content == "" {
		t.Error("пустой ответ от провайдера")
	}
}

func TestRouterUnknownProviderFails(t *testing.T) {
	_, err := NewRouter().Complete(context.Background(), "нет-такого", ChatRequest{})
	if err == nil {
		t.Error("неизвестный провайдер должен давать ошибку")
	}
}

func TestRouterHas(t *testing.T) {
	router := NewRouter()
	router.Register(NewMockProvider("p1"))
	if !router.Has("p1") {
		t.Error("Has(p1) должно быть true")
	}
	if router.Has("p2") {
		t.Error("Has(p2) должно быть false")
	}
}

func TestRouterConcurrentComplete(t *testing.T) {
	router := NewRouter()
	router.Register(NewMockProvider("mock"))

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := router.Complete(context.Background(), "mock", ChatRequest{
				Messages: []ChatMessage{{Role: "user", Content: "x"}},
			}); err != nil {
				t.Errorf("конкурентный Complete: %v", err)
			}
		}()
	}
	wg.Wait()
}
