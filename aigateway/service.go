package aigateway

import (
	"context"
	"encoding/json"
)

type ChatService interface {
	Create(ctx context.Context, req CreateChatRequest, opts ...CreateChatOption) (*ChatResponse, error)
	CreateStream(ctx context.Context, req CreateChatRequest, opts ...CreateChatOption) (<-chan *ChatResponse, error)
	Ping(ctx context.Context) error
}

type MessagePart struct {
	Text     *string
	ImageURL *string
}

type Message struct {
	Role  string
	Parts []*MessagePart
}

type ResponseSchema struct {
	// Strict 为 true 时要求输出字段与 schema 完全匹配，否则仅保证输出为合法 JSON。
	Strict bool
	Schema json.RawMessage
}

type CreateChatRequest struct {
	Model    string
	Messages []Message
}

type CreateChatOptionType string

const (
	CreateChatOptionTypeSchema      CreateChatOptionType = "schema"
	CreateChatOptionTypeTools       CreateChatOptionType = "tools"
	CreateChatOptionTypeTemperature CreateChatOptionType = "temperature"
	CreateChatOptionTypeMaxTokens   CreateChatOptionType = "max_tokens"
)

// 为了保持接口干净，把不是所有模型都支持的选项 （工具调用、温度等）放在这个结构体中，下层实现
// 可以检查输入的选项进行配置或者对不支持的参数返回错误（或者忽略并进行警告）。
type CreateChatOption struct {
	Type  CreateChatOptionType
	Value any
}

type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// Breakdown of tokens used in the prompt.
type CompletionUsagePromptTokensDetails struct {
	CacheWriteTokens int64
	CachedTokens     int64
	AudioTokens      int64
	ImageTokens      int64
	TextTokens       int64
}

type CompletionUsageCompletionTokensDetails struct {
	AcceptedPredictionTokens int64
	RejectedPredictionTokens int64
	AudioTokens              int64
	TextTokens               int64
	ReasoningTokens          int64
}

// Usage statistics for the completion request.
type CompletionUsage struct {
	PromptTokens            int64
	PromptTokensDetails     CompletionUsagePromptTokensDetails
	CompletionTokens        int64
	CompletionTokensDetails CompletionUsageCompletionTokensDetails
	// TotalTokens = PromptTokens + CompletionTokens.
	TotalTokens int64
}

type ChatResponse struct {
	Choices []Choice
	Model   string
	// `ChatResponse.Usage` 定义为 `*Usage`，以便在模型服务商返回 Token 用量时由 API 直接暴露，使调用方无需深入 `Raw` 自行查找相关字段。
	Usage *CompletionUsage
	// `Raw` 类型为 `json.RawMessage`，保存当前 API 未做解析的原始响应体，
	// 以适应复杂多变的厂商参数。
	Raw json.RawMessage
}

type Choice struct {
	Message ChoiceMessage
}

type ChoiceMessage struct {
	Content *string
	Role    string
}

type EmbeddingService interface {
	Create(ctx context.Context, req CreateEmbeddingRequest) (*EmbeddingResponse, error)
	Ping(ctx context.Context) error
}

type CreateEmbeddingRequest struct {
	Model string
	Input []string
}

type Embedding struct {
	// 批量输入返回结果不保证顺序，利用这个字段保证顺序
	Index  int
	Vector []float32
}

type EmbeddingResponse struct {
	Embeddings []Embedding
	Usage      *CompletionUsage
	Model      string
}
