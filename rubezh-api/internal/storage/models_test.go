package storage

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestCreateAndListModelProviders(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	name := "model-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	maxTokens := 4096
	created, err := store.CreateModelProvider(ctx, ModelProvider{
		Name:       name,
		TrustLevel: "trusted_local",
		Adapter:    "mock",
		MaxTokens:  &maxTokens,
	})
	if err != nil {
		t.Fatalf("CreateModelProvider: %v", err)
	}
	if created.ID == "" || created.Name != name {
		t.Errorf("создан некорректно: %+v", created)
	}
	if created.MaxTokens == nil || *created.MaxTokens != 4096 {
		t.Error("max_tokens не сохранён")
	}
	if !created.IsEnabled {
		t.Error("is_enabled по умолчанию должен быть true")
	}

	providers, err := store.ListModelProviders(ctx)
	if err != nil {
		t.Fatalf("ListModelProviders: %v", err)
	}
	found := false
	for _, p := range providers {
		if p.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Error("созданный провайдер отсутствует в списке")
	}
}

func TestCreateModelProviderRejectsDuplicateName(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	name := "dup-model-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	provider := ModelProvider{Name: name, TrustLevel: "external", Adapter: "mock"}
	if _, err := store.CreateModelProvider(ctx, provider); err != nil {
		t.Fatalf("первое создание: %v", err)
	}
	_, err := store.CreateModelProvider(ctx, provider)
	if !errors.Is(err, ErrModelProviderExists) {
		t.Errorf("дубликат имени: ожидалась ErrModelProviderExists, получено %v", err)
	}
}

func TestCreateModelProviderNullableFields(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	name := "nullable-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	created, err := store.CreateModelProvider(ctx, ModelProvider{
		Name: name, TrustLevel: "external", Adapter: "mock",
	})
	if err != nil {
		t.Fatalf("CreateModelProvider: %v", err)
	}
	if created.Endpoint != "" {
		t.Errorf("Endpoint = %q, ожидалось пусто (NULL)", created.Endpoint)
	}
	if created.MaxTokens != nil || created.RateLimitPerMin != nil {
		t.Error("max_tokens / rate_limit_per_min должны быть nil (NULL)")
	}
}
