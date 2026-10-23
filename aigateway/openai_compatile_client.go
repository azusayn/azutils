package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

const (
	maxEmbeddingBatchSize = 2048
)

func attempt(maxAttempts int, fn func() error) error {
	var merr error
	for range maxAttempts {
		if err := fn(); err != nil {
			merr = errors.Join(merr, err)
			continue
		}
		return nil
	}
	return merr
}

type OpenAICompatileClient struct {
	Chat      ChatService
	Embedding EmbeddingService

	sdk openai.Client
}

func NewOpenAICompatileClient(provider Provider) *OpenAICompatileClient {
	opts := []option.RequestOption{option.WithAPIKey(provider.GetAPIKey())}
	if baseURL := provider.GetBaseURL(); baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := &OpenAICompatileClient{
		sdk: openai.NewClient(opts...),
	}

	client.Chat = &openaiCompatileChat{client: client}
	client.Embedding = &openaiCompatileEmbedding{client: client}

	return client
}

type openaiCompatileChat struct {
	client *OpenAICompatileClient
}

type openaiChatParams struct {
	completionParams openai.ChatCompletionNewParams
	attempts         int
	schema           *compiledSchema
	checkSchema      bool
}

func (c *openaiCompatileChat) Create(ctx context.Context, req CreateChatRequest, opts ...CreateChatOption) (*ChatResponse, error) {
	params, err := buildChatParams(req, opts)
	if err != nil {
		return nil, err
	}

	var resp *openai.ChatCompletion
	createChatCompletion := func() error {
		v, err := c.client.sdk.Chat.Completions.New(ctx, params.completionParams)
		if err != nil {
			return err
		}
		resp = v
		if !params.checkSchema {
			return nil
		}
		for _, choice := range resp.Choices {
			if !responseMatchesSchema(choice.Message.Content, params.schema) {
				return fmt.Errorf("response %q did not match schema: %q", choice.Message.Content, params.schema.raw)
			}
		}
		return nil
	}
	if err := attempt(params.attempts, createChatCompletion); err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("chat completion response is nil")
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

func (c *openaiCompatileChat) CreateStream(ctx context.Context, req CreateChatRequest, opts ...CreateStreamOption) (<-chan *ChatResponse, error) {
	params, err := buildStreamParams(req, opts)
	if err != nil {
		return nil, err
	}

	stream := c.client.sdk.Chat.Completions.NewStreaming(ctx, params.completionParams)
	respCh := make(chan *ChatResponse)

	go func() {
		defer func() {
			stream.Close() //nolint:errcheck
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

func buildChatParams(req CreateChatRequest, opts []CreateChatOption) (openaiChatParams, error) {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages))
	for _, message := range req.Messages {
		param, err := buildChatMessage(message)
		if err != nil {
			return openaiChatParams{}, err
		}
		messages = append(messages, param)
	}

	params := openaiChatParams{
		completionParams: openai.ChatCompletionNewParams{
			Model:    shared.ChatModel(req.Model),
			Messages: messages,
		},
		attempts: 1,
	}
	for _, opt := range opts {
		switch opt.Type {
		case CreateChatOptionTypeTemperature:
			value, ok := opt.Value.(float64)
			if !ok {
				return openaiChatParams{}, fmt.Errorf("chat option temperature requires a float64")
			}
			params.completionParams.Temperature = openai.Float(value)

		case CreateChatOptionTypeMaxTokens:
			value, ok := opt.Value.(int)
			if !ok {
				return openaiChatParams{}, fmt.Errorf("chat option max_tokens requires an int")
			}
			params.completionParams.MaxTokens = openai.Int(int64(value))

		case CreateChatOptionTypeMaxAttempts:
			value, ok := opt.Value.(int)
			if !ok {
				return openaiChatParams{}, fmt.Errorf("chat option max_attempts requires an int")
			}
			params.attempts = value

		case CreateChatOptionTypeResponseFormat:
			schema, ok := opt.Value.(*ResponseSchema)
			if !ok || schema == nil {
				return openaiChatParams{}, fmt.Errorf("chat option schema requires a response schema")
			}
			compiled, err := compileResponseSchema(schema.Schema)
			if err != nil {
				return openaiChatParams{}, fmt.Errorf("chat option schema is invalid: %w", err)
			}
			params.schema = compiled
			params.checkSchema = true
			params.completionParams.ResponseFormat = openaiResponseFormat(schema)

		case CreateChatOptionTypeTools:
			tools, ok := opt.Value.([]Tool)
			if !ok {
				return openaiChatParams{}, fmt.Errorf("chat option tools requires a tool list")
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
						return openaiChatParams{}, fmt.Errorf("chat tool parameters are invalid: %w", err)
					}
					function.Parameters = parameters
				}
				converted = append(converted, openai.ChatCompletionFunctionTool(function))
			}
			params.completionParams.Tools = converted

		default:
			return openaiChatParams{}, fmt.Errorf("chat option %s is not supported", opt.Type)
		}
	}
	return params, nil
}

type openaiStreamParams struct {
	completionParams openai.ChatCompletionNewParams
}

func buildStreamParams(req CreateChatRequest, opts []CreateStreamOption) (openaiStreamParams, error) {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages))
	for _, message := range req.Messages {
		param, err := buildChatMessage(message)
		if err != nil {
			return openaiStreamParams{}, err
		}
		messages = append(messages, param)
	}

	params := openaiStreamParams{
		completionParams: openai.ChatCompletionNewParams{
			Model:    shared.ChatModel(req.Model),
			Messages: messages,
			StreamOptions: openai.ChatCompletionStreamOptionsParam{
				IncludeUsage: openai.Bool(true),
			},
		},
	}
	for _, opt := range opts {
		switch opt.Type {
		case CreateStreamOptionTypeTemperature:
			value, ok := opt.Value.(float64)
			if !ok {
				return openaiStreamParams{}, fmt.Errorf("stream option temperature requires a float64")
			}
			params.completionParams.Temperature = openai.Float(value)

		case CreateStreamOptionTypeMaxTokens:
			value, ok := opt.Value.(int)
			if !ok {
				return openaiStreamParams{}, fmt.Errorf("stream option max_tokens requires an int")
			}
			params.completionParams.MaxTokens = openai.Int(int64(value))

		case CreateStreamOptionTypeResponseFormat:
			format, ok := opt.Value.(*StreamResponseFormat)
			if !ok || format == nil {
				return openaiStreamParams{}, fmt.Errorf("stream option schema requires a response schema")
			}
			jsonSchema := shared.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:   "response",
				Strict: openai.Bool(format.Strict),
			}
			if len(format.Schema) > 0 {
				jsonSchema.Schema = format.Schema
			}
			params.completionParams.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
				OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
					JSONSchema: jsonSchema,
				},
			}

		case CreateStreamOptionTypeTools:
			tools, ok := opt.Value.([]Tool)
			if !ok {
				return openaiStreamParams{}, fmt.Errorf("stream option tools requires a tool list")
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
						return openaiStreamParams{}, fmt.Errorf("stream tool parameters are invalid: %w", err)
					}
					function.Parameters = parameters
				}
				converted = append(converted, openai.ChatCompletionFunctionTool(function))
			}
			params.completionParams.Tools = converted

		default:
			return openaiStreamParams{}, fmt.Errorf("stream option %s is not supported", opt.Type)
		}
	}
	return params, nil
}

func openaiResponseFormat(schema *ResponseSchema) openai.ChatCompletionNewParamsResponseFormatUnion {
	jsonSchema := shared.ResponseFormatJSONSchemaJSONSchemaParam{
		Name: "response",
	}
	if len(schema.Schema) > 0 {
		jsonSchema.Schema = schema.Schema
	}
	if !schema.LocalSchemaCheck {
		jsonSchema.Strict = openai.Bool(true)
	}
	return openai.ChatCompletionNewParamsResponseFormatUnion{
		OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
			JSONSchema: jsonSchema,
		},
	}
}

func buildChatMessage(message Message) (openai.ChatCompletionMessageParamUnion, error) {
	switch message.Role {
	case "system":
		parts, err := buildSystemMessageParts(message.Parts)
		if err != nil {
			return openai.ChatCompletionMessageParamUnion{}, err
		}
		return openai.SystemMessage(parts), nil
	case "user":
		parts, err := buildUserMessageParts(message.Parts)
		if err != nil {
			return openai.ChatCompletionMessageParamUnion{}, err
		}
		return openai.UserMessage(parts), nil
	case "assistant":
		parts, err := buildAssistantMessageParts(message.Parts)
		if err != nil {
			return openai.ChatCompletionMessageParamUnion{}, err
		}
		return openai.AssistantMessage(parts), nil
	case "developer":
		parts, err := buildDeveloperMessageParts(message.Parts)
		if err != nil {
			return openai.ChatCompletionMessageParamUnion{}, err
		}
		return openai.DeveloperMessage(parts), nil
	default:
		return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("chat message role %s is not supported", message.Role)
	}
}

func buildUserMessageParts(parts []*MessagePart) ([]openai.ChatCompletionContentPartUnionParam, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("chat message requires a part")
	}
	converted := make([]openai.ChatCompletionContentPartUnionParam, 0, len(parts))
	for _, part := range parts {
		if part == nil || (part.Text == nil && part.ImageURL == nil) {
			return nil, fmt.Errorf("chat message part requires text or an image URL")
		}
		if part.Text != nil {
			converted = append(converted, openai.TextContentPart(*part.Text))
		}
		if part.ImageURL != nil {
			converted = append(converted, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
				URL: *part.ImageURL,
			}))
		}
	}
	return converted, nil
}

func buildSystemMessageParts(parts []*MessagePart) ([]openai.ChatCompletionContentPartTextParam, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("chat message requires a part")
	}
	converted := make([]openai.ChatCompletionContentPartTextParam, 0, len(parts))
	for _, part := range parts {
		if part == nil || part.Text == nil {
			return nil, fmt.Errorf("chat message part requires text")
		}
		if part.ImageURL != nil {
			return nil, fmt.Errorf("chat message role system does not support images")
		}
		converted = append(converted, openai.ChatCompletionContentPartTextParam{Text: *part.Text})
	}
	return converted, nil
}

func buildDeveloperMessageParts(parts []*MessagePart) ([]openai.ChatCompletionContentPartTextParam, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("chat message requires a part")
	}
	converted := make([]openai.ChatCompletionContentPartTextParam, 0, len(parts))
	for _, part := range parts {
		if part == nil || part.Text == nil {
			return nil, fmt.Errorf("chat message part requires text")
		}
		if part.ImageURL != nil {
			return nil, fmt.Errorf("chat message role developer does not support images")
		}
		converted = append(converted, openai.ChatCompletionContentPartTextParam{Text: *part.Text})
	}
	return converted, nil
}

func buildAssistantMessageParts(parts []*MessagePart) ([]openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("chat message requires a part")
	}
	converted := make([]openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion, 0, len(parts))
	for _, part := range parts {
		if part == nil || part.Text == nil {
			return nil, fmt.Errorf("chat message part requires text")
		}
		if part.ImageURL != nil {
			return nil, fmt.Errorf("chat message role assistant does not support images")
		}
		converted = append(converted, openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion{
			OfText: &openai.ChatCompletionContentPartTextParam{Text: *part.Text},
		})
	}
	return converted, nil
}

func buildChatContent(content string, present bool) *string {
	if !present {
		return nil
	}
	return &content
}

func buildChatUsage(usage openai.CompletionUsage, present bool) *CompletionUsage {
	if !present {
		return nil
	}
	return &CompletionUsage{
		PromptTokens: usage.PromptTokens,
		PromptTokensDetails: CompletionUsagePromptTokensDetails{
			CacheWriteTokens: usage.PromptTokensDetails.CacheWriteTokens,
			CachedTokens:     usage.PromptTokensDetails.CachedTokens,
			AudioTokens:      usage.PromptTokensDetails.AudioTokens,
			ImageTokens:      usage.PromptTokensDetails.ImageTokens,
			TextTokens:       usage.PromptTokensDetails.TextTokens,
		},
		CompletionTokens: usage.CompletionTokens,
		CompletionTokensDetails: CompletionUsageCompletionTokensDetails{
			AcceptedPredictionTokens: usage.CompletionTokensDetails.AcceptedPredictionTokens,
			RejectedPredictionTokens: usage.CompletionTokensDetails.RejectedPredictionTokens,
			AudioTokens:              usage.CompletionTokensDetails.AudioTokens,
			TextTokens:               usage.CompletionTokensDetails.TextTokens,
			ReasoningTokens:          usage.CompletionTokensDetails.ReasoningTokens,
		},
		TotalTokens: usage.TotalTokens,
	}
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
	var promptTokens int64
	var totalTokens int64
	var hasUsage bool
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
		if resp.JSON.Usage.Valid() {
			hasUsage = true
			promptTokens += resp.Usage.PromptTokens
			totalTokens += resp.Usage.TotalTokens
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

	slices.SortFunc(results, func(a, b Embedding) int {
		return a.Index - b.Index
	})

	out := &EmbeddingResponse{
		Embeddings: results,
		Model:      req.Model,
	}
	if hasUsage {
		out.Usage = &CompletionUsage{
			PromptTokens: promptTokens,
			TotalTokens:  totalTokens,
		}
	}
	return out, nil
}
