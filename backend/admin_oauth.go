package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultCodeAssistClientID = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	defaultCodeAssistRedirect = "http://127.0.0.1:8999/oauth2callback"
	defaultCodeAssistScopes   = "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile"

	defaultAntigravityClientID = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	defaultAntigravityRedirect = "http://localhost:51121/oauth-callback"
	defaultAntigravityScopes   = "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile https://www.googleapis.com/auth/cclog https://www.googleapis.com/auth/experimentsandconfigs"

	googleOAuthAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleOAuthTokenURL = "https://oauth2.googleapis.com/token"
)

type oauthStateEntry struct {
	Verifier  string    `json:"verifier"`
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"created_at"`
}

var (
	oauthStatesMu sync.Mutex
	oauthStates   = make(map[string]oauthStateEntry)
)

func (app *App) adminOAuthURL(w http.ResponseWriter, r *http.Request) {
	if _, ok := app.verifyAdmin(w, r); !ok {
		return
	}
	provider := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	if provider == "" || provider == "code_assist" {
		provider = "google"
	}

	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to generate random verifier")
		return
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to generate random state")
		return
	}
	state := hex.EncodeToString(stateBytes)

	oauthStatesMu.Lock()
	// Bersihkan state kadaluarsa > 1 jam
	for s, entry := range oauthStates {
		if time.Since(entry.CreatedAt) > time.Hour {
			delete(oauthStates, s)
		}
	}
	oauthStates[state] = oauthStateEntry{
		Verifier:  verifier,
		Provider:  provider,
		CreatedAt: time.Now(),
	}
	oauthStatesMu.Unlock()

	// Simpan juga state ke data/google_pkce.json untuk kompatibilitas CLI
	pkcePath := filepath.Join(app.settings.DataDir, "google_pkce.json")
	_ = os.WriteFile(pkcePath, []byte(fmt.Sprintf(`{"verifier":%q,"state":%q,"ts":%d}`, verifier, state, time.Now().Unix())), 0o600)

	var authURL string
	switch provider {
	case "antigravity":
		cid := strings.TrimSpace(os.Getenv("ANTIGRAVITY_CLIENT_ID"))
		if cid == "" {
			cid = defaultAntigravityClientID
		}
		q := url.Values{
			"client_id":     {cid},
			"redirect_uri":  {defaultAntigravityRedirect},
			"response_type": {"code"},
			"scope":         {defaultAntigravityScopes},
			"access_type":   {"offline"},
			"prompt":        {"consent"},
			"state":         {state},
		}
		authURL = googleOAuthAuthURL + "?" + q.Encode()

	default: // google / code_assist
		cid := strings.TrimSpace(os.Getenv("CODE_ASSIST_CLIENT_ID"))
		if cid == "" {
			cid = defaultCodeAssistClientID
		}
		q := url.Values{
			"client_id":             {cid},
			"redirect_uri":          {defaultCodeAssistRedirect},
			"response_type":         {"code"},
			"scope":                 {defaultCodeAssistScopes},
			"access_type":           {"offline"},
			"prompt":                {"consent"},
			"state":                 {state},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
		}
		authURL = googleOAuthAuthURL + "?" + q.Encode()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"provider": provider,
		"url":      authURL,
		"state":    state,
	})
}

func (app *App) adminOAuthExchange(w http.ResponseWriter, r *http.Request) {
	if _, ok := app.verifyAdmin(w, r); !ok {
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Code     string `json:"code"`
		URL      string `json:"url"`
		State    string `json:"state"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" || provider == "code_assist" {
		provider = "google"
	}

	rawCode := strings.TrimSpace(req.Code)
	if rawCode == "" {
		rawCode = strings.TrimSpace(req.URL)
	}
	if rawCode == "" {
		writeError(w, http.StatusBadRequest, "Parameter 'code' atau 'url' wajib diisi")
		return
	}

	// Parsing URL redirect jika pengguna menempel full URL
	code := rawCode
	state := strings.TrimSpace(req.State)
	if strings.Contains(rawCode, "?") || strings.Contains(rawCode, "&") || strings.HasPrefix(rawCode, "http") {
		if parsed, err := url.Parse(rawCode); err == nil {
			if c := parsed.Query().Get("code"); c != "" {
				code = c
			}
			if s := parsed.Query().Get("state"); s != "" && state == "" {
				state = s
			}
		}
	}

	oauthStatesMu.Lock()
	stateEntry, hasState := oauthStates[state]
	oauthStatesMu.Unlock()

	verifier := ""
	if hasState {
		verifier = stateEntry.Verifier
	} else {
		// Fallback membaca google_pkce.json
		pkcePath := filepath.Join(app.settings.DataDir, "google_pkce.json")
		if raw, err := os.ReadFile(pkcePath); err == nil {
			var pkceData struct {
				Verifier string `json:"verifier"`
			}
			if json.Unmarshal(raw, &pkceData) == nil {
				verifier = pkceData.Verifier
			}
		}
	}

	var clientID, clientSecret, redirectURI, targetFileName, authType string
	if provider == "antigravity" {
		clientID = strings.TrimSpace(os.Getenv("ANTIGRAVITY_CLIENT_ID"))
		if clientID == "" {
			clientID = defaultAntigravityClientID
		}
		clientSecret = strings.TrimSpace(os.Getenv("ANTIGRAVITY_CLIENT_SECRET"))
		if clientSecret == "" {
			writeError(w, http.StatusBadRequest, "ANTIGRAVITY_CLIENT_SECRET belum dikonfigurasi di environment atau file .env")
			return
		}
		redirectURI = defaultAntigravityRedirect
		targetFileName = "antigravity_oauth.json"
		authType = authTypeAntigravity
	} else {
		clientID = strings.TrimSpace(os.Getenv("CODE_ASSIST_CLIENT_ID"))
		if clientID == "" {
			clientID = defaultCodeAssistClientID
		}
		clientSecret = strings.TrimSpace(os.Getenv("CODE_ASSIST_CLIENT_SECRET"))
		if clientSecret == "" {
			writeError(w, http.StatusBadRequest, "CODE_ASSIST_CLIENT_SECRET belum dikonfigurasi di environment atau file .env")
			return
		}
		redirectURI = defaultCodeAssistRedirect
		targetFileName = "google_oauth.json"
		authType = authTypeCodeAssist
	}

	postData := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {redirectURI},
	}
	if verifier != "" && provider != "antigravity" {
		postData.Set("code_verifier", verifier)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, googleOAuthTokenURL, strings.NewReader(postData.Encode()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("Gagal menghubungi Google OAuth: %v", err))
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var errObj map[string]any
		_ = json.Unmarshal(respBody, &errObj)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":          false,
			"status_code": resp.StatusCode,
			"error":       fmt.Sprintf("Google menolak kode otorisasi (HTTP %d)", resp.StatusCode),
			"detail":      string(respBody),
		})
		return
	}

	var tokenResp map[string]any
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		writeError(w, http.StatusInternalServerError, "Gagal mengurai respons token dari Google")
		return
	}

	refreshToken, _ := tokenResp["refresh_token"].(string)
	if refreshToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":     false,
			"error":  "Respons Google tidak memiliki refresh_token. Pastikan klik 'Izinkan' dan coba lagi.",
			"detail": string(respBody),
		})
		return
	}

	tokenResp["obtained_at"] = time.Now().Unix()
	prettyJSON, err := json.MarshalIndent(tokenResp, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	targetPath := filepath.Join(app.settings.DataDir, targetFileName)
	tmpPath := targetPath + ".tmp"
	if err := os.WriteFile(tmpPath, prettyJSON, 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Gagal menyimpan berkas token: %v", err))
		return
	}
	_ = os.Rename(tmpPath, targetPath)
	_ = os.Chmod(targetPath, 0o600)

	// Dapatkan email akun dari ID Token
	idToken, _ := tokenResp["id_token"].(string)
	email := emailFromJWTPayload(idToken)
	if email == "" {
		email = fmt.Sprintf("oauth_%s@google", provider)
	}

	// Daftarkan akun ke pool & data/accounts.json
	accHandle := "oauth:" + email
	newAcc := Account{
		Email:      email,
		Password:   "",
		Token:      accHandle,
		Cookies:    "",
		Username:   email,
		Source:     "oauth_" + provider,
		AuthType:   authType,
		OAuthFile:  targetFileName,
		StatusCode: "valid",
		Valid:      true,
	}

	// Simpan ke database accounts jika belum ada atau update
	_ = app.accounts.Add(newAcc)
	_ = app.accounts.Load()

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"provider":   provider,
		"email":      email,
		"saved_file": targetFileName,
		"account":    newAcc,
	})
}

func (app *App) adminOAuthStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := app.verifyAdmin(w, r); !ok {
		return
	}

	checkProvider := func(fileName string) map[string]any {
		path := filepath.Join(app.settings.DataDir, fileName)
		fi, err := os.Stat(path)
		if err != nil {
			return map[string]any{
				"configured": false,
				"file":       fileName,
			}
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return map[string]any{
				"configured": true,
				"file":       fileName,
				"error":      "Gagal membaca file",
			}
		}
		var data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			IDToken      string `json:"id_token"`
			ObtainedAt   int64  `json:"obtained_at"`
			ExpiresIn    int64  `json:"expires_in"`
		}
		_ = json.Unmarshal(raw, &data)
		email := emailFromJWTPayload(data.IDToken)

		return map[string]any{
			"configured":        data.RefreshToken != "",
			"file":              fileName,
			"email":             email,
			"obtained_at":       data.ObtainedAt,
			"has_refresh_token": data.RefreshToken != "",
			"has_access_token":  data.AccessToken != "",
			"size_bytes":        fi.Size(),
			"modified_at":       fi.ModTime().Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"providers": map[string]any{
			"google":      checkProvider("google_oauth.json"),
			"antigravity": checkProvider("antigravity_oauth.json"),
		},
	})
}
