package aigateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"path"
	"strings"

	"google.golang.org/genai"
)

type GenaiClient struct {
	Chat      ChatService
	Embedding EmbeddingService

	sdk *genai.Client
}

func NewGenaiClient(ctx context.Context, provider Provider) (*GenaiClient, error) {
	config := &genai.ClientConfig{
		Backend: genai.BackendGeminiAPI,
		APIKey:  provider.GetAPIKey(),
	}
	if baseURL := provider.GetBaseURL(); baseURL != "" {
		config.HTTPOptions = genai.HTTPOptions{BaseURL: baseURL}
	}
	sdk, err := genai.NewClient(ctx, config)
	if err != nil {
		return nil, err
	}

	client := &GenaiClient{sdk: sdk}
	client.Chat = &genaiChat{client: client}
	client.Embedding = &genaiEmbedding{}
	return client, nil
}

type genaiChat struct {
	client *GenaiClient
}

func (c *genaiChat) Create(ctx context.Context, req CreateChatRequest, opts ...CreateChatOption) (*ChatResponse, error) {
	config := &genai.GenerateContentConfig{}
	contents := make([]*genai.Content, 0, len(req.Messages))
	for _, message := range req.Messages {
		switch message.Role {
		case "system":
			parts, err := buildGenaiSystemParts(message.Parts)
			if err != nil {
				return nil, err
			}
			if config.SystemInstruction == nil {
				config.SystemInstruction = &genai.Content{}
			}
			config.SystemInstruction.Parts = append(config.SystemInstruction.Parts, parts...)
		case "user":
			content, err := buildGenaiUserContent(message.Parts)
			if err != nil {
				return nil, err
			}
			contents = append(contents, content)
		case "assistant":
			content, err := buildGenaiAssistantContent(message.Parts)
			if err != nil {
				return nil, err
			}
			contents = append(contents, content)
		default:
			return nil, fmt.Errorf("chat message role %s is not supported", message.Role)
		}
	}

	for _, opt := range opts {
		switch opt.Type {
		case CreateChatOptionTypeTemperature:
			value, ok := opt.Value.(float64)
			if !ok {
				return nil, fmt.Errorf("chat option temperature requires a float64")
			}
			temperature := float32(value)
			config.Temperature = &temperature

		case CreateChatOptionTypeMaxTokens:
			value, ok := opt.Value.(int)
			if !ok {
				return nil, fmt.Errorf("chat option max_tokens requires an int")
			}
			config.MaxOutputTokens = int32(value)

		case CreateChatOptionTypeResponseFormat:
			schema, ok := opt.Value.(*ResponseSchema)
			if !ok || schema == nil {
				return nil, fmt.Errorf("chat option schema requires a response schema")
			}
			config.ResponseMIMEType = "application/json"
			if len(schema.Schema) == 0 {
				break
			}
			var responseSchema any
			if err := json.Unmarshal(schema.Schema, &responseSchema); err != nil {
				return nil, fmt.Errorf("chat option schema is invalid: %w", err)
			}
			config.ResponseJsonSchema = responseSchema

		case CreateChatOptionTypeTools:
			tools, ok := opt.Value.([]Tool)
			if !ok {
				return nil, fmt.Errorf("chat option tools requires a tool list")
			}
			declarations := make([]*genai.FunctionDeclaration, 0, len(tools))
			for _, tool := range tools {
				declaration := &genai.FunctionDeclaration{
					Name:        tool.Name,
					Description: tool.Description,
				}
				if len(tool.Parameters) > 0 {
					var parameters any
					if err := json.Unmarshal(tool.Parameters, &parameters); err != nil {
						return nil, fmt.Errorf("chat tool parameters are invalid: %w", err)
					}
					declaration.ParametersJsonSchema = parameters
				}
				declarations = append(declarations, declaration)
			}
			config.Tools = []*genai.Tool{{
				FunctionDeclarations: declarations,
			}}

		default:
			return nil, fmt.Errorf("chat option %s is not supported", opt.Type)
		}
	}

	resp, err := c.client.sdk.Models.GenerateContent(ctx, req.Model, contents, config)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("empty response")
	}
	return buildGenaiChatResponse(resp)
}

func (c *genaiChat) CreateStream(context.Context, CreateChatRequest, ...CreateStreamOption) (<-chan *ChatResponse, error) {
	return nil, errors.New("genai chat stream is not implemented")
}

func buildGenaiSystemParts(parts []*MessagePart) ([]*genai.Part, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("chat message requires a part")
	}
	converted := make([]*genai.Part, 0, len(parts))
	for _, part := range parts {
		if part == nil || part.Text == nil {
			return nil, fmt.Errorf("chat message part requires text")
		}
		if part.ImageURL != nil {
			return nil, fmt.Errorf("chat message role system does not support images")
		}
		converted = append(converted, genai.NewPartFromText(*part.Text))
	}
	return converted, nil
}

func buildGenaiUserContent(parts []*MessagePart) (*genai.Content, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("chat message requires a part")
	}
	converted := make([]*genai.Part, 0, len(parts))
	for _, part := range parts {
		if part == nil || (part.Text == nil && part.ImageURL == nil) {
			return nil, fmt.Errorf("chat message part requires text or an image URL")
		}
		if part.Text != nil {
			converted = append(converted, genai.NewPartFromText(*part.Text))
		}
		if part.ImageURL != nil {
			image, err := buildGenaiImagePart(*part.ImageURL)
			if err != nil {
				return nil, err
			}
			converted = append(converted, image)
		}
	}
	return &genai.Content{
		Role:  genai.RoleUser,
		Parts: converted,
	}, nil
}

func buildGenaiAssistantContent(parts []*MessagePart) (*genai.Content, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("chat message requires a part")
	}
	converted := make([]*genai.Part, 0, len(parts))
	for _, part := range parts {
		if part == nil || part.Text == nil {
			return nil, fmt.Errorf("chat message part requires text")
		}
		if part.ImageURL != nil {
			return nil, fmt.Errorf("chat message role assistant does not support images")
		}
		converted = append(converted, genai.NewPartFromText(*part.Text))
	}
	return &genai.Content{
		Role:  genai.RoleModel,
		Parts: converted,
	}, nil
}

func buildGenaiImagePart(imageURL string) (*genai.Part, error) {
	if strings.HasPrefix(imageURL, "data:") {
		mimeType, data, err := decodeGenaiDataImage(imageURL)
		if err != nil {
			return nil, err
		}
		return &genai.Part{
			InlineData: &genai.Blob{
				Data:     data,
				MIMEType: mimeType,
			},
		}, nil
	}

	parsed, err := url.Parse(imageURL)
	if err != nil {
		return nil, fmt.Errorf("chat message image URL is invalid: %w", err)
	}
	mimeType := mime.TypeByExtension(path.Ext(parsed.Path))
	if mimeType == "" {
		return nil, fmt.Errorf("chat message image URL has no MIME type")
	}
	return genai.NewPartFromURI(imageURL, mimeType), nil
}

func decodeGenaiDataImage(imageURL string) (string, []byte, error) {
	rest, ok := strings.CutPrefix(imageURL, "data:")
	if !ok {
		return "", nil, fmt.Errorf("chat message image URL is invalid")
	}
	header, payload, ok := strings.Cut(rest, ",")
	if !ok {
		return "", nil, fmt.Errorf("chat message image URL is invalid")
	}
	mimeType, _, _ := strings.Cut(header, ";")
	if mimeType == "" {
		return "", nil, fmt.Errorf("chat message image URL has no MIME type")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", nil, fmt.Errorf("chat message image URL is invalid: %w", err)
	}
	return mimeType, data, nil
}

func buildGenaiChatResponse(resp *genai.GenerateContentResponse) (*ChatResponse, error) {
	raw, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}

	choices := make([]Choice, 0, len(resp.Candidates))
	for _, candidate := range resp.Candidates {
		if candidate == nil {
			continue
		}
		var text string
		var role string
		if candidate.Content != nil {
			role = candidate.Content.Role
			for _, part := range candidate.Content.Parts {
				if part == nil {
					continue
				}
				text += part.Text
			}
		}
		choices = append(choices, Choice{
			Message: ChoiceMessage{
				Content: &text,
				Role:    role,
			},
		})
	}

	return &ChatResponse{
		Choices: choices,
		Model:   resp.ModelVersion,
		Usage:   buildGenaiUsage(resp.UsageMetadata),
		Raw:     raw,
	}, nil
}

func buildGenaiUsage(usage *genai.GenerateContentResponseUsageMetadata) *CompletionUsage {
	if usage == nil {
		return nil
	}
	// Ref: https://github.com/BerriAI/litellm/issues/23731
	// Ref: https://docs.cloud.google.com/python/docs/reference/aiplatform/latest/google.cloud.aiplatform_v1beta1.types.UsageMetadata
	prompt := countModalities(usage.PromptTokensDetails)
	toolUse := countModalities(usage.ToolUsePromptTokensDetails)
	candidates := countModalities(usage.CandidatesTokensDetails)
	return &CompletionUsage{
		PromptTokens: int64(usage.PromptTokenCount) + int64(usage.ToolUsePromptTokenCount),
		PromptTokensDetails: CompletionUsagePromptTokensDetails{
			CachedTokens: int64(usage.CachedContentTokenCount),
			AudioTokens:  prompt.audio + toolUse.audio,
			ImageTokens:  prompt.image + toolUse.image,
			TextTokens:   prompt.text + toolUse.text,
		},
		CompletionTokens: int64(usage.CandidatesTokenCount) + int64(usage.ThoughtsTokenCount),
		CompletionTokensDetails: CompletionUsageCompletionTokensDetails{
			AudioTokens:     candidates.audio,
			TextTokens:      candidates.text,
			ReasoningTokens: int64(usage.ThoughtsTokenCount),
		},
		TotalTokens: int64(usage.TotalTokenCount),
	}
}

type modalityCounts struct {
	text  int64
	image int64
	audio int64
}

func countModalities(details []*genai.ModalityTokenCount) modalityCounts {
	var counts modalityCounts
	for _, detail := range details {
		if detail == nil {
			continue
		}
		count := int64(detail.TokenCount)
		switch detail.Modality {
		case genai.MediaModalityText:
			counts.text += count
		case genai.MediaModalityImage:
			counts.image += count
		case genai.MediaModalityAudio:
			counts.audio += count
		}
	}
	return counts
}

type genaiEmbedding struct{}

func (e *genaiEmbedding) Create(context.Context, CreateEmbeddingRequest) (*EmbeddingResponse, error) {
	return nil, errors.New("genai embedding is not implemented")
}
