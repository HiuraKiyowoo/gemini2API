package services

import (
	cryptorand "crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gemini2api-go/upstream"
)

const QwenBaseURL = "https://gemini.google.com"

type UpstreamEvent struct {
	Type          string
	Phase         string
	Content       string
	ReasoningText string
	Status        string
	Extra         map[string]any
	Raw           map[string]any
}

type TokenVerifyResult struct {
	Valid      bool
	StatusCode string
	Message    string
}

func QwenHeaders(token string) http.Header {
	headers := http.Header{}
	headers.Set("Accept", "application/json, text/event-stream")
	headers.Set("Content-Type", "application/json")
	headers.Set("User-Agent", "Mozilla/5.0 gemini2api-go")
	headers.Set("x-request-id", QwenRequestID())
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	return headers
}

func QwenRequestID() string {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		now := time.Now().UnixNano()
		return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", uint32(now), uint16(now>>32), uint16(now>>48), uint16(now>>16), uint64(now))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func ParseQwenJSONEvent(data string) []UpstreamEvent {
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		return nil
	}
	parsed := upstream.ParseQwenEvent(obj)
	out := make([]UpstreamEvent, 0, len(parsed))
	for _, evt := range parsed {
		out = append(out, UpstreamEvent{
			Type:          evt.Type,
			Phase:         evt.Phase,
			Content:       evt.Content,
			ReasoningText: evt.ReasoningText,
			Status:        evt.Status,
			Extra:         evt.Extra,
			Raw:           evt.Raw,
		})
	}
	return out
}

func FormatUpstreamError(obj map[string]any) string {
	if obj == nil {
		return ""
	}
	if errorObj, ok := obj["error"].(map[string]any); ok {
		code := qwenFirstString(errorObj["code"])
		if code == "" {
			code = "upstream_error"
		}
		details := qwenFirstString(errorObj["details"], errorObj["message"], errorObj["type"])
		return "Gemini upstream error code=" + code + " details=" + details
	}
	if errText, ok := obj["error"].(string); ok && strings.TrimSpace(errText) != "" {
		return "Gemini upstream error details=" + errText
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

func qwenFirstString(values ...any) string {
	for _, value := range values {
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
