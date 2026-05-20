package llm

import (
	"context"
	"fmt"
	"sync"
)

// Router маршрутизирует запросы к зарегистрированным провайдерам по имени.
// Безопасен для конкурентного использования.
type Router struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRouter создаёт пустой роутер.
func NewRouter() *Router {
	return &Router{providers: make(map[string]Provider)}
}

// Register регистрирует провайдера (как правило, при старте сервиса).
func (r *Router) Register(provider Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[provider.Name()] = provider
}

// Replace атомарно заменяет набор провайдеров (hot-reload после INSERT/
// UPDATE в БД). Используется handler'ами createModel и updateModelAPIKey,
// чтобы новый провайдер становился виден без restart api (F2).
func (r *Router) Replace(providers []Provider) {
	next := make(map[string]Provider, len(providers))
	for _, p := range providers {
		next[p.Name()] = p
	}
	r.mu.Lock()
	r.providers = next
	r.mu.Unlock()
}

// Has сообщает, зарегистрирован ли провайдер с указанным именем.
func (r *Router) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.providers[name]
	return ok
}

// Count возвращает число зарегистрированных провайдеров.
func (r *Router) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.providers)
}

// Complete направляет запрос провайдеру с указанным именем.
func (r *Router) Complete(
	ctx context.Context, providerName string, req ChatRequest,
) (ChatResponse, error) {
	r.mu.RLock()
	provider, ok := r.providers[providerName]
	r.mu.RUnlock()
	if !ok {
		return ChatResponse{}, fmt.Errorf(
			"llm: провайдер %q не зарегистрирован", providerName)
	}
	return provider.Complete(ctx, req)
}
