package services

type ClientProfile struct {
	Name      string
	UserAgent string
	Headers   map[string]string
}

func DefaultClientProfile() ClientProfile {
	return ClientProfile{
		Name:      "gemini-web",
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",
		Headers: map[string]string{
			"Origin":  "https://gemini.google.com",
			"Referer": "https://gemini.google.com/app",
		},
	}
}
