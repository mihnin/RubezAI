package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AnthropicProvider — клиент к Claude (Messages API, /v1/messages).
// Не OpenAI-совместим: иной заголовок аутентификации (x-api-key + anthropic-
// version), другая структура запроса (system отдельным полем + max_tokens
// обязательно), другая структура ответа (content: [{type, text}]).
//
// Документация: https://docs.anthropic.com/en/api/messages
type AnthropicProvider struct {
	name     string
	endpoint string // обычно https://api.anthropic.com
	apiKey   string
	client   *http.Client
}

// NewAnthropicProvider создаёт провайдер. endpoint без /v1/messages — путь
// дописывается внутри (как в OpenAI: endpoint = base без /chat/completions).
func NewAnthropicProvider(name, endpoint, apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		name:     name,
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *AnthropicProvider) Name() string { return p.name }

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	Messages  []anthropicMessage `json:"messages"`
	System    string             `json:"system,omitempty"`
	MaxTokens int                `json:"max_tokens"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model string `json:"model"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Complete выполняет POST к /v1/messages с авторизацией через x-api-key.
// MaxTokens у anthropic обязателен — берём константу defaultMaxTokens
// (ChatRequest не несёт это поле; будущее расширение — добавить max_tokens
// в Provider-интерфейс).
const defaultMaxTokens = 1024

func (p *AnthropicProvider) Complete(
	ctx context.Context, req ChatRequest,
) (ChatResponse, error) {
	system, messages := splitSystemMessages(req.Messages)
	body, err := json.Marshal(anthropicRequest{
		Model:     req.Model,
		Messages:  messages,
		System:    system,
		MaxTokens: defaultMaxTokens,
	})
	if err != nil {
		return ChatResponse{}, fmt.Errorf("anthropic: marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, p.endpoint+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("anthropic: NewRequest: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", keyOrDefault(req.APIKeyOverride, p.apiKey))
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("anthropic: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return ChatResponse{}, fmt.Errorf(
			"anthropic: HTTP %d: %s", resp.StatusCode, string(buf))
	}

	var ar anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return ChatResponse{}, fmt.Errorf("anthropic: decode: %w", err)
	}
	text := assembleAnthropicText(ar.Content)
	if text == "" {
		return ChatResponse{}, errors.New("anthropic: пустой ответ (content[].text отсутствует)")
	}
	return ChatResponse{Content: text, Model: req.Model}, nil
}

// splitSystemMessages извлекает system-сообщения в отдельное поле (Anthropic
// требует system как top-level field). Остальные роли (user/assistant)
// возвращаются как messages-массив. Несколько system-блоков склеиваются \n\n.
func splitSystemMessages(in []ChatMessage) (string, []anthropicMessage) {
	var systems []string
	out := make([]anthropicMessage, 0, len(in))
	for _, m := range in {
		if m.Role == "system" {
			systems = append(systems, m.Content)
			continue
		}
		out = append(out, anthropicMessage{Role: m.Role, Content: m.Content})
	}
	return joinNonEmpty(systems, "\n\n"), out
}

func joinNonEmpty(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i > 0 && out != "" {
			out += sep
		}
		out += p
	}
	return out
}

func assembleAnthropicText(content []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	var b bytes.Buffer
	for _, c := range content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}
