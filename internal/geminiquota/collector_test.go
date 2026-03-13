package geminiquota

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestResolveAccessTokenRefreshesExpiredCreds(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "oauth_creds.json")
	t.Setenv(geminiOAuthClientIDEnvVar, "test-client-id")
	t.Setenv(geminiOAuthClientSecretEnvVar, "test-client-secret")
	if err := os.WriteFile(credsPath, []byte(`{
  "access_token": "fixture-expired-credential",
  "refresh_token": "fixture-refresh-credential",
  "expiry_date": 1000
}`), 0o600); err != nil {
		t.Fatalf("write creds: %v", err)
	}

	var receivedBody string
	client := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
  "access_token": "fixture-fresh-credential",
  "expires_in": 3600,
  "token_type": "Bearer"
}`)),
		}, nil
	})

	token, creds, err := resolveAccessToken(client, "", credsPath, time.Unix(2000, 0))
	if err != nil {
		t.Fatalf("resolve token: %v", err)
	}
	if token != "fixture-fresh-credential" {
		t.Fatalf("expected refreshed token, got %q", token)
	}
	if creds.AccessToken != "fixture-fresh-credential" {
		t.Fatalf("expected creds updated, got %+v", creds)
	}
	if !strings.Contains(receivedBody, "refresh_token=fixture-refresh-credential") {
		t.Fatalf("expected refresh token request body, got %q", receivedBody)
	}

	raw, err := os.ReadFile(credsPath)
	if err != nil {
		t.Fatalf("read updated creds: %v", err)
	}
	if !strings.Contains(string(raw), "fixture-fresh-credential") {
		t.Fatalf("expected refreshed creds to persist, got %s", raw)
	}
}

func TestDefaultOAuthCredsPathUsesEnvironmentOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom-oauth-creds.json")
	t.Setenv(geminiOAuthCredsPathEnvVar, override)

	if got := defaultOAuthCredsPath(); got != override {
		t.Fatalf("expected override path %q, got %q", override, got)
	}
}

func TestLoadOAuthClientConfigFromEnvironment(t *testing.T) {
	t.Setenv(geminiOAuthClientIDEnvVar, "client-id")
	t.Setenv(geminiOAuthClientSecretEnvVar, "client-secret")

	cfg, err := loadOAuthClientConfig()
	if err != nil {
		t.Fatalf("load client config: %v", err)
	}
	if cfg.ClientID != "client-id" || cfg.ClientSecret != "client-secret" {
		t.Fatalf("unexpected client config: %+v", cfg)
	}
}

func TestLoadOAuthClientConfigDiscoversLocalGeminiCLIConstants(t *testing.T) {
	t.Setenv(geminiOAuthClientIDEnvVar, "")
	t.Setenv(geminiOAuthClientSecretEnvVar, "")

	sourcePath := filepath.Join(t.TempDir(), "oauth2.js")
	if err := os.WriteFile(sourcePath, []byte(`
const OAUTH_CLIENT_ID = 'fixture-installed-client-id.apps.googleusercontent.com';
const OAUTH_CLIENT_SECRET = 'fixture-installed-client-secret';
`), 0o600); err != nil {
		t.Fatalf("write oauth source: %v", err)
	}
	t.Setenv(geminiOAuthSourcePathEnvVar, sourcePath)

	cfg, err := loadOAuthClientConfig()
	if err != nil {
		t.Fatalf("discover client config: %v", err)
	}
	if cfg.ClientID != "fixture-installed-client-id.apps.googleusercontent.com" {
		t.Fatalf("unexpected discovered client id: %+v", cfg)
	}
	if cfg.ClientSecret != "fixture-installed-client-secret" {
		t.Fatalf("unexpected discovered client secret: %+v", cfg)
	}
}
