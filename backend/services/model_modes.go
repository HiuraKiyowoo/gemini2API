package services

import "strings"

type ModelMode struct {
	RequestedModel string
	BaseModel      string
	ChatType       string
	ForceThinking  bool
	Mode           string
}

func ParseModelMode(modelID, defaultModel string) ModelMode {
	requested := strings.TrimSpace(modelID)
	if requested == "" {
		requested = defaultModel
	}
	lowered := strings.ToLower(requested)
	suffixes := []struct {
		suffix        string
		chatType      string
		forceThinking bool
		mode          string
	}{
		{"-deep-research", "deep_research", false, "deep_research"},
		{"-deep_research", "deep_research", false, "deep_research"},
		{"-web-dev", "web_dev", false, "webdev"},
		{"-thinking", "t2t", true, "thinking"},
		{"-search", "t2t", false, "search"},
		{"-webdev", "web_dev", false, "webdev"},
		{"-image", "t2i", false, "image"},
		{"-video", "t2v", false, "video"},
		{"-slides", "slides", false, "slides"},
		{"-t2i", "t2i", false, "image"},
		{"-t2v", "t2v", false, "video"},
	}
	for _, s := range suffixes {
		if strings.HasSuffix(lowered, s.suffix) {
			return ModelMode{
				RequestedModel: requested,
				BaseModel:      strings.TrimSpace(requested[:len(requested)-len(s.suffix)]),
				ChatType:       s.chatType,
				ForceThinking:  s.forceThinking,
				Mode:           s.mode,
			}
		}
	}
	return ModelMode{RequestedModel: requested, BaseModel: requested, ChatType: "t2t", Mode: "chat"}
}

func modelModeSuffixes() []string {
	return []string{"-deep-research", "-deep_research", "-web-dev", "-thinking", "-search", "-webdev", "-image", "-video", "-slides", "-t2i", "-t2v"}
}
