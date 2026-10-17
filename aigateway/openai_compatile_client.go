package aigateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

const (
	maxEmbeddingBatchSize = 2048
	dummyPayload          = "ping"
)

type OpenAICompatileClient struct {
	Chat      ChatService
	Embedding EmbeddingService

	sdk openai.Client
}

func NewOpenAICompatileClient() *OpenAICompatileClient {
	client := &OpenAICompatileClient{
		sdk: openai.NewClient(),
	}

	client.Chat = &openaiCompatileChat{client: client}
	client.Embedding = &openaiCompatileEmbedding{client: client}

	return client
}

type openaiCompatileChat struct {
	client *OpenAICompatileClient
}

func (c *openaiCompatileChat) Create(ctx context.Context, req CreateChatRequest, opts ...CreateChatOption) (*ChatResponse, error) {
	params, err := buildChatParams(req, opts)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.sdk.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}

	choices := make([]Choice, 0, len(resp.Choices))
	for _, choice := range resp.Choices {
		choices = append(choices, Choice{
			Message: ChoiceMessage{
				Content: buildChatContent(choice.Message.Content, choice.Message.JSON.Content.Valid()),
				Role:    string(choice.Message.Role),
			},
		})
	}

	return &ChatResponse{
		Choices: choices,
		Model:   resp.Model,
		Usage:   buildChatUsage(resp.Usage, resp.JSON.Usage.Valid()),
		Raw:     json.RawMessage(resp.RawJSON()),
	}, nil
}

func (c *openaiCompatileChat) CreateStream(ctx context.Context, req CreateChatRequest, opts ...CreateChatOption) (<-chan *ChatResponse, error) {
	params, err := buildChatParams(req, opts)
	if err != nil {
		return nil, err
	}
	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
		IncludeUsage: openai.Bool(true),
	}

	stream := c.client.sdk.Chat.Completions.NewStreaming(ctx, params)
	respCh := make(chan *ChatResponse)

	go func() {
		defer func() {
			stream.Close()
			close(respCh)
		}()

		for stream.Next() {
			chunk := stream.Current()
			choices := make([]Choice, 0, len(chunk.Choices))
			for _, choice := range chunk.Choices {
				choices = append(choices, Choice{
					Message: ChoiceMessage{
						Content: buildChatContent(choice.Delta.Content, choice.Delta.JSON.Content.Valid()),
						Role:    choice.Delta.Role,
					},
				})
			}
			resp := &ChatResponse{
				Choices: choices,
				Model:   chunk.Model,
				Usage:   buildChatUsage(chunk.Usage, chunk.JSON.Usage.Valid()),
				Raw:     json.RawMessage(chunk.RawJSON()),
			}
			select {
			case <-ctx.Done():
				return

			case respCh <- resp:
			}
		}

	}()

	return respCh, nil
}

func buildChatParams(req CreateChatRequest, opts []CreateChatOption) (openai.ChatCompletionNewParams, error) {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages))
	for _, message := range req.Messages {
		param, err := buildChatMessage(message)
		if err != nil {
			return openai.ChatCompletionNewParams{}, err
		}
		messages = append(messages, param)
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(req.Model),
		Messages: messages,
	}
	for _, opt := range opts {
		switch opt.Type {
		case CreateChatOptionTypeTemperature:
			value, ok := opt.Value.(float64)
			if !ok {
				return openai.ChatCompletionNewParams{}, fmt.Errorf("chat option temperature requires a float64")
			}
			params.Temperature = openai.Float(value)

		case CreateChatOptionTypeMaxTokens:
			value, ok := opt.Value.(int)
			if !ok {
				return openai.ChatCompletionNewParams{}, fmt.Errorf("chat option max_tokens requires an int")
			}
			params.MaxTokens = openai.Int(int64(value))

		case CreateChatOptionTypeSchema:
			schema, ok := opt.Value.(*ResponseSchema)
			if !ok || schema == nil {
				return openai.ChatCompletionNewParams{}, fmt.Errorf("chat option schema requires a response schema")
			}
			if schema.Strict {
				params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
					OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
						JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
							Strict: openai.Bool(true),
						},
					},
				}
				break
			}
			format := shared.NewResponseFormatJSONObjectParam()
			params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
				OfJSONObject: &format,
			}

		case CreateChatOptionTypeTools:
			tools, ok := opt.Value.([]Tool)
			if !ok {
				return openai.ChatCompletionNewParams{}, fmt.Errorf("chat option tools requires a tool list")
			}
			converted := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
			for _, tool := range tools {
				function := shared.FunctionDefinitionParam{Name: tool.Name}
				if tool.Description != "" {
					function.Description = openai.String(tool.Description)
				}
				if len(tool.Parameters) > 0 {
					var parameters shared.FunctionParameters
					if err := json.Unmarshal(tool.Parameters, &parameters); err != nil {
						return openai.ChatCompletionNewParams{}, fmt.Errorf("chat tool parameters are invalid: %w", err)
					}
					function.Parameters = parameters
				}
				converted = append(converted, openai.ChatCompletionFunctionTool(function))
			}
			params.Tools = converted

		default:
			return openai.ChatCompletionNewParams{}, fmt.Errorf("chat option %s is not supported", opt.Type)
		}
	}
	return params, nil
}

func buildChatMessage(message Message) (openai.ChatCompletionMessageParamUnion, error) {
	switch message.Role {
	case "system":
		return openai.SystemMessage(message.Content), nil
	case "user":
		return openai.UserMessage(message.Content), nil
	case "assistant":
		return openai.AssistantMessage(message.Content), nil
	case "developer":
		return openai.DeveloperMessage(message.Content), nil
	default:
		return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("chat message role %s is not supported", message.Role)
	}
}

func buildChatContent(content string, present bool) *string {
	if !present {
		return nil
	}
	return &content
}

func buildChatUsage(usage openai.CompletionUsage, present bool) *Usage {
	if !present {
		return nil
	}
	return &Usage{
		InputTokens:  int(usage.PromptTokens),
		OutputTokens: int(usage.CompletionTokens),
	}
}

func (c *openaiCompatileChat) Ping(ctx context.Context) error {
	_, err := c.client.sdk.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(dummyPayload),
		},
	})
	return err
}

type openaiCompatileEmbedding struct {
	client *OpenAICompatileClient
}

func (c *openaiCompatileEmbedding) Create(ctx context.Context, req CreateEmbeddingRequest) (*EmbeddingResponse, error) {
	texts := req.Input
	if len(texts) == 0 {
		return nil, nil
	}

	var results []Embedding
	for i := 0; i < len(texts); i += maxEmbeddingBatchSize {
		end := i + maxEmbeddingBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		params := openai.EmbeddingNewParams{
			Input: openai.EmbeddingNewParamsInputUnion{
				OfArrayOfStrings: batch,
			},
			Model:          req.Model,
			EncodingFormat: openai.EmbeddingNewParamsEncodingFormatFloat,
		}

		resp, err := c.client.sdk.Embeddings.New(ctx, params)
		if err != nil {
			return nil, err
		}

		for _, d := range resp.Data {
			vec := make([]float32, len(d.Embedding))
			for j, val := range d.Embedding {
				vec[j] = float32(val)
			}
			results = append(results, Embedding{
				Index:  i + int(d.Index),
				Vector: vec,
			})
		}
	}

	return &EmbeddingResponse{
		Embeddings: results,
		Model:      req.Model,
	}, nil
}

func (c *openaiCompatileEmbedding) Ping(ctx context.Context) error {
	_, err := c.client.sdk.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfString: openai.String(dummyPayload),
		},
	})
	return err
}
