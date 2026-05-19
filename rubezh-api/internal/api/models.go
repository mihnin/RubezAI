package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// validAdapter — допустимые адаптеры (CHECK model_providers.adapter).
var validAdapter = map[string]bool{"mock": true, "openai_compatible": true}

type modelProviderDTO struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	TrustLevel      string    `json:"trust_level"`
	Adapter         string    `json:"adapter"`
	Endpoint        string    `json:"endpoint"`
	MaxTokens       *int      `json:"max_tokens"`
	RateLimitPerMin *int      `json:"rate_limit_per_min"`
	IsEnabled       bool      `json:"is_enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type createModelRequest struct {
	Name            string `json:"name"`
	TrustLevel      string `json:"trust_level"`
	Adapter         string `json:"adapter"`
	Endpoint        string `json:"endpoint"`
	MaxTokens       *int   `json:"max_tokens"`
	RateLimitPerMin *int   `json:"rate_limit_per_min"`
}

// validate проверяет запрос на создание провайдера против контракта.
func (r createModelRequest) validate() error {
	if r.Name == "" {
		return errors.New("поле name обязательно")
	}
	if !validModelTrust[r.TrustLevel] {
		return fmt.Errorf("недопустимый trust_level: %q", r.TrustLevel)
	}
	if !validAdapter[r.Adapter] {
		return fmt.Errorf("недопустимый adapter: %q", r.Adapter)
	}
	if r.Adapter == "openai_compatible" {
		if r.Endpoint == "" {
			return errors.New("для adapter openai_compatible требуется endpoint")
		}
		parsed, err := url.ParseRequestURI(r.Endpoint)
		if err != nil || parsed.Host == "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf(
				"endpoint должен быть корректным http(s)-URL: %q", r.Endpoint)
		}
	}
	if r.MaxTokens != nil && *r.MaxTokens <= 0 {
		return errors.New("max_tokens должен быть положительным")
	}
	if r.RateLimitPerMin != nil && *r.RateLimitPerMin <= 0 {
		return errors.New("rate_limit_per_min должен быть положительным")
	}
	return nil
}

func modelProviderToDTO(p storage.ModelProvider) modelProviderDTO {
	return modelProviderDTO{
		ID:              p.ID,
		Name:            p.Name,
		TrustLevel:      p.TrustLevel,
		Adapter:         p.Adapter,
		Endpoint:        p.Endpoint,
		MaxTokens:       p.MaxTokens,
		RateLimitPerMin: p.RateLimitPerMin,
		IsEnabled:       p.IsEnabled,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

// listModelsHandler возвращает список провайдеров моделей.
func listModelsHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers, err := store.ListModelProviders(r.Context())
		if err != nil {
			http.Error(w, "ошибка чтения провайдеров", http.StatusInternalServerError)
			return
		}
		out := make([]modelProviderDTO, len(providers))
		for i, p := range providers {
			out[i] = modelProviderToDTO(p)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// createModelHandler регистрирует нового провайдера модели.
func createModelHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createModelRequest
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		if err := req.validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		created, err := store.CreateModelProvider(r.Context(), storage.ModelProvider{
			Name:            req.Name,
			TrustLevel:      req.TrustLevel,
			Adapter:         req.Adapter,
			Endpoint:        req.Endpoint,
			MaxTokens:       req.MaxTokens,
			RateLimitPerMin: req.RateLimitPerMin,
		})
		if err != nil {
			if errors.Is(err, storage.ErrModelProviderExists) {
				http.Error(w, "провайдер с таким именем уже существует",
					http.StatusConflict)
				return
			}
			http.Error(w, "не удалось создать провайдера",
				http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, modelProviderToDTO(created))
	}
}
