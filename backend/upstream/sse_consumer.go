package upstream

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// Event is a normalized chunk emitted by Qwen's SSE stream.
type Event struct {
	Type          string
	Phase         string
	Content       string
	ReasoningText string
	Status        string
	Extra         map[string]any
	Raw           map[string]any
}

// ConsumeSSE parses server-sent events from r and invokes onEvent for every
// normalized upstream message.
func ConsumeSSE(r io.Reader, onEvent func(Event) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var block strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if err := ParseSSEBlock(block.String(), onEvent); err != nil {
				return err
			}
			block.Reset()
			continue
		}
		block.WriteString(line)
		block.WriteByte('\n')
	}
	if strings.TrimSpace(block.String()) != "" {
		if err := ParseSSEBlock(block.String(), onEvent); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// ParseSSEBlock decodes one SSE block with data: JSON payloads.
func ParseSSEBlock(block string, onEvent func(Event) error) error {
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(data), &obj); err != nil {
			continue
		}
		for _, event := range ParseQwenEvent(obj) {
			if err := onEvent(event); err != nil {
				return err
			}
		}
	}
	return nil
}

// ParseQwenEvent normalizes the different text fields Qwen uses across models.
func ParseQwenEvent(obj map[string]any) []Event {
	events := []Event{}
	if choices, ok := obj["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			delta, _ := choice["delta"].(map[string]any)
			phase := firstString(delta["phase"])
			if phase == "" {
				phase = "answer"
			}
			content := firstString(delta["content"])
			extra, _ := delta["extra"].(map[string]any)
			reasoning := extractReasoning(delta, extra)
			if reasoning != "" {
				content = reasoning
				if phase == "answer" {
					phase = "thinking_summary"
				}
			}
			events = append(events, Event{
				Type:          "delta",
				Phase:         phase,
				Content:       content,
				ReasoningText: reasoning,
				Status:        firstString(delta["status"]),
				Extra:         extra,
				Raw:           obj,
			})
			return events
		}
	}
	content := firstString(obj["content"], obj["answer"], obj["text"], obj["delta"])
	reasoning := firstString(obj["reasoning_content"], obj["reasoning"], obj["thinking"])
	status := firstString(obj["status"])
	eventType := firstString(obj["event"], obj["type"], status)
	if content != "" || reasoning != "" || eventType != "" {
		phase := eventType
		if phase == "" {
			phase = "answer"
		}
		if reasoning != "" {
			content = reasoning
			if phase == "answer" {
				phase = "thinking_summary"
			}
		}
		events = append(events, Event{Type: firstNonEmpty(eventType, "delta"), Phase: phase, Content: content, ReasoningText: reasoning, Status: status, Raw: obj})
	}
	if data, ok := obj["data"].(map[string]any); ok {
		events = append(events, ParseQwenEvent(data)...)
	}
	if msg, ok := obj["message"].(map[string]any); ok {
		events = append(events, ParseQwenEvent(msg)...)
	}
	return events
}

func extractReasoning(delta map[string]any, extra map[string]any) string {
	if delta == nil {
		return ""
	}
	values := []any{
		delta["reasoning_content"],
		delta["reasoning"],
		delta["reasoning_text"],
		delta["thinking"],
		delta["thoughts"],
	}
	if extra != nil {
		values = append(values, extra["reasoning_content"], extra["reasoning"], extra["reasoning_text"], extra["thinking"], extra["thoughts"])
	}
	return firstString(values...)
}

func firstString(values ...any) string {
	for _, value := range values {
		if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// ConsumeGeminiStream parses the Google Gemini StreamGenerate response stream.
func ConsumeGeminiStream(r io.Reader, isThinking bool, onEvent func(Event) error) error {
	reader := bufio.NewReader(r)
	var accumulatedText string
	eventsCount := 0

	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			trimmed := strings.TrimSpace(line)
			if strings.Contains(trimmed, "wrb.fr") {
				if idx := strings.Index(trimmed, "[["); idx >= 0 {
					jsonPart := trimmed[idx:]
					var rawArr any
					if json.Unmarshal([]byte(jsonPart), &rawArr) == nil {
						if innerStr := FindWRBPayload(rawArr); innerStr != "" {
							var innerObj any
							if json.Unmarshal([]byte(innerStr), &innerObj) == nil {
								newFull := ExtractCandidateText(innerObj)
								if newFull != "" {
									var delta string
									if strings.HasPrefix(newFull, accumulatedText) {
										delta = newFull[len(accumulatedText):]
										accumulatedText = newFull
									} else {
										delta = newFull
										accumulatedText += newFull
									}
									if delta != "" {
										eventsCount++
										phase := "answer"
										if isThinking {
											phase = "thinking"
										}
										evt := Event{
											Type:    "delta",
											Phase:   phase,
											Content: delta,
										}
										if callErr := onEvent(evt); callErr != nil {
											return callErr
										}
									}
								}
							}
						}
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
	}

	if eventsCount == 0 && accumulatedText == "" {
		return errors.New("upstream Gemini returned empty response without text tokens")
	}
	return nil
}

// FindWRBPayload extracts the JSON string from a nested Google RPC array.
func FindWRBPayload(item any) string {
	arr, ok := item.([]any)
	if !ok {
		return ""
	}
	if len(arr) > 2 {
		if tag, ok := arr[0].(string); ok && tag == "wrb.fr" {
			if s, ok := arr[2].(string); ok && s != "" {
				return s
			}
		}
	}
	for _, sub := range arr {
		if s := FindWRBPayload(sub); s != "" {
			return s
		}
	}
	return ""
}

// ExtractCandidateText extracts the output text from Gemini inner response.
func ExtractCandidateText(inner any) string {
	arr, ok := inner.([]any)
	if !ok || len(arr) <= 4 || arr[4] == nil {
		return ""
	}
	cands, ok := arr[4].([]any)
	if !ok || len(cands) == 0 {
		return ""
	}
	cand, ok := cands[0].([]any)
	if !ok || len(cand) <= 1 || cand[1] == nil {
		return ""
	}
	switch v := cand[1].(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, part := range v {
			if s, ok := part.(string); ok {
				sb.WriteString(s)
			} else if list, ok := part.([]any); ok {
				for _, sub := range list {
					if s, ok := sub.(string); ok {
						sb.WriteString(s)
					}
				}
			}
		}
		return sb.String()
	}
	return ""
}
