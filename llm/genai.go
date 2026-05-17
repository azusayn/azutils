package llm

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"path/filepath"
	"time"

	"google.golang.org/genai"
)

const GenaiRespSchema string = `
{
	"type": "object",
	"properties": {
		"response": {
			"type": "string"
		}
	}
}
`

type ConInfoExtractor struct {
	// available model names in Google AI Studio.
	// e.g. "gemma-4-26b-a4b-it".
	Model  string
	Client *genai.Client
	Schema genai.Schema
}

func NewConInfoExtractor(ctx context.Context, model string, apiKey string) (*ConInfoExtractor, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		Backend: genai.BackendGeminiAPI,
		APIKey:  apiKey,
	})
	if err != nil {
		return nil, err
	}
	extractor := &ConInfoExtractor{
		Model:  model,
		Client: client,
	}
	if err := json.Unmarshal([]byte(GenaiRespSchema), &extractor.Schema); err != nil {
		return nil, err
	}
	return extractor, nil
}

type ExtractResponse struct {
	TokenUsage   int32
	ModelVersion string
	CreateTime   time.Time
	ResponseID   string
	EventText    string
	Latency      time.Duration
}

func (e *ConInfoExtractor) Extract(ctx context.Context, prompt string, imageBytes []byte, imageFileName string) (*ExtractResponse, error) {
	start := time.Now()
	config := &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				{Text: prompt},
			},
		},
		ResponseMIMEType: "application/json",
		ResponseSchema:   &e.Schema,
	}

	content := &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			{
				InlineData: &genai.Blob{
					Data:     imageBytes,
					MIMEType: mime.TypeByExtension(filepath.Ext(imageFileName)),
				},
			},
		},
	}

	resp, err := e.Client.Models.GenerateContent(ctx, e.Model, []*genai.Content{content}, config)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.New("empty response")
	}

	var tokenUsage int32
	if resp.UsageMetadata != nil {
		tokenUsage = resp.UsageMetadata.TotalTokenCount
	}

	var eventText string
	if cands := resp.Candidates; len(cands) != 0 && cands[0].Content != nil {
		if parts := cands[0].Content.Parts; len(parts) != 0 {
			genaiResp := struct {
				Response string `json:"response"`
			}{}
			if err := json.Unmarshal([]byte(parts[0].Text), &genaiResp); err != nil {
				return nil, err
			}
			eventText = genaiResp.Response
		}
	}

	elasped := time.Since(start)

	return &ExtractResponse{
		TokenUsage:   tokenUsage,
		CreateTime:   resp.CreateTime,
		ModelVersion: resp.ModelVersion,
		ResponseID:   resp.ResponseID,
		EventText:    eventText,
		Latency:      elasped,
	}, nil
}
