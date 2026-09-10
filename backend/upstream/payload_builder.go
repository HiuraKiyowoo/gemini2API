package upstream

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// NormalizeChatType maps API-facing aliases onto Qwen upstream chat types.
func NormalizeChatType(chatType string) string {
	if chatType == "image_gen" || chatType == "t2i" {
		return "t2i"
	}
	return chatType
}

// BuildChatPayload builds the Qwen /api/v2/chat/completions request body.
func BuildChatPayload(chatID, model, content string, hasCustomTools bool, files []map[string]any, chatType string, imageOptions map[string]any, thinkingEnabled *bool, enableSearch bool) map[string]any {
	if chatType == "" {
		chatType = "t2t"
	}
	ts := time.Now().Unix()
	isImage := chatType == "image_gen" || chatType == "t2i"
	isVideo := chatType == "t2v"
	featureConfig := map[string]any{}
	messageChatType := chatType
	subChatType := chatType
	messageMeta := map[string]any{"subChatType": chatType}

	if isImage {
		ratio := imageRatio(imageOptions)
		featureConfig = map[string]any{
			"thinking_enabled": false, "output_schema": "phase", "auto_thinking": false,
			"thinking_mode": "off", "auto_search": false, "code_interpreter": false,
			"function_calling": false, "plugins_enabled": true, "image_generation": true,
			"default_aspect_ratio": ratio,
		}
		messageChatType = "t2t"
		subChatType = "t2i"
		messageMeta = map[string]any{"subChatType": "t2i", "mode": "image_generation", "aspectRatio": ratio, "size": ratio}
	} else if isVideo {
		ratio := imageRatio(imageOptions)
		featureConfig = map[string]any{
			"thinking_enabled": false, "output_schema": "phase", "auto_thinking": false,
			"thinking_mode": "off", "auto_search": false, "code_interpreter": false,
			"function_calling": false, "plugins_enabled": true, "video_generation": true,
			"default_aspect_ratio": ratio,
		}
		messageChatType = "t2v"
		subChatType = "t2v"
		messageMeta = map[string]any{"subChatType": "t2v", "mode": "video_generation", "aspectRatio": ratio, "size": ratio}
	} else {
		thinking := true
		autoThinking := true
		thinkingMode := "Auto"
		if hasCustomTools {
			thinking = false
			autoThinking = false
			thinkingMode = "Disabled"
		}
		if thinkingEnabled != nil {
			thinking = *thinkingEnabled
			autoThinking = *thinkingEnabled
			if thinking {
				thinkingMode = "Auto"
			} else {
				thinkingMode = "Disabled"
			}
		}
		featureConfig = map[string]any{
			"thinking_enabled": thinking, "output_schema": "phase", "research_mode": "normal",
			"auto_thinking": autoThinking, "thinking_mode": thinkingMode, "thinking_format": "summary",
			"auto_search": enableSearch || chatType == "deep_research", "code_interpreter": false,
			"plugins_enabled": false, "function_calling": false, "enable_tools": false,
			"enable_function_call": false, "tool_choice": "none",
		}
	}

	if files == nil {
		files = []map[string]any{}
	}
	payload := map[string]any{
		"stream": true, "version": "2.1", "incremental_output": true, "chat_id": chatID,
		"chat_mode": "normal", "model": model, "parent_id": nil,
		"messages": []map[string]any{{
			"fid": randomID(), "parentId": nil, "childrenIds": []string{randomID()},
			"role": "user", "content": content, "user_action": "chat", "files": files,
			"timestamp": ts, "models": []string{model}, "chat_type": messageChatType,
			"feature_config": featureConfig, "extra": map[string]any{"meta": messageMeta},
			"sub_chat_type": subChatType, "parent_id": nil,
		}},
		"timestamp": ts,
	}
	if isImage || isVideo {
		payload["size"] = imageRatio(imageOptions)
	}
	payload["prompt"] = content
	payload["model"] = model
	payload["chat_id"] = chatID
	if thinkingEnabled != nil {
		payload["thinking_enabled"] = *thinkingEnabled
	}
	return payload
}

// BuildGeminiPayload builds Google Gemini Web StreamGenerate payload.
func BuildGeminiPayload(prompt, model string, thinking bool, atToken string) (string, error) {
	inner := make([]any, 80)
	inner[0] = []any{prompt, 0, nil, nil, nil, nil, 0}
	inner[1] = []string{"en"}
	inner[2] = []any{"", "", "", nil, nil, nil, nil, nil, nil, ""}
	inner[6] = []int{0}
	inner[7] = 1
	inner[10] = 1
	inner[11] = 0

	modelCode := 1 // 1: gemini-flash, 2: gemini-thinking, 3: gemini-pro
	switch strings.ToLower(model) {
	case "gemini-2.5-pro", "gemini-pro", "gpt-4o", "claude-3-5-sonnet", "claude-3.5-sonnet":
		modelCode = 3
	case "gemini-2.5-flash-thinking", "gemini-thinking":
		modelCode = 2
		thinking = true
	default:
		if thinking {
			modelCode = 2
		} else {
			modelCode = 1
		}
	}

	if thinking {
		inner[17] = [][]int{{0}}
	} else {
		inner[17] = [][]int{{4}}
	}

	inner[18] = 0
	inner[27] = 1
	inner[30] = []int{4}
	inner[41] = []int{2}
	inner[53] = 0
	inner[59] = UUID()
	inner[61] = []any{}
	inner[68] = 1
	inner[79] = modelCode

	innerBytes, err := json.Marshal(inner)
	if err != nil {
		return "", err
	}

	outer := []any{nil, string(innerBytes)}
	outerBytes, err := json.Marshal(outer)
	if err != nil {
		return "", err
	}

	val := url.Values{}
	val.Set("f.req", string(outerBytes))
	val.Set("at", atToken)
	return val.Encode(), nil
}

func UUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func imageRatio(options map[string]any) string {
	for _, key := range []string{"ratio", "aspect_ratio", "aspectRatio"} {
		if v, ok := options[key].(string); ok && v != "" {
			return v
		}
	}
	return "1:1"
}

func randomID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(buf)
}
