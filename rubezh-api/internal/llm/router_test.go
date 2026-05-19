package llm

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
)

// errProvider — провайдер-заглушка, всегда возвращающий ошибку.
type errProvider struct{ name string }

func (p errProvider) Name() string { return p.name }

func (p errProvider) Complete(context.Context, ChatRequest) (ChatResponse, error) {
	return ChatResponse{}, errors.New("сбой провайдера")
}

// tagProvider — провайдер, возвращающий заданную метку (проверка перезаписи).
type tagProvider struct {
	name string
	tag  string
}

func (p tagProvider) Name() string { return p.name }

func (p tagProvider) Complete(context.Context, ChatRequest) (ChatResponse, error) {
	return ChatResponse{Content: p.tag}, nil
}

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

func TestRouterRegisterOverwrites(t *testing.T) {
	router := NewRouter()
	router.Register(tagProvider{name: "dup", tag: "первый"})
	router.Register(tagProvider{name: "dup", tag: "второй"})
	if router.Count() != 1 {
		t.Errorf("Count = %d, ожидалось 1 после перезаписи", router.Count())
	}
	resp, _ := router.Complete(context.Background(), "dup", ChatRequest{})
	if resp.Content != "второй" {
		t.Errorf("ожидался ответ перезаписавшего провайдера, получено %q", resp.Content)
	}
}

func TestRouterPropagatesProviderError(t *testing.T) {
	router := NewRouter()
	router.Register(errProvider{name: "broken"})
	_, err := router.Complete(context.Background(), "broken", ChatRequest{})
	if err == nil || err.Error() != "сбой провайдера" {
		t.Errorf("ошибка провайдера должна пробрасываться, получено %v", err)
	}
}

func TestRouterConcurrentRegisterAndRead(t *testing.T) {
	router := NewRouter()
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			router.Register(NewMockProvider("mock-" + strconv.Itoa(i)))
		}()
	}
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = router.Count()
			_ = router.Has("mock-0")
		}()
	}
	wg.Wait()
	if router.Count() != 10 {
		t.Errorf("Count = %d, ожидалось 10", router.Count())
	}
}
