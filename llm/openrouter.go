package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	or "github.com/revrost/go-openrouter"
)

type OpenRouterClient struct {
	*or.Client
	apiKey string
}

func NewOpenRouterClient(apiKey string) *OpenRouterClient {
	return &OpenRouterClient{
		Client: or.NewClient(apiKey),
		apiKey: apiKey,
	}
}

type OpenRouterResponse struct {
	Text            string
	PromptTokens    int
	CompletionToken int
	Cost            float64
}

func (client *OpenRouterClient) SendMessage(ctx context.Context, text string, model string) (*OpenRouterResponse, error) {
	resp, err := client.CreateChatCompletion(
		ctx,
		or.ChatCompletionRequest{
			// e.g. deepseek/deepseek-v3.2, openai/gpt-5-chat ...
			Model: model,
			Messages: []or.ChatCompletionMessage{
				or.UserMessage(text),
			},
		},
	)
	if err != nil {
		return nil, err
	}

	return &OpenRouterResponse{
		Text:            resp.Choices[0].Message.Content.Text,
		PromptTokens:    resp.Usage.PromptTokens,
		CompletionToken: resp.Usage.CompletionTokens,
		Cost:            resp.Usage.Cost,
	}, nil
}

type OpenRouterCredits struct {
	Data struct {
		Label      string  `json:"label"`
		Usage      float64 `json:"usage"`
		Limit      float64 `json:"limit"`
		IsFreeTier bool    `json:"is_free_tier"`
		RateLimit  struct {
			Requests int    `json:"requests"`
			Interval string `json:"interval"`
		} `json:"rate_limit"`
	} `json:"data"`
}

func (client *OpenRouterClient) GetCredits(ctx context.Context) (*OpenRouterCredits, error) {
	req, err := http.NewRequest(http.MethodGet, "https://openrouter.ai/api/v1/auth/key", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+client.apiKey)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api responded with status %d", resp.StatusCode)
	}

	var credits OpenRouterCredits
	if err := json.NewDecoder(resp.Body).Decode(&credits); err != nil {
		return nil, fmt.Errorf("failed to parse json response: %w", err)
	}

	return &credits, nil
}
