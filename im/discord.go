package im

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type DiscordMessage struct {
	Username        string          `json:"username"`
	AvatarUrl       string          `json:"avatar_url"`
	Content         string          `json:"content"`
	Embeds          []Embed         `json:"embeds"`
	AllowedMentions AllowedMentions `json:"allowed_mentions"`
}

type Embed struct {
	Title       string    `json:"title"`
	Url         string    `json:"url"`
	Description string    `json:"description"`
	Color       string    `json:"color"`
	Author      Author    `json:"author"`
	Fields      []Field   `json:"fields"`
	Thumbnail   Thumbnail `json:"thumbnail"`
	Image       Image     `json:"image"`
	Footer      Footer    `json:"footer"`
}

type Author struct {
	Name    string `json:"name"`
	Url     string `json:"url"`
	IconUrl string `json:"icon_url"`
}

type Field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type Thumbnail struct {
	Url string `json:"url"`
}

type Image struct {
	Url string `json:"url"`
}

type Footer struct {
	Text    string `json:"text"`
	IconUrl string `json:"icon_url"`
}

type AllowedMentions struct {
	Parse []string `json:"parse"`
	Users []string `json:"users"`
	Roles []string `json:"roles"`
}

type DiscordClient struct {
	ChannelURL string
	// DisplayName is the name shown as the message sender in Discord.
	DisplayName string
}

func NewDiscordClient(channelURL string, displayName string) *DiscordClient {
	return &DiscordClient{
		ChannelURL:  channelURL,
		DisplayName: displayName,
	}
}

func (client *DiscordClient) SendMessage(ctx context.Context, messages []string) error {
	for _, m := range messages {
		msg := DiscordMessage{
			Username: client.DisplayName,
			Content:  m,
		}

		payload := new(bytes.Buffer)
		err := json.NewEncoder(payload).Encode(msg)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.ChannelURL, payload)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if code := resp.StatusCode; code != http.StatusOK && code != http.StatusNoContent {
			responseBody, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			return fmt.Errorf("code: %d: %s", code, string(responseBody))
		}

	}
	return nil
}
