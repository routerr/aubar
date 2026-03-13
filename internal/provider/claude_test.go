package provider

import (
	"context"
	"testing"

	"github.com/routerr/aubar/internal/claudequota"
	"github.com/routerr/aubar/internal/config"
	"github.com/routerr/aubar/internal/domain"
)

func TestClaudeProviderFetchUsageUsesInProcessCollectorByDefault(t *testing.T) {
	cli := &captureCLI{}
	p := NewClaudeProvider(config.ProviderSetting{
		Enabled:            true,
		SourceOrder:        []string{"cli"},
		CLICommand:         "",
		TimeoutSeconds:     2,
		MinIntervalSeconds: 30,
	}, cli)
	p.collectOutput = func(_ context.Context) (claudequota.Output, error) {
		return claudequota.Output{
			SubscriptionQuota: &claudequota.SubscriptionQuota{
				Source: "api.anthropic.com/api/oauth/usage",
				FiveHour: &claudequota.QuotaWindow{
					UtilizationPct: 23,
					ResetsAt:       "2026-03-13T12:00:00Z",
				},
				SevenDay: &claudequota.QuotaWindow{
					UtilizationPct: 15,
					ResetsAt:       "2026-03-19T12:00:00Z",
				},
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
	if snap.Source != "claude-quota" {
		t.Fatalf("expected claude-quota source, got %+v", snap)
	}
	if snap.RemainingPercent == nil || *snap.RemainingPercent != 77 {
		t.Fatalf("expected 77 remaining percent, got %+v", snap)
	}
	if got, ok := snap.Metadata["secondary_used_percent"].(float64); !ok || got != 15 {
		t.Fatalf("expected secondary quota metadata, got %+v", snap.Metadata)
	}
}

func TestClaudeProviderFetchUsageUsesExplicitCLIOverride(t *testing.T) {
	cli := &captureCLI{
		stdout: `{
			"subscription_quota": {
				"five_hour": {"utilization_pct": 42, "resets_at": "2026-03-13T12:00:00Z"},
				"seven_day": {"utilization_pct": 18, "resets_at": "2026-03-19T12:00:00Z"}
			}
		}`,
	}
	p := NewClaudeProvider(config.ProviderSetting{
		Enabled:            true,
		SourceOrder:        []string{"cli"},
		CLICommand:         "custom-claude-helper --json",
		TimeoutSeconds:     2,
		MinIntervalSeconds: 30,
	}, cli)
	p.collectOutput = func(_ context.Context) (claudequota.Output, error) {
		t.Fatal("expected explicit CLI override to bypass in-process collector")
		return claudequota.Output{}, nil
	}

	snap := p.FetchUsage(context.Background())
	if cli.command != "custom-claude-helper --json" {
		t.Fatalf("expected explicit CLI command, got %q", cli.command)
	}
	if snap.Status != domain.StatusOK {
		t.Fatalf("expected ok snapshot, got %+v", snap)
	}
}
