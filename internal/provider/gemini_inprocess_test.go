package provider

import (
	"context"
	"testing"

	"github.com/routerr/aubar/internal/config"
	"github.com/routerr/aubar/internal/domain"
	"github.com/routerr/aubar/internal/geminiquota"
)

func TestGeminiProviderFetchUsageUsesInProcessCollectorByDefault(t *testing.T) {
	cli := &captureCLI{}
	p := NewGeminiProvider(config.ProviderSetting{
		Enabled:            true,
		SourceOrder:        []string{"cli"},
		CLICommand:         "",
		TimeoutSeconds:     2,
		MinIntervalSeconds: 30,
	}, cli)
	p.collectOutput = func(_ context.Context) (geminiquota.Output, error) {
		return geminiquota.Output{
			Source: "network_cloudcode_api",
			Models: []geminiquota.ModelQuota{
				{ModelID: "gemini-3.1-pro", RemainingPercent: 68},
				{ModelID: "gemini-3.1-flash", RemainingPercent: 81},
			},
		}, nil
	}

	snap := p.FetchUsage(context.Background())
	if cli.command != "" {
		t.Fatalf("expected no CLI command, got %q", cli.command)
	}
	if snap.Status != domain.StatusOK {
		t.Fatalf("expected ok snapshot, got %+v", snap)
	}
	if snap.Source != "cli" {
		t.Fatalf("expected cli source, got %+v", snap)
	}
	if snap.RemainingPercent == nil || *snap.RemainingPercent != 68 {
		t.Fatalf("expected 68 remaining percent, got %+v", snap)
	}
}

func TestGeminiProviderFetchUsageUsesExplicitCLIOverride(t *testing.T) {
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
		CLICommand:         "custom-gemini-helper --json",
		TimeoutSeconds:     2,
		MinIntervalSeconds: 30,
	}, cli)
	p.collectOutput = func(_ context.Context) (geminiquota.Output, error) {
		t.Fatal("expected explicit CLI override to bypass in-process collector")
		return geminiquota.Output{}, nil
	}

	snap := p.FetchUsage(context.Background())
	if cli.command != "custom-gemini-helper --json" {
		t.Fatalf("expected explicit CLI command, got %q", cli.command)
	}
	if snap.Status != domain.StatusOK {
		t.Fatalf("expected ok snapshot, got %+v", snap)
	}
}
