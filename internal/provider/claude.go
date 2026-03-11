package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/raychang/ai-usage-bar/internal/config"
	"github.com/raychang/ai-usage-bar/internal/domain"
)

type ClaudeProvider struct {
	setting config.ProviderSetting
	cli     CLIExecutor
	client  HTTPClient
}

func NewClaudeProvider(setting config.ProviderSetting, cli CLIExecutor) *ClaudeProvider {
	if cli == nil {
		cli = DefaultCLIExecutor{}
	}
	return &ClaudeProvider{
		setting: setting,
		cli:     cli,
		client:  defaultHTTPClient(time.Duration(setting.TimeoutSeconds) * time.Second),
	}
}

func (p *ClaudeProvider) ID() domain.ProviderID { return domain.ProviderClaude }

func (p *ClaudeProvider) MinInterval() time.Duration {
	interval := time.Duration(p.setting.MinIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	return interval
}

func (p *ClaudeProvider) FetchUsage(ctx context.Context) domain.ProviderSnapshot {
	var errs []string
	for _, src := range p.setting.SourceOrder {
		src = strings.ToLower(strings.TrimSpace(src))
		switch src {
		case "cli":
			s, err := p.fetchCLI(ctx)
			if err == nil {
				return s
			}
			errs = append(errs, "cli: "+err.Error())
		}
	}
	return errored(p.ID(), "combined", strings.Join(errs, " | "))
}

func (p *ClaudeProvider) fetchCLI(ctx context.Context) (domain.ProviderSnapshot, error) {
	cmd := strings.TrimSpace(p.setting.CLICommand)
	commands := []string{}
	switch cmd {
	case "", "claude usage --json":
		commands = append(commands, quotaCLICommands()...)
	default:
		commands = append(commands, cmd)
	}

	timeout := time.Duration(p.setting.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	var errs []string
	for _, command := range commands {
		out, errOut, attempts, err := runCLIWithRetry(ctx, p.cli, command, timeout, 1)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v (%s)", command, err, strings.TrimSpace(errOut)))
			continue
		}

		// Try parsing as quota first
		if snap, err := parseClaudeQuotaSnapshot(out, p.ID()); err == nil {
			if snap.Metadata == nil {
				snap.Metadata = map[string]any{}
			}
			snap.Metadata["cli_attempts"] = attempts
			snap.Metadata["cli_command"] = command
			return snap, nil
		}

		// Fallback to basic usage parsing
		payload, err := parseCLIJSON(out)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", command, err))
			continue
		}
		usage, ok := findFirstNumber(payload, "usage", "total", "tokens", "cost", "amount")
		if !ok {
			errs = append(errs, fmt.Sprintf("%s: usage field missing", command))
			continue
		}
		unit := "tokens"
		if _, ok := findFirstNumber(payload, "cost", "amount", "usd"); ok {
			unit = "usd"
		}
		s := degraded(p.ID(), "cli", "native remaining quota unavailable")
		s.UsageValue = usage
		s.UsageUnit = unit
		s.Metadata = map[string]any{"cli_attempts": attempts, "cli_command": command}
		return s, nil
	}

	return domain.ProviderSnapshot{}, fmt.Errorf("cli failed: %s", strings.Join(errs, " | "))
}

func quotaCLICommands() []string {
	commands := []string{"./quota"}
	if exe, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), "quota")
		quoted := fmt.Sprintf("%q", sibling)
		if quoted != commands[0] {
			commands = append(commands, quoted)
		}
	}
	return commands
}

func parseClaudeQuotaSnapshot(stdout string, providerID domain.ProviderID) (domain.ProviderSnapshot, error) {
	payload, err := parseCLIJSON(stdout)
	if err != nil {
		return domain.ProviderSnapshot{}, err
	}

	rawQuota, ok := payload["subscription_quota"].(map[string]any)
	if !ok {
		return domain.ProviderSnapshot{}, fmt.Errorf("subscription_quota missing in quota output")
	}

	fiveHour, err := parseClaudeQuotaWindow(rawQuota["five_hour"])
	if err != nil {
		return domain.ProviderSnapshot{}, fmt.Errorf("five_hour quota invalid: %w", err)
	}
	sevenDay, err := parseClaudeQuotaWindow(rawQuota["seven_day"])
	if err != nil {
		return domain.ProviderSnapshot{}, fmt.Errorf("seven_day quota invalid: %w", err)
	}

	limit := 100.0
	snap := okSnapshot(providerID, "claude-quota", "percent", fiveHour.usedPercent, &limit)
	snap.Metadata = map[string]any{
		"primary_used_percent":     fiveHour.usedPercent,
		"primary_window_minutes":   300,
		"primary_resets_at":        fiveHour.resetsAt,
		"secondary_used_percent":   sevenDay.usedPercent,
		"secondary_window_minutes": 10080,
		"secondary_resets_at":      sevenDay.resetsAt,
		"quota_source":             "subscription_quota",
	}
	return snap, nil
}

type claudeQuotaWindow struct {
	usedPercent float64
	resetsAt    string
}

func parseClaudeQuotaWindow(v any) (claudeQuotaWindow, error) {
	raw, ok := v.(map[string]any)
	if !ok {
		return claudeQuotaWindow{}, fmt.Errorf("expected object")
	}

	usedPercent, ok := toFloat(raw["utilization_pct"])
	if !ok {
		return claudeQuotaWindow{}, fmt.Errorf("utilization_pct missing")
	}

	return claudeQuotaWindow{
		usedPercent: clampPercent(usedPercent),
		resetsAt:    stringValue(raw["resets_at"]),
	}, nil
}

func clampPercent(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return v
	}
}
