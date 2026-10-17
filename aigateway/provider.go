package aigateway

import "context"

type BaseProvider struct {
	Name    string
	BaseURL string
	APIKey  string
}

type ModelType string

const (
	ModelTypeLLM       ModelType = "llm"
	ModelTypeEmbedding ModelType = "embedding"
	ModelTypeTTS       ModelType = "tts"
)

type APIProtocol string

const (
	APIProtocolChatCompletions = "chat_completions"
	APIProtocolResponsesAPI    = "responses_api"
)

type Model struct {
	Type     ModelType
	Name     string
	Protocol APIProtocol
}

type Provider interface {
	ListModels(ctx context.Context) ([]*Model, error)
}
