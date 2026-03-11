package provider

import (
	"context"
	"testing"
	"time"

	"github.com/raychang/ai-usage-bar/internal/config"
	"github.com/raychang/ai-usage-bar/internal/domain"
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

func TestGeminiProviderFetchCLIUsesLocalQuotaHelperAndParsesQuota(t *testing.T) {
	cli := &captureCLI{
		stdout: `{
			"source": "network_cloudcode_api",
			"models": [
				{"model_id":"gemini-3.1-pro","remaining_percent":68},
				{"model_id":"gemini-3.1-flash","remaining_percent":81}
			]
		}`,
	}
	p := NewGeminiProvider(config.ProviderSetting{
		Enabled:            true,
		SourceOrder:        []string{"cli"},
		CLICommand:         "gemini usage --json",
		TimeoutSeconds:     2,
		MinIntervalSeconds: 30,
	}, cli)

	snap := p.FetchUsage(context.Background())
	if cli.command != "./gemini-quota" {
		t.Fatalf("expected local quota helper command, got %q", cli.command)
	}
	if snap.Status != domain.StatusOK {
		t.Fatalf("expected ok snapshot, got %+v", snap)
	}
	if snap.Source != "cli" {
		t.Fatalf("expected cli source, got %+v", snap)
	}
	if snap.RemainingPercent == nil || *snap.RemainingPercent != 68 {
		t.Fatalf("expected remaining percent, got %+v", snap)
	}
	if got, ok := snap.Metadata["gemini_left_major_version_tag"].(string); !ok || got != "3" {
		t.Fatalf("expected left major version tag, got %+v", snap.Metadata)
	}
	if got, ok := snap.Metadata["gemini_right_major_version_tag"].(string); !ok || got != "3" {
		t.Fatalf("expected right major version tag, got %+v", snap.Metadata)
	}
}

func TestGeminiProviderParsesGeminiQuotaModelsWithFallbackChains(t *testing.T) {
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
	if snap.RemainingPercent == nil || *snap.RemainingPercent != 83.2 {
		t.Fatalf("expected summary remaining percent from fallback chains, got %+v", snap)
	}
	if got, ok := snap.Metadata["gemini_left_model_id"].(string); !ok || got != "gemini-3-pro-preview" {
		t.Fatalf("expected left fallback model, got %+v", snap.Metadata)
	}
	if got, ok := snap.Metadata["gemini_right_model_id"].(string); !ok || got != "gemini-3-flash-lite-preview" {
		t.Fatalf("expected right fallback model, got %+v", snap.Metadata)
	}
	if got, ok := snap.Metadata["gemini_left_major_version_tag"].(string); !ok || got != "3" {
		t.Fatalf("expected left major version tag, got %+v", snap.Metadata)
	}
	if got, ok := snap.Metadata["gemini_right_major_version_tag"].(string); !ok || got != "3" {
		t.Fatalf("expected right major version tag, got %+v", snap.Metadata)
	}
}
