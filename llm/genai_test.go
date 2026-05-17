package llm

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestGenai(t *testing.T) {
	ctx := context.Background()

	apiKey := os.Getenv("TEST_GENAI_API_KEY")
	if apiKey == "" {
		t.Skip("TEST_GENAI_API_KEY not set")
	}

	extractor, err := NewConInfoExtractor(ctx, "gemma-4-26b-a4b-it", apiKey)
	if err != nil {
		t.Fatal(err)
	}

	imagePath := os.Getenv("TEST_GENAI_IMAGE_PATH")
	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}

	promptPath := os.Getenv("TEST_GENAI_PROMPT_PATH")
	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second*20)
	defer cancel()
	resp, err := extractor.Extract(ctx, string(promptBytes), imageBytes, imagePath)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(resp.EventText)
}
