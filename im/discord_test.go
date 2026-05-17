package im

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestDiscord(t *testing.T) {
	channelURL := os.Getenv("TEST_DISCORD_CHANNEL")
	if channelURL == "" {
		t.Skip("TEST_DISCORD_CHANNEL not set")
	}

	client := NewDiscordClient(channelURL, "azusayn")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	if err := client.SendMessage(ctx, []string{"Hello, from azusayn"}); err != nil {
		t.Fatal(err)
	}

}
