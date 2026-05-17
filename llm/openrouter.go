package llm

import (
	"context"

	or "github.com/revrost/go-openrouter"
)

type OpenRouterClient struct {
	*or.Client
}

func NewOpenRouterClient(apiKey string) *OpenRouterClient {
	return &OpenRouterClient{
		Client: or.NewClient(apiKey),
	}
}

type OpenRouterResponse struct {
	Text            string
	PromptTokens    int
	CompletionToken int
	Cost            float64
}

func (client *OpenRouterClient) SendMessage(text string, model string) (*OpenRouterResponse, error) {
	resp, err := client.CreateChatCompletion(
		context.Background(),
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
