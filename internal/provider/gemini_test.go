package provider

import (
	"context"
	"testing"
	"time"

	"github.com/routerr/aubar/internal/config"
	"github.com/routerr/aubar/internal/domain"
)

type captureCLI struct {
	command string
	stdout  string
	stderr  string
	err     error
}

func (c *captureCLI) Run(_ context.Context, command string, _ time.Duration) (string, string, error) {
	c.command = command
	return c.stdout, c.stderr, c.err
}

func TestGeminiProviderParsesGeminiQuotaModelsWithFallbackChain(t *testing.T) {
	cli := &captureCLI{
		stdout: `{
			"source": "network_cloudcode_api",
			"models": [
				{"model_id":"gemini-3.1-pro","remaining_percent":0},
				{"model_id":"gemini-3-pro-preview","remaining_percent":84.8},
				{"model_id":"gemini-2.5-pro","remaining_percent":70.0},
				{"model_id":"gemini-3.1-flash","remaining_percent":0},
				{"model_id":"gemini-3.1-flash-lite","remaining_percent":0},
				{"model_id":"gemini-3-flash-lite-preview","remaining_percent":83.2},
				{"model_id":"gemini-2.5-flash","remaining_percent":60.0}
			]
		}`,
	}
	p := NewGeminiProvider(config.ProviderSetting{
		Enabled:            true,
		SourceOrder:        []string{"cli"},
		CLICommand:         "./gemini-quota",
		TimeoutSeconds:     2,
		MinIntervalSeconds: 30,
	}, cli)

	snap := p.FetchUsage(context.Background())
	if snap.Status != domain.StatusOK {
		t.Fatalf("expected ok snapshot, got %+v", snap)
	}
	// 3.1-pro is exhausted, 3.1-flash is exhausted, so fallback selects
	// gemini-3-pro-preview (84.8%) as first available in the unified chain.
	if snap.RemainingPercent == nil || *snap.RemainingPercent != 84.8 {
		t.Fatalf("expected 84.8 remaining percent from fallback chain, got %+v", snap)
	}
	if got, ok := snap.Metadata["gemini_model_id"].(string); !ok || got != "gemini-3-pro-preview" {
		t.Fatalf("expected fallback model gemini-3-pro-preview, got %v", snap.Metadata["gemini_model_id"])
	}
	if got, ok := snap.Metadata["gemini_model_tag"].(string); !ok || got != "3p" {
		t.Fatalf("expected model tag 3p, got %v", snap.Metadata["gemini_model_tag"])
	}
}
