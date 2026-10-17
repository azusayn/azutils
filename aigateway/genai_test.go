package aigateway

import (
	"context"
	"encoding/base64"
	"mime"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenai(t *testing.T) {
	ctx := context.Background()

	apiKey := os.Getenv("TEST_GENAI_API_KEY")
	if apiKey == "" {
		t.Skip("TEST_GENAI_API_KEY not set")
	}

	client, err := NewGenaiClient(ctx, NewOpenAICompatibleProvider(BaseProvider{APIKey: apiKey}))
	if err != nil {
		t.Fatal(err)
	}

	imagePath := os.Getenv("TEST_GENAI_IMAGE_PATH")
	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	mimeType := mime.TypeByExtension(filepath.Ext(imagePath))
	if mimeType == "" {
		t.Fatal("image has no MIME type")
	}
	imageURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(imageBytes)

	promptPath := os.Getenv("TEST_GENAI_PROMPT_PATH")
	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(promptBytes)

	ctx, cancel := context.WithTimeout(ctx, time.Second*20)
	defer cancel()
	resp, err := client.Chat.Create(ctx, CreateChatRequest{
		Model: "gemma-4-26b-a4b-it",
		Messages: []Message{
			{Role: "system", Parts: []*MessagePart{{Text: &prompt}}},
			{Role: "user", Parts: []*MessagePart{{ImageURL: &imageURL}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == nil {
		t.Fatal("empty response")
	}

	t.Log(*resp.Choices[0].Message.Content)
}
