package provider

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/routerr/aubar/internal/domain"
)

type codexAuth struct {
	AuthMode string `json:"auth_mode"`
}

type codexWindow struct {
	UsedPercent   float64
	WindowMinutes int
	ResetsAt      int64
}

type codexRateLimits struct {
	LimitID   string
	PlanType  string
	Primary   codexWindow
	Secondary codexWindow
}

func ProbeCodexSession() (domain.ProviderSnapshot, error) {
	return readCodexSessionSnapshot(domain.ProviderOpenAI)
}

func readCodexSessionSnapshot(providerID domain.ProviderID) (domain.ProviderSnapshot, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return domain.ProviderSnapshot{}, err
	}
	rolloutPath, err := latestCodexRolloutFile(filepath.Join(home, ".codex", "sessions"))
	if err != nil {
		return domain.ProviderSnapshot{}, err
	}
	limits, err := latestCodexRateLimits(rolloutPath)
	if err != nil {
		return domain.ProviderSnapshot{}, err
	}

	limit := 100.0
	usage := math.Max(limits.Primary.UsedPercent, limits.Secondary.UsedPercent)
	snap := okSnapshot(providerID, "cli", "percent", usage, &limit)
	snap.Metadata = map[string]any{
		"experimental":             true,
		"limit_id":                 limits.LimitID,
		"plan_type":                limits.PlanType,
		"primary_used_percent":     limits.Primary.UsedPercent,
		"primary_window_minutes":   limits.Primary.WindowMinutes,
		"primary_resets_at":        limits.Primary.ResetsAt,
		"secondary_used_percent":   limits.Secondary.UsedPercent,
		"secondary_window_minutes": limits.Secondary.WindowMinutes,
		"secondary_resets_at":      limits.Secondary.ResetsAt,
		"codex_rollout_file":       rolloutPath,
		"codex_rollout_updated_at": fileUpdatedAt(rolloutPath),
	}
	if authMode := readCodexAuthMode(filepath.Join(home, ".codex", "auth.json")); authMode != "" {
		snap.Metadata["auth_mode"] = authMode
	}
	return snap, nil
}

func latestCodexRolloutFile(root string) (string, error) {
	var newestPath string
	var newestMod time.Time
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if !strings.HasSuffix(name, ".jsonl") || !strings.Contains(name, "rollout") {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if newestPath == "" || info.ModTime().After(newestMod) {
			newestPath = path
			newestMod = info.ModTime()
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("no Codex session rate-limit telemetry found in ~/.codex/sessions yet; open a Codex session first")
		}
		return "", err
	}
	if newestPath == "" {
		return "", fmt.Errorf("no Codex session rate-limit telemetry found in ~/.codex/sessions yet; open a Codex session first")
	}
	return newestPath, nil
}

func latestCodexRateLimits(path string) (codexRateLimits, error) {
	f, err := os.Open(path)
	if err != nil {
		return codexRateLimits{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var last codexRateLimits
	var found bool
	for scanner.Scan() {
		limits, ok := extractCodexRateLimits(scanner.Bytes())
		if ok {
			last = limits
			found = true
		}
	}
	if err := scanner.Err(); err != nil {
		return codexRateLimits{}, err
	}
	if !found {
		return codexRateLimits{}, fmt.Errorf("no Codex session rate-limit telemetry found in ~/.codex/sessions yet; open a Codex session first")
	}
	return last, nil
}

func extractCodexRateLimits(line []byte) (codexRateLimits, bool) {
	var payload any
	if err := json.Unmarshal(line, &payload); err != nil {
		return codexRateLimits{}, false
	}
	raw, ok := findCodexRateLimits(payload)
	if !ok {
		return codexRateLimits{}, false
	}
	primary, primaryOK := parseCodexWindow(raw["primary"])
	secondary, secondaryOK := parseCodexWindow(raw["secondary"])
	if !primaryOK && !secondaryOK {
		return codexRateLimits{}, false
	}
	if !primaryOK {
		primary = secondary
	}
	if !secondaryOK {
		secondary = primary
	}
	return codexRateLimits{
		LimitID:   stringValue(raw["limit_id"]),
		PlanType:  stringValue(raw["plan_type"]),
		Primary:   primary,
		Secondary: secondary,
	}, true
}

func findCodexRateLimits(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		if raw, ok := t["rate_limits"]; ok {
			if limits, ok := raw.(map[string]any); ok && strings.EqualFold(stringValue(limits["limit_id"]), "codex") {
				return limits, true
			}
		}
		for _, item := range t {
			if limits, ok := findCodexRateLimits(item); ok {
				return limits, true
			}
		}
	case []any:
		for _, item := range t {
			if limits, ok := findCodexRateLimits(item); ok {
				return limits, true
			}
		}
	}
	return nil, false
}

func parseCodexWindow(v any) (codexWindow, bool) {
	raw, ok := v.(map[string]any)
	if !ok {
		return codexWindow{}, false
	}
	used, usedOK := toFloat(raw["used_percent"])
	window, windowOK := toFloat(raw["window_minutes"])
	resets, resetsOK := toFloat(raw["resets_at"])
	if !usedOK {
		return codexWindow{}, false
	}
	out := codexWindow{UsedPercent: used}
	if windowOK {
		out.WindowMinutes = int(window)
	}
	if resetsOK {
		out.ResetsAt = int64(resets)
	}
	return out, true
}

func readCodexAuthMode(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	// Limit read to 1MB (GO-HTTPCLIENT-001)
	raw, err := io.ReadAll(io.LimitReader(f, 1024*1024))
	if err != nil {
		return ""
	}
	var auth codexAuth
	if err := json.Unmarshal(raw, &auth); err != nil {
		return ""
	}
	return strings.TrimSpace(auth.AuthMode)
}

func fileUpdatedAt(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return info.ModTime().UTC().Format(time.RFC3339)
}
