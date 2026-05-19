package llm

import "context"

// MockProvider — детерминированный провайдер для MVP и тестов: не выполняет
// внешних сетевых вызовов.
type MockProvider struct {
	name string
}

// NewMockProvider создаёт mock-провайдер с заданным именем.
func NewMockProvider(name string) *MockProvider {
	return &MockProvider{name: name}
}

// Name возвращает имя провайдера.
func (p *MockProvider) Name() string { return p.name }

// Complete возвращает детерминированный ответ, отражающий запрос пользователя.
func (p *MockProvider) Complete(
	ctx context.Context, req ChatRequest,
) (ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return ChatResponse{}, err
	}
	lastUser := ""
	for _, message := range req.Messages {
		if message.Role == "user" {
			lastUser = message.Content
		}
	}
	return ChatResponse{
		Content: "[mock] обработан запрос: " + lastUser,
		Model:   req.Model,
	}, nil
}
