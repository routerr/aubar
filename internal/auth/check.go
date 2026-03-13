package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Help struct {
	Provider     string
	GetKeyURL    string
	DocsURL      string
	Instructions []string
}

type Result struct {
	Provider string
	OK       bool
	Warning  string
	Message  string
	Help     Help
}

type Checker struct {
	Client        *http.Client
	OpenAIBaseURL string
	ClaudeBaseURL string
	GeminiBaseURL string
}

func DefaultChecker() *Checker {
	return &Checker{
		Client:        &http.Client{Timeout: 10 * time.Second},
		OpenAIBaseURL: "https://api.openai.com",
		ClaudeBaseURL: "https://api.anthropic.com",
		GeminiBaseURL: "https://generativelanguage.googleapis.com",
	}
}

func ValidateCredential(ctx context.Context, provider, key string) Result {
	return DefaultChecker().Validate(ctx, provider, key)
}

func (c *Checker) Validate(ctx context.Context, provider, key string) Result {
	provider = strings.ToLower(strings.TrimSpace(provider))
	key = strings.TrimSpace(key)
	help := ProviderHelp(provider)
	if key == "" {
		return Result{
			Provider: provider,
			Message:  "credential is empty",
			Help:     help,
		}
	}

	switch provider {
	case "openai":
		return c.validateOpenAI(ctx, key, help)
	case "claude":
		return c.validateClaude(ctx, key, help)
	case "gemini":
		return c.validateGemini(ctx, key, help)
	default:
		return Result{
			Provider: provider,
			Message:  "unknown provider",
			Help:     help,
		}
	}
}

func ProviderHelp(provider string) Help {
	switch provider {
	case "openai":
		return Help{
			Provider:  provider,
			GetKeyURL: "https://platform.openai.com/api-keys",
			DocsURL:   "https://help.openai.com/en/articles/9687866",
			Instructions: []string{
				"Create a Secret API key on the OpenAI API key page.",
				"For Aubar usage and cost retrieval, create an Admin API key in the OpenAI dashboard's Admin keys section as an organization owner.",
			},
		}
	case "claude":
		return Help{
			Provider:  provider,
			GetKeyURL: "https://console.anthropic.com/settings/keys",
			DocsURL:   "https://docs.anthropic.com/en/api/data-usage-cost-api",
			Instructions: []string{
				"Join or create an Anthropic organization in Console -> Settings -> Organization.",
				"Create an Admin API key as an organization admin; the Usage & Cost Admin API requires a key starting with sk-ant-admin.",
			},
		}
	case "gemini":
		return Help{
			Provider:  provider,
			GetKeyURL: "https://aistudio.google.com/app/apikey",
			DocsURL:   "https://ai.google.dev/tutorials/setup",
			Instructions: []string{
				"Open Google AI Studio and create or view a Gemini API key from the API Keys page.",
				"If no project is available, import or create one in Google AI Studio first.",
			},
		}
	default:
		return Help{Provider: provider}
	}
}

func (c *Checker) validateOpenAI(ctx context.Context, key string, help Help) Result {
	if !strings.HasPrefix(key, "sk-") {
		return Result{
			Provider: "openai",
			Message:  "OpenAI keys should start with sk-",
			Help:     help,
		}
	}

	u, _ := url.Parse(strings.TrimRight(c.OpenAIBaseURL, "/") + "/v1/organization/costs")
	q := u.Query()
	q.Set("start_time", strconv.FormatInt(time.Now().Add(-7*24*time.Hour).Unix(), 10))
	q.Set("bucket_width", "1d")
	q.Set("limit", "1")
	u.RawQuery = q.Encode()

	status, body, err := c.do(ctx, http.MethodGet, u.String(), map[string]string{
		"Authorization": "Bearer " + key,
	})
	if err != nil {
		return Result{Provider: "openai", Message: "failed to validate key against OpenAI usage endpoint: " + err.Error(), Help: help}
	}
	switch status {
	case http.StatusOK:
		return Result{Provider: "openai", OK: true, Help: help}
	case http.StatusUnauthorized:
		return Result{Provider: "openai", Message: "OpenAI rejected the credential for the organization usage endpoint", Help: help}
	case http.StatusForbidden, http.StatusNotFound:
		return Result{
			Provider: "openai",
			Message:  "Aubar needs an OpenAI Admin API key with organization owner access for usage/cost data",
			Help:     help,
		}
	default:
		return Result{Provider: "openai", Message: fmt.Sprintf("OpenAI usage endpoint returned %d: %s", status, body), Help: help}
	}
}

func (c *Checker) validateClaude(ctx context.Context, key string, help Help) Result {
	if !strings.HasPrefix(key, "sk-ant-admin") {
		return Result{
			Provider: "claude",
			Message:  "Claude usage/cost retrieval requires an Admin API key starting with sk-ant-admin",
			Help:     help,
		}
	}

	u, _ := url.Parse(strings.TrimRight(c.ClaudeBaseURL, "/") + "/v1/organizations/usage_report/messages")
	q := u.Query()
	q.Set("starting_at", time.Now().Add(-24*time.Hour).Format(time.RFC3339))
	q.Set("ending_at", time.Now().Format(time.RFC3339))
	u.RawQuery = q.Encode()

	status, body, err := c.do(ctx, http.MethodGet, u.String(), map[string]string{
		"x-api-key":         key,
		"anthropic-version": "2023-06-01",
	})
	if err != nil {
		return Result{Provider: "claude", Message: "failed to validate key against Anthropic Usage & Cost API: " + err.Error(), Help: help}
	}
	switch status {
	case http.StatusOK:
		return Result{Provider: "claude", OK: true, Help: help}
	case http.StatusUnauthorized:
		return Result{Provider: "claude", Message: "Anthropic rejected the Admin API key", Help: help}
	case http.StatusForbidden, http.StatusNotFound:
		return Result{
			Provider: "claude",
			Message:  "Anthropic Usage & Cost API requires an organization admin and the Admin API",
			Help:     help,
		}
	default:
		return Result{Provider: "claude", Message: fmt.Sprintf("Anthropic usage endpoint returned %d: %s", status, body), Help: help}
	}
}

func (c *Checker) validateGemini(ctx context.Context, key string, help Help) Result {
	if !strings.HasPrefix(key, "AIza") {
		return Result{
			Provider: "gemini",
			Message:  "Gemini API keys from Google AI Studio usually start with AIza",
			Help:     help,
		}
	}

	u, _ := url.Parse(strings.TrimRight(c.GeminiBaseURL, "/") + "/v1beta/models")
	q := u.Query()
	q.Set("key", key)
	u.RawQuery = q.Encode()

	status, body, err := c.do(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{Provider: "gemini", Message: "failed to validate key against the Gemini models endpoint: " + err.Error(), Help: help}
	}
	switch status {
	case http.StatusOK:
		return Result{
			Provider: "gemini",
			OK:       true,
			Warning:  "Gemini key works, but the API does not expose a native remaining quota value here; Aubar will show N/A for remaining quota.",
			Help:     help,
		}
	case http.StatusUnauthorized, http.StatusForbidden:
		return Result{Provider: "gemini", Message: "Gemini rejected the API key", Help: help}
	default:
		return Result{Provider: "gemini", Message: fmt.Sprintf("Gemini models endpoint returned %d: %s", status, body), Help: help}
	}
}

func (c *Checker) do(ctx context.Context, method, rawURL string, headers map[string]string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return 0, "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return resp.StatusCode, strings.TrimSpace(string(body)), nil
}
