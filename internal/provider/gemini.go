package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/raychang/ai-usage-bar/internal/config"
	"github.com/raychang/ai-usage-bar/internal/domain"
)

type GeminiProvider struct {
	setting config.ProviderSetting
	cli     CLIExecutor
	client  HTTPClient
}

func NewGeminiProvider(setting config.ProviderSetting, cli CLIExecutor) *GeminiProvider {
	if cli == nil {
		cli = DefaultCLIExecutor{}
	}
	return &GeminiProvider{
		setting: setting,
		cli:     cli,
		client:  defaultHTTPClient(time.Duration(setting.TimeoutSeconds) * time.Second),
	}
}

func (p *GeminiProvider) ID() domain.ProviderID { return domain.ProviderGemini }

func (p *GeminiProvider) MinInterval() time.Duration {
	return time.Duration(p.setting.MinIntervalSeconds) * time.Second
}

func (p *GeminiProvider) FetchUsage(ctx context.Context) domain.ProviderSnapshot {
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

func (p *GeminiProvider) fetchCLI(ctx context.Context) (domain.ProviderSnapshot, error) {
	cmd := strings.TrimSpace(p.setting.CLICommand)
	commands := []string{}
	switch cmd {
	case "", "gemini usage --json":
		commands = append(commands, geminiQuotaCLICommands()...)
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

		payload, err := parseCLIJSON(out)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", command, err))
			continue
		}
		if modelQuota, ok := extractGeminiModelQuota(payload); ok {
			s, err := geminiSnapshotFromModelQuota(p.ID(), modelQuota)
			if err == nil {
				if s.Metadata == nil {
					s.Metadata = map[string]any{}
				}
				s.Metadata["cli_attempts"] = attempts
				s.Metadata["cli_command"] = command
				return s, nil
			}
			errs = append(errs, fmt.Sprintf("%s: %v", command, err))
			continue
		}
		if stats, ok := extractGeminiCLIStats(payload); ok {
			s := okSnapshot(p.ID(), "cli", stats.unit, stats.usage, &stats.limit)
			if stats.remainingPercent != nil {
				remaining := clampGeminiPercent(*stats.remainingPercent)
				s.RemainingPercent = &remaining
				if s.LimitValue != nil && *s.LimitValue > 0 {
					remainingValue := (*s.LimitValue * remaining) / 100
					s.RemainingValue = &remainingValue
				}
			}
			s.Metadata = map[string]any{"cli_attempts": attempts, "cli_command": command}
			if stats.model != "" {
				s.Metadata["model"] = stats.model
			}
			return s, nil
		}

		usage, ok := findFirstNumber(payload, "usage", "total", "tokens", "cost", "amount")
		if !ok {
			s := degraded(p.ID(), "cli", "native remaining quota unavailable")
			s.Metadata = map[string]any{
				"raw_supported": true,
				"cli_attempts":  attempts,
				"cli_command":   command,
			}
			return s, nil
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

	if len(errs) == 0 {
		return domain.ProviderSnapshot{}, errors.New("cli command unavailable")
	}
	return domain.ProviderSnapshot{}, errors.New(strings.Join(errs, " | "))
}

type geminiCLIStats struct {
	usage            float64
	limit            float64
	remainingPercent *float64
	unit             string
	model            string
}

func extractGeminiCLIStats(v any) (geminiCLIStats, bool) {
	switch t := v.(type) {
	case map[string]any:
		stats, ok := geminiCLIStatsFromMap(t)
		if ok {
			return stats, true
		}
		for _, raw := range t {
			if stats, ok := extractGeminiCLIStats(raw); ok {
				return stats, true
			}
		}
	case []any:
		for _, item := range t {
			if stats, ok := extractGeminiCLIStats(item); ok {
				return stats, true
			}
		}
	}
	return geminiCLIStats{}, false
}

func geminiCLIStatsFromMap(m map[string]any) (geminiCLIStats, bool) {
	model := firstString(m, "model", "modelName", "name")

	limit, hasLimit := firstNumberFromMap(m,
		"limit", "quota", "max", "dailyLimit", "tokenLimit", "requestLimit", "quotaLimit",
	)
	used, hasUsed := firstNumberFromMap(m,
		"used", "usage", "usedTokens", "tokensUsed", "total", "totalTokens", "tokenCount",
	)
	remaining, hasRemaining := firstNumberFromMap(m,
		"remaining", "remainingTokens", "quotaRemaining",
	)
	remainingPercent, hasRemainingPercent := firstNumberFromMap(m,
		"remainingPercent", "remaining_percent", "quotaRemainingPercent", "percentRemaining",
	)

	if !hasLimit {
		return geminiCLIStats{}, false
	}
	if !hasUsed && hasRemaining {
		used = limit - remaining
		hasUsed = true
	}
	if !hasRemainingPercent {
		switch {
		case hasRemaining && limit > 0:
			remainingPercent = (remaining / limit) * 100
			hasRemainingPercent = true
		case hasUsed && limit > 0:
			remainingPercent = ((limit - used) / limit) * 100
			hasRemainingPercent = true
		}
	}
	if !hasUsed {
		return geminiCLIStats{}, false
	}

	unit := "tokens"
	if firstNumberExistsInMap(m, "cost", "usd", "amountUsd", "spentUsd") {
		unit = "usd"
	}

	stats := geminiCLIStats{
		usage: used,
		limit: limit,
		unit:  unit,
		model: model,
	}
	if hasRemainingPercent {
		p := clampGeminiPercent(remainingPercent)
		stats.remainingPercent = &p
	}
	return stats, true
}

func firstNumberFromMap(m map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		raw, ok := m[key]
		if !ok {
			continue
		}
		if n, ok := toFloat(raw); ok {
			return n, true
		}
	}
	return 0, false
}

func firstNumberExistsInMap(m map[string]any, keys ...string) bool {
	_, ok := firstNumberFromMap(m, keys...)
	return ok
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := m[key]
		if !ok {
			continue
		}
		if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func clampGeminiPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

type geminiModelQuota struct {
	ModelID          string
	RemainingPercent float64
}

func geminiQuotaCLICommands() []string {
	commands := []string{"./gemini-quota"}
	if exe, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), "gemini-quota")
		quoted := fmt.Sprintf("%q", sibling)
		if quoted != commands[0] {
			commands = append(commands, quoted)
		}
	}
	return commands
}

func extractGeminiModelQuota(v any) ([]geminiModelQuota, bool) {
	root, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	rawModels, ok := root["models"].([]any)
	if !ok || len(rawModels) == 0 {
		return nil, false
	}

	models := make([]geminiModelQuota, 0, len(rawModels))
	for _, item := range rawModels {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		modelID := strings.TrimSpace(stringValue(row["model_id"]))
		remaining, ok := toFloat(row["remaining_percent"])
		if modelID == "" || !ok {
			continue
		}
		models = append(models, geminiModelQuota{
			ModelID:          strings.ToLower(modelID),
			RemainingPercent: clampGeminiPercent(remaining),
		})
	}
	if len(models) == 0 {
		return nil, false
	}
	return models, true
}

func geminiSnapshotFromModelQuota(providerID domain.ProviderID, models []geminiModelQuota) (domain.ProviderSnapshot, error) {
	leftChain := []string{"gemini-3.1-pro", "gemini-3-pro", "gemini-2.5-pro", "gemini-1.5-pro"}
	rightChain := []string{"gemini-3.1-flash", "gemini-3.1-flash-lite", "gemini-3-flash", "gemini-3-flash-lite", "gemini-2.5-flash-lite", "gemini-2.5-flash", "gemini-1.5-flash"}

	left, leftID, leftMajor, leftOK := geminiSelectChainPercent(models, leftChain)
	right, rightID, rightMajor, rightOK := geminiSelectChainPercent(models, rightChain)
	if !leftOK && !rightOK {
		return domain.ProviderSnapshot{}, fmt.Errorf("no matching pro/flash model quotas found")
	}

	if !leftOK {
		left = right
		leftID = rightID
		leftMajor = rightMajor
	}
	if !rightOK {
		right = left
		rightID = leftID
		rightMajor = leftMajor
	}

	limit := 100.0
	used := 100 - clampGeminiPercent(minFloat(left, right))
	s := okSnapshot(providerID, "cli", "percent", used, &limit)
	s.Metadata = map[string]any{
		"gemini_left_remaining_percent":   clampGeminiPercent(left),
		"gemini_right_remaining_percent":  clampGeminiPercent(right),
		"gemini_left_model_id":            leftID,
		"gemini_right_model_id":           rightID,
		"gemini_left_major_version_tag":   leftMajor,
		"gemini_right_major_version_tag":  rightMajor,
		"gemini_left_fallback_chain":      strings.Join(leftChain, ","),
		"gemini_right_fallback_chain":     strings.Join(rightChain, ","),
		"gemini_layout_format":            "G XX% YY%",
		"gemini_layout_value_description": "left=pro chain right=flash chain",
	}
	return s, nil
}

func geminiSelectChainPercent(models []geminiModelQuota, chain []string) (percent float64, modelID string, majorTag string, ok bool) {
	byModel := make(map[string]float64, len(models))
	for _, m := range models {
		byModel[m.ModelID] = m.RemainingPercent
	}
	var exhaustedCandidateFound bool
	var exhaustedPercent float64
	var exhaustedModelID string
	var exhaustedMajorTag string
	for _, candidate := range chain {
		p, matchedModelID, found := findGeminiRemaining(byModel, candidate)
		if !found {
			continue
		}
		remaining := clampGeminiPercent(p)
		if remaining > 0 {
			return remaining, matchedModelID, geminiMajorTag(candidate), true
		}
		if !exhaustedCandidateFound {
			exhaustedCandidateFound = true
			exhaustedPercent = remaining
			exhaustedModelID = matchedModelID
			exhaustedMajorTag = geminiMajorTag(candidate)
		}
	}
	if exhaustedCandidateFound {
		return exhaustedPercent, exhaustedModelID, exhaustedMajorTag, true
	}
	return 0, "", "", false
}

func findGeminiRemaining(byModel map[string]float64, candidate string) (float64, string, bool) {
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	if candidate == "" {
		return 0, "", false
	}
	if p, ok := byModel[candidate]; ok {
		return p, candidate, true
	}
	candidates := make([]string, 0, len(byModel))
	for modelID := range byModel {
		if modelID == candidate {
			return byModel[modelID], modelID, true
		}
		if strings.HasPrefix(modelID, candidate+"-") || strings.HasPrefix(modelID, candidate+"_") {
			candidates = append(candidates, modelID)
		}
	}
	if len(candidates) == 0 {
		return 0, "", false
	}
	sort.Strings(candidates)
	return byModel[candidates[0]], candidates[0], true
}

func geminiMajorTag(modelID string) string {
	id := strings.ToLower(strings.TrimSpace(modelID))
	switch {
	case strings.Contains(id, "gemini-3.") || strings.Contains(id, "gemini-3-"):
		return "3"
	case strings.Contains(id, "gemini-2.") || strings.Contains(id, "gemini-2-"):
		return "2"
	case strings.Contains(id, "gemini-1.") || strings.Contains(id, "gemini-1-"):
		return "1"
	default:
		return "?"
	}
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
