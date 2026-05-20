package llm

import (
	"context"
	"sync"
	"testing"
)

type fakeProvider struct{ name string }

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Complete(_ context.Context, _ ChatRequest) (ChatResponse, error) {
	return ChatResponse{Content: f.name}, nil
}

func TestRouterReplaceAtomicallySwapsSet(t *testing.T) {
	r := NewRouter()
	r.Register(&fakeProvider{name: "old1"})
	r.Register(&fakeProvider{name: "old2"})

	r.Replace([]Provider{
		&fakeProvider{name: "new1"},
		&fakeProvider{name: "new2"},
		&fakeProvider{name: "new3"},
	})

	if r.Count() != 3 {
		t.Errorf("count = %d, ожидалось 3", r.Count())
	}
	if r.Has("old1") || r.Has("old2") {
		t.Error("старые провайдеры должны быть вытеснены")
	}
	if !r.Has("new1") || !r.Has("new2") || !r.Has("new3") {
		t.Error("новые провайдеры должны быть зарегистрированы")
	}
}

func TestRouterReplaceEmpty(t *testing.T) {
	r := NewRouter()
	r.Register(&fakeProvider{name: "x"})
	r.Replace(nil)
	if r.Count() != 0 {
		t.Errorf("Replace(nil) должен очистить роутер, got count = %d", r.Count())
	}
}

func TestRouterReplaceConcurrentSafety(t *testing.T) {
	r := NewRouter()
	r.Replace([]Provider{&fakeProvider{name: "init"}})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.Replace([]Provider{&fakeProvider{name: "p"}})
		}()
		go func() {
			defer wg.Done()
			_, _ = r.Complete(context.Background(), "p", ChatRequest{})
			_ = r.Has("p")
			_ = r.Count()
		}()
	}
	wg.Wait()
	// Проверка: race detector не должен сработать; финальное состояние стабильно.
	if r.Count() != 1 {
		t.Errorf("count = %d, ожидалось 1", r.Count())
	}
}
