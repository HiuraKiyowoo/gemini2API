package upstream

import (
	"context"
	"encoding/json"
	"strings"
)

// ChatClient is the narrow upstream contract required to execute one Qwen turn.
type ChatClient interface {
	CreateChat(ctx context.Context, token, model, chatType string) (string, error)
	DeleteChat(ctx context.Context, token, chatID string) bool
	StreamChat(ctx context.Context, token, chatID string, payload map[string]any, onEvent func(Event) error) error
}

type ExecuteOptions struct {
	Token           string
	Model           string
	Prompt          string
	ChatType        string
	HasCustomTools  bool
	Files           []map[string]any
	MediaOptions    map[string]any
	ThinkingEnabled *bool
	EnableSearch    bool
	DeleteWhenDone  bool
}

// ExecuteTurn creates a chat, streams one payload, and optionally deletes the
// temporary upstream conversation.
func ExecuteTurn(ctx context.Context, client ChatClient, opts ExecuteOptions, onEvent func(Event) error) (string, error) {
	chatType := NormalizeChatType(opts.ChatType)
	chatID, err := client.CreateChat(ctx, opts.Token, opts.Model, chatType)
	if err != nil {
		return "", err
	}
	if opts.DeleteWhenDone {
		defer client.DeleteChat(context.Background(), opts.Token, chatID)
	}
	payload := BuildChatPayload(chatID, opts.Model, opts.Prompt, opts.HasCustomTools, opts.Files, chatType, opts.MediaOptions, opts.ThinkingEnabled, opts.EnableSearch)
	if err := client.StreamChat(ctx, opts.Token, chatID, payload, onEvent); err != nil {
		return chatID, err
	}
	return chatID, nil
}

func FormatUpstreamError(obj map[string]any) string {
	if obj == nil {
		return ""
	}
	if errorObj, ok := obj["error"].(map[string]any); ok {
		code := firstString(errorObj["code"])
		if code == "" {
			code = "upstream_error"
		}
		details := firstString(errorObj["details"], errorObj["message"], errorObj["type"])
		return "Gemini upstream error code=" + code + " details=" + details
	}
	if errorText, ok := obj["error"].(string); ok && strings.TrimSpace(errorText) != "" {
		return "Gemini upstream error: " + errorText
	}
	return ""
}

func ExtractUpstreamError(text string) string {
	if strings.Contains(text, "accounts.google.com") || strings.Contains(text, "ServiceLogin") {
		return "Google Gemini session cookies invalid or expired. Silakan perbarui cookie akun (__Secure-1PSID, SAPISID)."
	}
	if strings.Contains(text, "429") || strings.Contains(text, "RESOURCE_EXHAUSTED") {
		return "Google Gemini rate limit / quota exceeded (429)."
	}
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if line == "" || line == "[DONE]" || !strings.HasPrefix(line, "{") {
			continue
		}
		var obj map[string]any
		if json.Unmarshal([]byte(line), &obj) != nil {
			continue
		}
		if message := FormatUpstreamError(obj); message != "" {
			return message
		}
	}
	return ""
}

// Verifier is implemented by transports that can answer "is this account
// usable?" with a human-readable diagnosis (and, when blocked, the reason).
type Verifier interface {
	Verify(ctx context.Context, token string) (bool, string)
}

var (
	_ Verifier = (*CodeAssistClient)(nil)
	_ Verifier = (*AntigravityClient)(nil)
)
