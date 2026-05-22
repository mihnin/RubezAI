// Package llm — маршрутизация запросов к провайдерам LLM.
//
// Provider абстрагирует конкретную модель; Router выбирает провайдера по
// имени. Реализации MVP — MockProvider и OpenAIProvider (OpenAI-совместимый
// endpoint, в т. ч. vLLM и DeepSeek).
package llm

import "context"

// ChatMessage — одно сообщение диалога.
type ChatMessage struct {
	Role    string // system | user | assistant
	Content string
}

// ChatRequest — запрос к LLM.
type ChatRequest struct {
	Model    string
	Messages []ChatMessage
	// APIKeyOverride — персональный ключ пользователя (L); если непуст, адаптер
	// использует его вместо ключа провайдера. Endpoint/trust не меняются.
	APIKeyOverride string
}

// ChatResponse — ответ LLM.
type ChatResponse struct {
	Content string
	Model   string
}

// keyOrDefault возвращает override-ключ, если он непуст, иначе ключ провайдера.
func keyOrDefault(override, fallback string) string {
	if override != "" {
		return override
	}
	return fallback
}

// Provider — интерфейс провайдера LLM. Реализации взаимозаменяемы, что
// позволяет добавить новую модель без изменения вызывающего кода.
type Provider interface {
	// Name возвращает уникальное имя провайдера.
	Name() string
	// Complete выполняет неблокирующий (без стриминга) запрос к модели.
	Complete(ctx context.Context, req ChatRequest) (ChatResponse, error)
}
