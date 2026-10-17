package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	or "github.com/revrost/go-openrouter"
)

type OpenRouterClient struct {
	Chat      ChatService
	Embedding EmbeddingService

	sdk *or.Client
}

func NewOpenRouterClient(provider Provider) *OpenRouterClient {
	client := &OpenRouterClient{
		sdk: or.NewClient(provider.GetAPIKey()),
	}

	client.Chat = &openRouterChat{client: client}
	client.Embedding = &openRouterEmbedding{}

	return client
}

type openRouterChat struct {
	client *OpenRouterClient
}

func (c *openRouterChat) Create(ctx context.Context, req CreateChatRequest, opts ...CreateChatOption) (*ChatResponse, error) {
	params, err := buildOpenRouterChatRequest(req, opts)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.sdk.CreateChatCompletion(ctx, params)
	if err != nil {
		return nil, err
	}
	return buildOpenRouterChatResponse(resp)
}

func (c *openRouterChat) CreateStream(ctx context.Context, req CreateChatRequest, opts ...CreateChatOption) (<-chan *ChatResponse, error) {
	params, err := buildOpenRouterChatRequest(req, opts)
	if err != nil {
		return nil, err
	}
	params.StreamOptions = &or.StreamOptions{IncludeUsage: true}

	stream, err := c.client.sdk.CreateChatCompletionStream(ctx, params)
	if err != nil {
		return nil, err
	}

	respCh := make(chan *ChatResponse)
	go func() {
		defer func() {
			stream.Close()
			close(respCh)
		}()

		for {
			chunk, err := stream.Recv()
			if err != nil {
				return
			}
			resp, err := buildOpenRouterStreamResponse(chunk)
			if err != nil {
				return
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

func (c *openRouterChat) Ping(ctx context.Context) error {
	_, err := c.client.sdk.CreateChatCompletion(ctx, or.ChatCompletionRequest{
		Messages: []or.ChatCompletionMessage{
			or.UserMessage(dummyPayload),
		},
	})
	return err
}

func buildOpenRouterChatRequest(req CreateChatRequest, opts []CreateChatOption) (or.ChatCompletionRequest, error) {
	messages := make([]or.ChatCompletionMessage, 0, len(req.Messages))
	for _, message := range req.Messages {
		param, err := buildOpenRouterMessage(message)
		if err != nil {
			return or.ChatCompletionRequest{}, err
		}
		messages = append(messages, param)
	}

	params := or.ChatCompletionRequest{
		Model:    req.Model,
		Messages: messages,
	}
	for _, opt := range opts {
		switch opt.Type {
		case CreateChatOptionTypeTemperature:
			value, ok := opt.Value.(float64)
			if !ok {
				return or.ChatCompletionRequest{}, fmt.Errorf("chat option temperature requires a float64")
			}
			params.Temperature = float32(value)

		case CreateChatOptionTypeMaxTokens:
			value, ok := opt.Value.(int)
			if !ok {
				return or.ChatCompletionRequest{}, fmt.Errorf("chat option max_tokens requires an int")
			}
			params.MaxTokens = value

		case CreateChatOptionTypeSchema:
			schema, ok := opt.Value.(*ResponseSchema)
			if !ok || schema == nil {
				return or.ChatCompletionRequest{}, fmt.Errorf("chat option schema requires a response schema")
			}
			if schema.Strict {
				params.ResponseFormat = &or.ChatCompletionResponseFormat{
					Type: or.ChatCompletionResponseFormatTypeJSONSchema,
					JSONSchema: &or.ChatCompletionResponseFormatJSONSchema{
						Strict: true,
					},
				}
				break
			}
			params.ResponseFormat = &or.ChatCompletionResponseFormat{
				Type: or.ChatCompletionResponseFormatTypeJSONObject,
			}

		case CreateChatOptionTypeTools:
			tools, ok := opt.Value.([]Tool)
			if !ok {
				return or.ChatCompletionRequest{}, fmt.Errorf("chat option tools requires a tool list")
			}
			converted := make([]or.Tool, 0, len(tools))
			for _, tool := range tools {
				function := &or.FunctionDefinition{Name: tool.Name}
				if tool.Description != "" {
					function.Description = tool.Description
				}
				if len(tool.Parameters) > 0 {
					function.Parameters = tool.Parameters
				}
				converted = append(converted, or.Tool{
					Type:     or.ToolTypeFunction,
					Function: function,
				})
			}
			params.Tools = converted

		default:
			return or.ChatCompletionRequest{}, fmt.Errorf("chat option %s is not supported", opt.Type)
		}
	}
	return params, nil
}

func buildOpenRouterMessage(message Message) (or.ChatCompletionMessage, error) {
	switch message.Role {
	case "system":
		return or.SystemMessage(message.Content), nil
	case "user":
		return or.UserMessage(message.Content), nil
	case "assistant":
		return or.AssistantMessage(message.Content), nil
	default:
		return or.ChatCompletionMessage{}, fmt.Errorf("chat message role %s is not supported", message.Role)
	}
}

func buildOpenRouterChatResponse(resp or.ChatCompletionResponse) (*ChatResponse, error) {
	raw, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}

	choices := make([]Choice, 0, len(resp.Choices))
	for _, choice := range resp.Choices {
		text := choice.Message.Content.Text
		choices = append(choices, Choice{
			Message: ChoiceMessage{
				Content: &text,
				Role:    choice.Message.Role,
			},
		})
	}

	return &ChatResponse{
		Choices: choices,
		Model:   resp.Model,
		Usage:   buildOpenRouterUsage(resp.Usage),
		Raw:     raw,
	}, nil
}

func buildOpenRouterStreamResponse(chunk or.ChatCompletionStreamResponse) (*ChatResponse, error) {
	raw, err := json.Marshal(chunk)
	if err != nil {
		return nil, err
	}

	choices := make([]Choice, 0, len(chunk.Choices))
	for _, choice := range chunk.Choices {
		text := choice.Delta.Content
		choices = append(choices, Choice{
			Message: ChoiceMessage{
				Content: &text,
				Role:    choice.Delta.Role,
			},
		})
	}

	return &ChatResponse{
		Choices: choices,
		Model:   chunk.Model,
		Usage:   buildOpenRouterUsage(chunk.Usage),
		Raw:     raw,
	}, nil
}

func buildOpenRouterUsage(usage *or.Usage) *Usage {
	if usage == nil {
		return nil
	}
	return &Usage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
	}
}

type openRouterEmbedding struct{}

func (e *openRouterEmbedding) Create(context.Context, CreateEmbeddingRequest) (*EmbeddingResponse, error) {
	return nil, errors.New("openrouter embedding is not implemented")
}

func (e *openRouterEmbedding) Ping(context.Context) error {
	return errors.New("openrouter embedding is not implemented")
}
