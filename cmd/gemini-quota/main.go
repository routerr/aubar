package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/raychang/ai-usage-bar/internal/config"
	"github.com/raychang/ai-usage-bar/internal/envload"
)

// ── Types ────────────────────────────────────────────────────────────────────

type QuotaBucket struct {
	ResetTime         string  `json:"resetTime"`
	TokenType         string  `json:"tokenType"`
	ModelId           string  `json:"modelId"`
	RemainingFraction float64 `json:"remainingFraction"`
}

type QuotaResponse struct {
	Buckets []QuotaBucket `json:"buckets"`
}

type ModelQuota struct {
	ModelId          string  `json:"model_id"`
	ResetTime        string  `json:"reset_time"`
	RemainingPercent float64 `json:"remaining_percent"`
	UsedPercent      float64 `json:"used_percent"`
}

type Output struct {
	GeneratedAt string       `json:"generated_at"`
	Source      string       `json:"source"`
	Models      []ModelQuota `json:"models"`
	Note        string       `json:"note,omitempty"`
}

type oauthCreds struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiryDate   int64  `json:"expiry_date,omitempty"`
}

type oauthRefreshResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	IDToken     string `json:"id_token,omitempty"`
	Scope       string `json:"scope,omitempty"`
	TokenType   string `json:"token_type,omitempty"`
}

// ── Real-Time Network Retrieval via CloudCode-PA ────────────────────────────

var oauthTokenEndpoint = "https://oauth2.googleapis.com/token"

const (
	geminiOAuthCredsPathEnvVar    = "GEMINI_OAUTH_CREDS_PATH"
	geminiOAuthClientIDEnvVar     = "GEMINI_OAUTH_CLIENT_ID"
	geminiOAuthClientSecretEnvVar = "GEMINI_OAUTH_CLIENT_SECRET"
	geminiOAuthSourcePathEnvVar   = "GEMINI_OAUTH_SOURCE_PATH"
)

type oauthClientConfig struct {
	ClientID     string
	ClientSecret string
}

var oauthClientIDPattern = regexp.MustCompile(`const OAUTH_CLIENT_ID = '([^']+)'`)
var oauthClientSecretPattern = regexp.MustCompile(`const OAUTH_CLIENT_SECRET = '([^']+)'`)

type httpStatusError struct {
	StatusCode int
}

func (e httpStatusError) Error() string {
	return fmt.Sprintf("http %d", e.StatusCode)
}

func fetchCloudCodeQuota(accessToken string) (*QuotaResponse, error) {
	projectID := strings.TrimSpace(os.Getenv("GEMINI_QUOTA_PROJECT_ID"))
	if projectID == "" {
		projectID = "totemic-carrier-5jts2" // Fallback to the public dummy project
	}

	reqBody, _ := json.Marshal(map[string]string{"project": projectID})

	req, err := http.NewRequest("POST", "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	// Fake some CLI headers
	req.Header.Set("User-Agent", "GeminiCLI/0.32.1/gemini-3-pro-preview (darwin; arm64) google-api-nodejs-client/10.6.1")
	req.Header.Set("X-Goog-Api-Client", "gl-node/25.6.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, httpStatusError{StatusCode: resp.StatusCode}
	}

	var quota QuotaResponse
	// Limit read to 1MB to prevent memory exhaustion (GO-HTTPCLIENT-001)
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&quota); err != nil {
		return nil, err
	}

	return &quota, nil
}

// ── Utility ──────────────────────────────────────────────────────────────────

func maskToken(token string) string {
	if len(token) <= 12 {
		return "***"
	}
	return token[:6] + "..." + token[len(token)-6:]
}

func defaultOAuthCredsPath() string {
	if override := strings.TrimSpace(os.Getenv(geminiOAuthCredsPathEnvVar)); override != "" {
		return override
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "oauth_creds.json")
}

func loadOAuthCreds(path string) (oauthCreds, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return oauthCreds{}, err
	}
	var creds oauthCreds
	if err := json.Unmarshal(b, &creds); err != nil {
		return oauthCreds{}, err
	}
	return creds, nil
}

func saveOAuthCreds(path string, creds oauthCreds) error {
	b, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func tokenExpired(creds oauthCreds, now time.Time) bool {
	if strings.TrimSpace(creds.AccessToken) == "" {
		return true
	}
	if creds.ExpiryDate <= 0 {
		return false
	}
	return now.Add(time.Minute).UnixMilli() >= creds.ExpiryDate
}

func refreshAccessToken(creds oauthCreds, now time.Time) (oauthCreds, error) {
	if strings.TrimSpace(creds.RefreshToken) == "" {
		return creds, fmt.Errorf("refresh_token missing")
	}
	clientCfg, err := loadOAuthClientConfig()
	if err != nil {
		return creds, err
	}

	form := url.Values{}
	form.Set("client_id", clientCfg.ClientID)
	form.Set("client_secret", clientCfg.ClientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", creds.RefreshToken)

	req, err := http.NewRequest(http.MethodPost, oauthTokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return creds, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return creds, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return creds, httpStatusError{StatusCode: resp.StatusCode}
	}

	var refreshed oauthRefreshResponse
	// Limit read to 64KB - OAuth refresh responses should be small (GO-HTTPCLIENT-001)
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&refreshed); err != nil {
		return creds, err
	}
	if strings.TrimSpace(refreshed.AccessToken) == "" {
		return creds, fmt.Errorf("refresh response missing access_token")
	}

	creds.AccessToken = refreshed.AccessToken
	if refreshed.ExpiresIn > 0 {
		creds.ExpiryDate = now.Add(time.Duration(refreshed.ExpiresIn) * time.Second).UnixMilli()
	}
	if strings.TrimSpace(refreshed.Scope) != "" {
		creds.Scope = refreshed.Scope
	}
	if strings.TrimSpace(refreshed.TokenType) != "" {
		creds.TokenType = refreshed.TokenType
	}
	if strings.TrimSpace(refreshed.IDToken) != "" {
		creds.IDToken = refreshed.IDToken
	}
	return creds, nil
}

func loadOAuthClientConfig() (oauthClientConfig, error) {
	clientID := strings.TrimSpace(os.Getenv(geminiOAuthClientIDEnvVar))
	clientSecret := strings.TrimSpace(os.Getenv(geminiOAuthClientSecretEnvVar))
	if clientID != "" && clientSecret != "" {
		return oauthClientConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
		}, nil
	}
	if discovered, err := discoverOAuthClientConfig(); err == nil {
		return discovered, nil
	}
	return oauthClientConfig{}, fmt.Errorf("%s and %s must be set, or a local Gemini CLI OAuth source must be discoverable", geminiOAuthClientIDEnvVar, geminiOAuthClientSecretEnvVar)
}

func discoverOAuthClientConfig() (oauthClientConfig, error) {
	for _, candidate := range geminiOAuthSourceCandidates() {
		cfg, err := loadOAuthClientConfigFromFile(candidate)
		if err == nil {
			return cfg, nil
		}
	}
	return oauthClientConfig{}, fmt.Errorf("no local Gemini CLI OAuth source found")
}

func geminiOAuthSourceCandidates() []string {
	seen := map[string]struct{}{}
	candidates := []string{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		candidates = append(candidates, path)
	}

	if override := strings.TrimSpace(os.Getenv(geminiOAuthSourcePathEnvVar)); override != "" {
		add(override)
		return candidates
	}

	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, "node_modules", "@google", "gemini-cli-core", "dist", "src", "code_assist", "oauth2.js"))
		if matches, err := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "lib", "node_modules", "@google", "gemini-cli-core", "dist", "src", "code_assist", "oauth2.js")); err == nil {
			for _, match := range matches {
				add(match)
			}
		}
	}

	if geminiPath, err := exec.LookPath("gemini"); err == nil {
		if resolved, err := filepath.EvalSymlinks(geminiPath); err == nil {
			for _, path := range geminiOAuthSourceCandidatesFromGeminiBinary(resolved) {
				add(path)
			}
		}
	}

	return candidates
}

func geminiOAuthSourceCandidatesFromGeminiBinary(geminiPath string) []string {
	root := filepath.Clean(geminiPath)
	candidates := []string{}
	for i := 0; i < 6; i++ {
		candidates = append(candidates,
			filepath.Join(root, "..", "@google", "gemini-cli-core", "dist", "src", "code_assist", "oauth2.js"),
			filepath.Join(root, "..", "gemini-cli-core", "dist", "src", "code_assist", "oauth2.js"),
			filepath.Join(root, "node_modules", "@google", "gemini-cli-core", "dist", "src", "code_assist", "oauth2.js"),
		)
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	return candidates
}

func loadOAuthClientConfigFromFile(path string) (oauthClientConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return oauthClientConfig{}, err
	}
	content := string(raw)
	clientIDMatch := oauthClientIDPattern.FindStringSubmatch(content)
	clientSecretMatch := oauthClientSecretPattern.FindStringSubmatch(content)
	if len(clientIDMatch) < 2 || len(clientSecretMatch) < 2 {
		return oauthClientConfig{}, fmt.Errorf("oauth constants not found in %s", path)
	}
	return oauthClientConfig{
		ClientID:     strings.TrimSpace(clientIDMatch[1]),
		ClientSecret: strings.TrimSpace(clientSecretMatch[1]),
	}, nil
}

func resolveAccessToken(overrideToken, credsPath string, now time.Time) (string, oauthCreds, error) {
	overrideToken = strings.TrimSpace(overrideToken)
	if overrideToken != "" {
		return overrideToken, oauthCreds{AccessToken: overrideToken}, nil
	}

	creds, err := loadOAuthCreds(credsPath)
	if err != nil {
		return "", oauthCreds{}, err
	}
	if tokenExpired(creds, now) && strings.TrimSpace(creds.RefreshToken) != "" {
		refreshed, err := refreshAccessToken(creds, now)
		if err != nil {
			return "", creds, err
		}
		creds = refreshed
		if err := saveOAuthCreds(credsPath, creds); err != nil {
			return "", creds, err
		}
	}
	return strings.TrimSpace(creds.AccessToken), creds, nil
}

func main() {
	var (
		flagToken = flag.String("token", "", "OAuth Access Token (overrides ~/.gemini/oauth_creds.json)")
	)
	flag.Parse()

	// 0. Load .env from the repo cwd and the app config directory.
	_ = envload.Load(envload.DefaultEnvPaths(config.DefaultConfigDir())...)

	// 1. Resolve Access Token
	credsPath := defaultOAuthCredsPath()
	token, creds, err := resolveAccessToken(*flagToken, credsPath, time.Now())

	out := Output{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Models:      []ModelQuota{},
	}

	if err != nil || token == "" {
		out.Source = "none"
		if err != nil {
			out.Note = fmt.Sprintf("Failed to resolve OAuth token: %v", err)
		} else {
			out.Note = "No OAuth token found. Ensure ~/.gemini/oauth_creds.json exists or pass -token."
		}
		printJSON(out)
		return
	}

	// 2. Fetch Real-Time Quota
	quota, err := fetchCloudCodeQuota(token)
	if statusErr, ok := err.(httpStatusError); ok && statusErr.StatusCode == http.StatusUnauthorized && strings.TrimSpace(*flagToken) == "" && strings.TrimSpace(creds.RefreshToken) != "" {
		refreshed, refreshErr := refreshAccessToken(creds, time.Now())
		if refreshErr == nil {
			creds = refreshed
			_ = saveOAuthCreds(credsPath, creds)
			token = creds.AccessToken
			quota, err = fetchCloudCodeQuota(token)
		}
	}
	if err != nil {
		out.Source = "error"
		out.Note = fmt.Sprintf("Failed to fetch quota from cloudcode-pa: %v", err)
		printJSON(out)
		return
	}

	out.Source = "network_cloudcode_api"

	// 3. Map Data
	for _, b := range quota.Buckets {
		if strings.HasSuffix(b.ModelId, "_vertex") {
			continue // Filter out duplicate vertex entries for cleaner output
		}

		remPct := b.RemainingFraction * 100
		usedPct := 100.0 - remPct

		out.Models = append(out.Models, ModelQuota{
			ModelId:          b.ModelId,
			ResetTime:        b.ResetTime,
			RemainingPercent: remPct,
			UsedPercent:      usedPct,
		})
	}

	printJSON(out)
}

func printJSON(out Output) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding output: %v\n", err)
		os.Exit(1)
	}
}
