package llm

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestGenai(t *testing.T) {
	ctx := context.Background()

	extractor, err := NewConInfoExtractor(ctx, "gemma-4-26b-a4b-it", os.Getenv("API_KEY"))
	if err != nil {
		t.Fatal(err)
	}

	imagePath := "event.jpg"
	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		t.Fatal(err)
	}

	promptPath := "prompt.txt"
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
