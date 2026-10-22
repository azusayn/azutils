package aigateway

import (
	"context"
	"encoding/json"
)

type ChatService interface {
	Create(ctx context.Context, req CreateChatRequest, opts ...CreateChatOption) (*ChatResponse, error)
	CreateStream(ctx context.Context, req CreateChatRequest, opts ...CreateStreamOption) (<-chan *ChatResponse, error)
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
	// LocalSchemaCheck 为 true 时不向模型发送 strict 参数，仅在本地校验响应是否符合 schema；为 false 时向模型发送 strict: true，同时在本地进行校验。
	LocalSchemaCheck bool
	Schema           json.RawMessage
}

type CreateChatRequest struct {
	Model    string
	Messages []Message
}

type CreateChatOptionType string

const (
	CreateChatOptionTypeResponseFormat CreateChatOptionType = "response_format"
	CreateChatOptionTypeMaxAttempts    CreateChatOptionType = "max_attempts"
	CreateChatOptionTypeTools          CreateChatOptionType = "tools"
	CreateChatOptionTypeTemperature    CreateChatOptionType = "temperature"
	CreateChatOptionTypeMaxTokens      CreateChatOptionType = "max_tokens"
)

// 为了保持接口干净，把不是所有模型都支持的选项 （工具调用、温度等）放在这个结构体中，下层实现
// 可以检查输入的选项进行配置或者对不支持的参数返回错误（或者忽略并进行警告）。
type CreateChatOption struct {
	Type  CreateChatOptionType
	Value any
}

// WithResponseFormat 设置响应的 JSON Schema 格式。
// localSchemaCheck 为 true 时不向模型发送 strict 参数，仅在本地校验；为 false 时向模型发送 strict: true，同时在本地进行校验。
func WithResponseFormat(schema json.RawMessage, localSchemaCheck bool) CreateChatOption {
	return CreateChatOption{
		Type: CreateChatOptionTypeResponseFormat,
		Value: &ResponseSchema{
			Schema:           schema,
			LocalSchemaCheck: localSchemaCheck,
		},
	}
}

// WithMaxAttempts 设置最大请求次数，默认为 1.
func WithMaxAttempts(maxAttempts int) CreateChatOption {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return CreateChatOption{
		Type:  CreateChatOptionTypeMaxAttempts,
		Value: maxAttempts,
	}
}

func WithTools(tools []Tool) CreateChatOption {
	return CreateChatOption{
		Type:  CreateChatOptionTypeTools,
		Value: tools,
	}
}

func WithTemperature(temperature float64) CreateChatOption {
	return CreateChatOption{
		Type:  CreateChatOptionTypeTemperature,
		Value: temperature,
	}
}

func WithMaxTokens(maxTokens int) CreateChatOption {
	return CreateChatOption{
		Type:  CreateChatOptionTypeMaxTokens,
		Value: maxTokens,
	}
}

type CreateStreamOptionType string

const (
	CreateStreamOptionTypeResponseFormat CreateStreamOptionType = "response_format"
	CreateStreamOptionTypeTools          CreateStreamOptionType = "tools"
	CreateStreamOptionTypeTemperature    CreateStreamOptionType = "temperature"
	CreateStreamOptionTypeMaxTokens      CreateStreamOptionType = "max_tokens"
)

type CreateStreamOption struct {
	Type  CreateStreamOptionType
	Value any
}

type StreamResponseFormat struct {
	Schema json.RawMessage
	Strict bool
}

func WithStreamResponseFormat(schema json.RawMessage, strict bool) CreateStreamOption {
	return CreateStreamOption{
		Type: CreateStreamOptionTypeResponseFormat,
		Value: &StreamResponseFormat{
			Schema: schema,
			Strict: strict,
		},
	}
}

func WithStreamTools(tools []Tool) CreateStreamOption {
	return CreateStreamOption{
		Type:  CreateStreamOptionTypeTools,
		Value: tools,
	}
}

func WithStreamTemperature(temperature float64) CreateStreamOption {
	return CreateStreamOption{
		Type:  CreateStreamOptionTypeTemperature,
		Value: temperature,
	}
}

func WithStreamMaxTokens(maxTokens int) CreateStreamOption {
	return CreateStreamOption{
		Type:  CreateStreamOptionTypeMaxTokens,
		Value: maxTokens,
	}
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
