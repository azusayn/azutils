package im

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larksdk "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type Challenge struct {
	Challenge string `json:"challenge"`
}

type SenderID struct {
	OpenID string `json:"open_id"`
	UserID string `json:"user_id"`
}

type Message struct {
	Content  string    `json:"content"`
	Mentions []Mention `json:"mentions"`
	ChatID   string    `json:"chat_id"`
}

type Mention struct {
	ID  SenderID `json:"id"`
	Key string   `json:"key"`
}

type Sender struct {
	SenderID SenderID `json:"sender_id"`
}

type Event struct {
	Sender  Sender  `json:"sender"`
	Message Message `json:"message"`
}

type Header struct {
	EventType string `json:"event_type"`
	EventID   string `json:"event_id"`
}

type ReciveMessageRequest struct {
	Header Header `json:"header"`
	Event  Event  `json:"event"`
}

type LarkClient struct {
	*larksdk.Client
	*slog.Logger
}

func NewLarkClient(appID, appSecret string, logger *slog.Logger) *LarkClient {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return &LarkClient{
		Client: lark.NewClient(appID, appSecret),
		Logger: logger,
	}
}

func (client *LarkClient) SendMessage(ctx context.Context, chatID, text string) error {
	content := parseMarkdownToLarkPost(text)
	contentBytes, _ := json.Marshal(content)

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(larkim.MsgTypePost).
			Content(string(contentBytes)).
			Build()).
		Build()

	resp, err := client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("logId: %s, error response: \n%s", resp.RequestId(), larkcore.Prettify(resp.CodeError))
	}

	return nil
}

func parseMarkdownToLarkPost(text string) map[string]interface{} {
	var contentBlocks [][]map[string]interface{}

	codeBlockRegex := regexp.MustCompile("```([a-zA-Z]*)\n([\\s\\S]*?)```")
	matches := codeBlockRegex.FindAllStringSubmatchIndex(text, -1)

	if len(matches) == 0 {
		blocks := parseTextToParagraphs(text)
		contentBlocks = append(contentBlocks, blocks...)
	} else {
		lastIndex := 0
		for _, match := range matches {
			if match[0] > lastIndex {
				beforeText := text[lastIndex:match[0]]
				blocks := parseTextToParagraphs(beforeText)
				contentBlocks = append(contentBlocks, blocks...)
			}

			language := ""
			if match[3] > match[2] {
				language = text[match[2]:match[3]]
			}
			code := strings.TrimSuffix(text[match[4]:match[5]], "\n")

			contentBlocks = append(contentBlocks, []map[string]interface{}{
				{"tag": "code_block", "language": language, "text": code},
			})

			lastIndex = match[1]
		}

		if lastIndex < len(text) {
			afterText := text[lastIndex:]
			blocks := parseTextToParagraphs(afterText)
			contentBlocks = append(contentBlocks, blocks...)
		}
	}

	if len(contentBlocks) == 0 {
		contentBlocks = append(contentBlocks, []map[string]interface{}{
			{"tag": "text", "text": " "},
		})
	}

	return map[string]interface{}{
		"zh_cn": map[string]interface{}{
			"title":   "",
			"content": contentBlocks,
		},
	}
}

func parseTextToParagraphs(text string) [][]map[string]interface{} {
	var paragraphs [][]map[string]interface{}

	text = strings.ReplaceAll(text, "---", "")
	text = strings.ReplaceAll(text, "`", "")
	boldRegex := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	text = boldRegex.ReplaceAllString(text, "$1")
	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	text = linkRegex.ReplaceAllString(text, "$1")
	headerRegex := regexp.MustCompile(`(?m)^#{1,6}\s+`)
	text = headerRegex.ReplaceAllString(text, "")

	lines := strings.Split(text, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		paragraphs = append(paragraphs, []map[string]interface{}{
			{"tag": "text", "text": line},
		})
	}

	return paragraphs
}

var (
	eventIDmap sync.Map
)

func HandleLarkWebhook(larkClient *LarkClient, onMessage func(string) (string, error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			larkClient.Error("failed to read from request", slog.Any("err", err))
			return
		}
		defer r.Body.Close()

		var challenge Challenge
		if json.Unmarshal(body, &challenge) == nil && challenge.Challenge != "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"challenge": challenge.Challenge})
			return
		}

		var req ReciveMessageRequest
		if err := json.Unmarshal(body, &req); err != nil {
			larkClient.Error("failed to unmarshal json", slog.Any("err", err))
			return
		}
		defer func() {
			// return 'code:0' to prevent retrying from feishu...
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"code": "0"})
		}()
		eventID := req.Header.EventID
		if _, ok := eventIDmap.Load(eventID); ok {
			// ignore redundant requests.
			return
		}
		eventIDmap.Store(eventID, true)

		switch req.Header.EventType {
		case "im.message.receive_v1":
			var contentJson map[string]string
			if err := json.Unmarshal([]byte(req.Event.Message.Content), &contentJson); err != nil {
				larkClient.Error("failed to unmarshal json", slog.Any("err", err))
				return
			}

			text := contentJson["text"]
			botMentioned := false
			for _, mention := range req.Event.Message.Mentions {
				text = strings.Replace(text, mention.Key, "", 1)
				// bot's user_id is empty string.
				// TOOD: this is only for messages in chat room.
				if mention.ID.UserID == "" {
					botMentioned = true
				}
			}

			if botMentioned && text != "" {
				msg, err := onMessage(text)
				if err != nil {
					larkClient.Error("failed to get response message", slog.Any("err", err))
					return
				}
				// send message to lark bot.
				if err := larkClient.SendMessage(r.Context(), req.Event.Message.ChatID, msg); err != nil {
					larkClient.Error("failed to send message to lark server", slog.Any("err", err))
					return
				}
			}

		default:
			larkClient.Warn("unknown lark event", slog.Any("event", req.Header.EventType))
		}
	}
}
