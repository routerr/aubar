package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/routerr/aubar/internal/claudequota"
	"github.com/routerr/aubar/internal/config"
	"github.com/routerr/aubar/internal/domain"
)

type ClaudeProvider struct {
	setting       config.ProviderSetting
	cli           CLIExecutor
	client        HTTPClient
	collectOutput func(context.Context) (claudequota.Output, error)
}

func NewClaudeProvider(setting config.ProviderSetting, cli CLIExecutor) *ClaudeProvider {
	if cli == nil {
		cli = DefaultCLIExecutor{}
	}
	p := &ClaudeProvider{
		setting: setting,
		cli:     cli,
		client:  defaultHTTPClient(time.Duration(setting.TimeoutSeconds) * time.Second),
	}
	p.collectOutput = p.defaultCollectOutput
	return p
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
	if usesRuntimeCollector(cmd, "claude usage --json") {
		return p.fetchCollected(ctx)
	}
	return p.fetchCLICommand(ctx, cmd)
}

func (p *ClaudeProvider) fetchCollected(ctx context.Context) (domain.ProviderSnapshot, error) {
	out, err := p.collectOutput(ctx)
	if err != nil {
		return domain.ProviderSnapshot{}, err
	}
	if out.SubscriptionQuota != nil {
		return snapshotFromClaudeQuotaOutput(*out.SubscriptionQuota, p.ID()), nil
	}
	if snap, ok := snapshotFromClaudeUsageOutput(out, p.ID()); ok {
		return snap, nil
	}
	return domain.ProviderSnapshot{}, errors.New(joinClaudeErrors(out.Errors))
}

func (p *ClaudeProvider) fetchCLICommand(ctx context.Context, command string) (domain.ProviderSnapshot, error) {
	commands := []string{command}

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

func (p *ClaudeProvider) defaultCollectOutput(_ context.Context) (claudequota.Output, error) {
	return claudequota.Collect(context.Background(), claudequota.Options{
		Timeout:   time.Duration(p.setting.TimeoutSeconds) * time.Second,
		NoProbe:   true,
		NoQuota:   false,
		Client:    p.client,
		ClaudeDir: "",
	}), nil
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

func snapshotFromClaudeQuotaOutput(quota claudequota.SubscriptionQuota, providerID domain.ProviderID) domain.ProviderSnapshot {
	limit := 100.0
	fiveHourUsed := 100.0
	fiveHourReset := ""
	if quota.FiveHour != nil {
		fiveHourUsed = clampPercent(quota.FiveHour.UtilizationPct)
		fiveHourReset = quota.FiveHour.ResetsAt
	}
	sevenDayUsed := 100.0
	sevenDayReset := ""
	if quota.SevenDay != nil {
		sevenDayUsed = clampPercent(quota.SevenDay.UtilizationPct)
		sevenDayReset = quota.SevenDay.ResetsAt
	}
	snap := okSnapshot(providerID, "claude-quota", "percent", fiveHourUsed, &limit)
	snap.Metadata = map[string]any{
		"primary_used_percent":     fiveHourUsed,
		"primary_window_minutes":   300,
		"primary_resets_at":        fiveHourReset,
		"secondary_used_percent":   sevenDayUsed,
		"secondary_window_minutes": 10080,
		"secondary_resets_at":      sevenDayReset,
		"quota_source":             quota.Source,
	}
	if quota.ExtraUsage != nil {
		snap.Metadata["extra_usage_enabled"] = quota.ExtraUsage.IsEnabled
		snap.Metadata["extra_usage_limit_usd"] = quota.ExtraUsage.LimitUSD
		snap.Metadata["extra_usage_used_usd"] = quota.ExtraUsage.UsedUSD
	}
	return snap
}

func snapshotFromClaudeUsageOutput(out claudequota.Output, providerID domain.ProviderID) (domain.ProviderSnapshot, bool) {
	if out.Last5Hours == nil {
		return domain.ProviderSnapshot{}, false
	}
	s := degraded(providerID, "claude-local", "native remaining quota unavailable")
	s.UsageValue = out.Last5Hours.TotalCostUSD
	s.UsageUnit = "usd"
	s.Metadata = map[string]any{
		"claude_total_cost_usd": out.Last5Hours.TotalCostUSD,
	}
	if topModel, ok := claudeTopModelCost(out.Last5Hours.Models); ok {
		s.Metadata["claude_model_cost_usd"] = topModel.CostUSD
		s.Metadata["claude_model_id"] = topModel.ModelID
	}
	if out.SubscriptionQuota != nil {
		quotaMeta := snapshotFromClaudeQuotaOutput(*out.SubscriptionQuota, providerID).Metadata
		maps.Copy(s.Metadata, quotaMeta)
	}
	return s, true
}

func joinClaudeErrors(errs []claudequota.APIError) string {
	if len(errs) == 0 {
		return "claude quota unavailable"
	}
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		part := strings.TrimSpace(err.Period + ": " + err.Message)
		if strings.TrimSpace(err.Note) != "" {
			part += " (" + strings.TrimSpace(err.Note) + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " | ")
}

func claudeTopModelCost(models []claudequota.ModelUsage) (claudequota.ModelUsage, bool) {
	var top claudequota.ModelUsage
	var found bool
	for _, model := range models {
		if !found || model.CostUSD > top.CostUSD {
			top = model
			found = true
		}
	}
	return top, found
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
