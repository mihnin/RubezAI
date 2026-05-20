package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rubezh-ai/rubezh-api/internal/crypto"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// validAdapter — допустимые адаптеры (CHECK model_providers.adapter).
var validAdapter = map[string]bool{"mock": true, "openai_compatible": true}

// modelProviderDTO — публичная форма ModelProvider.
// has_api_key: bool (Итерация 9.5) — есть ли зашифрованный ключ;
// сам ключ никогда не возвращается в API (даже masked).
type modelProviderDTO struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	TrustLevel      string    `json:"trust_level"`
	Adapter         string    `json:"adapter"`
	Endpoint        string    `json:"endpoint"`
	MaxTokens       *int      `json:"max_tokens"`
	RateLimitPerMin *int      `json:"rate_limit_per_min"`
	IsEnabled       bool      `json:"is_enabled"`
	HasAPIKey       bool      `json:"has_api_key"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// createModelRequest — POST /api/models. APIKey (plaintext) опционально:
// если задан — шифруется AES-GCM с AAD=name перед записью в БД.
type createModelRequest struct {
	Name            string  `json:"name"`
	TrustLevel      string  `json:"trust_level"`
	Adapter         string  `json:"adapter"`
	Endpoint        string  `json:"endpoint"`
	MaxTokens       *int    `json:"max_tokens"`
	RateLimitPerMin *int    `json:"rate_limit_per_min"`
	APIKey          *string `json:"api_key"`
}

// patchAPIKeyRequest — POST /api/models/:id/api-key. Plaintext шифруется
// с AAD = name того же провайдера (read-modify-write). Пустая строка
// сбрасывает ключ (NULL) — provider будет использовать env LLM_API_KEY
// fallback (deprecated, для backward compat).
type patchAPIKeyRequest struct {
	APIKey string `json:"api_key"`
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
		HasAPIKey:       p.HasAPIKey(),
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

// modelProviderAPIKeyAAD — AAD для шифрования API-ключа провайдера.
// Привязан к name: переименование провайдера сделает ключ нечитаемым
// (требует переустановки ключа — намеренно).
func modelProviderAPIKeyAAD(name string) []byte {
	return []byte("model_provider_api_key:" + name)
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
// cipher — для шифрования опционального api_key из тела запроса (Итерация 9.5).
func createModelHandler(
	store *storage.Storage, cipher *crypto.Cipher,
) http.HandlerFunc {
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
		var encrypted []byte
		if req.APIKey != nil && *req.APIKey != "" {
			if cipher == nil {
				http.Error(w, "MAPPING_ENCRYPTION_KEY не настроен — api_key недоступен",
					http.StatusServiceUnavailable)
				return
			}
			ct, err := cipher.Encrypt([]byte(*req.APIKey),
				modelProviderAPIKeyAAD(req.Name))
			if err != nil {
				http.Error(w, "ошибка шифрования api_key",
					http.StatusInternalServerError)
				return
			}
			encrypted = ct
		}
		created, err := store.CreateModelProvider(r.Context(), storage.ModelProvider{
			Name:            req.Name,
			TrustLevel:      req.TrustLevel,
			Adapter:         req.Adapter,
			Endpoint:        req.Endpoint,
			MaxTokens:       req.MaxTokens,
			RateLimitPerMin: req.RateLimitPerMin,
			APIKeyEncrypted: encrypted,
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

// updateModelAPIKeyHandler — POST /api/models/:id/api-key.
// Заменяет зашифрованный ключ или удаляет его (api_key="").
func updateModelAPIKeyHandler(
	store *storage.Storage, cipher *crypto.Cipher,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		var req patchAPIKeyRequest
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		// Получаем провайдера — нужно name для AAD.
		provider, err := store.GetModelProvider(r.Context(), id)
		if errors.Is(err, storage.ErrModelProviderNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "ошибка чтения", http.StatusInternalServerError)
			return
		}
		var encrypted []byte
		if req.APIKey != "" {
			if cipher == nil {
				http.Error(w, "MAPPING_ENCRYPTION_KEY не настроен",
					http.StatusServiceUnavailable)
				return
			}
			ct, encErr := cipher.Encrypt([]byte(req.APIKey),
				modelProviderAPIKeyAAD(provider.Name))
			if encErr != nil {
				http.Error(w, "ошибка шифрования",
					http.StatusInternalServerError)
				return
			}
			encrypted = ct
		}
		if err := store.UpdateModelProviderAPIKey(r.Context(),
			id, encrypted); err != nil {
			if errors.Is(err, storage.ErrModelProviderNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "ошибка обновления",
				http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
