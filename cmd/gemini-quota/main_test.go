package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "access_token": "fixture-fresh-credential",
  "expires_in": 3600,
  "token_type": "Bearer"
}`))
	}))
	defer server.Close()

	prevEndpoint := oauthTokenEndpoint
	oauthTokenEndpoint = server.URL
	defer func() { oauthTokenEndpoint = prevEndpoint }()

	token, creds, err := resolveAccessToken("", credsPath, time.Unix(2000, 0))
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
