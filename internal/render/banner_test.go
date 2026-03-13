package render

import (
	"strings"
	"testing"
	"time"

	"github.com/routerr/aubar/internal/domain"
)

func TestRenderLine(t *testing.T) {
	p := 74.0
	col := domain.Collection{
		GeneratedAt: time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC),
		Snapshots: []domain.ProviderSnapshot{
			{Provider: domain.ProviderOpenAI, Status: domain.StatusOK, RemainingPercent: &p},
			{Provider: domain.ProviderClaude, Status: domain.StatusDegraded, UsageValue: 0.1, UsageUnit: "usd", Reason: "native remaining quota unavailable", Metadata: map[string]any{"claude_total_cost_usd": 0.1, "claude_model_cost_usd": 0.08}},
		},
	}
	line := RenderLine(col, false)
	for _, needle := range []string{"❀ 74%", "✽ 0.10$ 0.08$"} {
		if !strings.Contains(line, needle) {
			t.Fatalf("missing %s in %q", needle, line)
		}
	}
	for _, needle := range []string{"offline"} {
		if strings.Contains(line, needle) {
			t.Fatalf("unexpected %s in %q", needle, line)
		}
	}
}

func TestRenderLineShowsUsageOnlyProvidersAsDisconnected(t *testing.T) {
	col := domain.Collection{
		GeneratedAt: time.Date(2026, 3, 8, 3, 0, 0, 0, time.UTC),
		Snapshots: []domain.ProviderSnapshot{
			{Provider: domain.ProviderClaude, Status: domain.StatusDegraded, Source: "claude-session", UsageValue: 0.1, UsageUnit: "usd", Reason: "native remaining quota unavailable"},
		},
	}

	line := RenderLineWithoutTimestamp(col, false)
	for _, needle := range []string{"✽ 0.10$ 0.10$"} {
		if !strings.Contains(line, needle) {
			t.Fatalf("expected %s in %q", needle, line)
		}
	}
	if strings.Contains(line, "waiting for data") {
		t.Fatalf("expected provider content in %q", line)
	}
}

func TestRenderLineOmitsProvidersWithoutUsableData(t *testing.T) {
	p := 71.0
	col := domain.Collection{
		GeneratedAt: time.Date(2026, 3, 8, 3, 0, 0, 0, time.UTC),
		Snapshots: []domain.ProviderSnapshot{
			{Provider: domain.ProviderOpenAI, Status: domain.StatusOK, Source: "codex-session", RemainingPercent: &p},
			{Provider: domain.ProviderGemini, Status: domain.StatusDegraded, Source: "gemini-session", Reason: "temporary backend error"},
		},
	}

	line := RenderLineWithoutTimestamp(col, false)
	if strings.Contains(line, "") {
		t.Fatalf("expected unavailable provider to be hidden in %q", line)
	}
}

func TestRenderLineMarksExperimentalSessionSources(t *testing.T) {
	p := 71.0
	geminiIcon := providerBrainIcon(domain.ProviderSnapshot{Provider: domain.ProviderGemini}, false)
	col := domain.Collection{
		GeneratedAt: time.Date(2026, 3, 8, 3, 0, 0, 0, time.UTC),
		Snapshots: []domain.ProviderSnapshot{
			{Provider: domain.ProviderOpenAI, Status: domain.StatusOK, Source: "codex-session", RemainingPercent: &p},
			{Provider: domain.ProviderClaude, Status: domain.StatusDegraded, Source: "claude-session", UsageValue: 0.1, UsageUnit: "usd", Reason: "native remaining quota unavailable"},
			{Provider: domain.ProviderGemini, Status: domain.StatusDegraded, Source: "gemini-session", UsageValue: 240, UsageUnit: "tokens", Reason: "native remaining quota unavailable"},
		},
	}
	line := RenderLine(col, false)
	for _, needle := range []string{"❀ 71%", "✽ 0.10$ 0.10$", geminiIcon} {
		if !strings.Contains(line, needle) {
			t.Fatalf("expected %s in %q", needle, line)
		}
	}
	for _, needle := range []string{"240tok", "[offline]"} {
		if strings.Contains(line, needle) {
			t.Fatalf("unexpected %s in %q", needle, line)
		}
	}
}

func TestRenderLineShowsDisconnectedForUnknownUsageOnlyWhenNeeded(t *testing.T) {
	geminiIcon := providerBrainIcon(domain.ProviderSnapshot{Provider: domain.ProviderGemini}, false)
	col := domain.Collection{
		GeneratedAt: time.Date(2026, 3, 8, 3, 0, 0, 0, time.UTC),
		Snapshots: []domain.ProviderSnapshot{
			{Provider: domain.ProviderGemini, Status: domain.StatusDegraded, Source: "api", Reason: "native remaining quota unavailable"},
		},
	}

	line := RenderLineWithoutTimestamp(col, false)
	if !strings.Contains(line, geminiIcon) {
		t.Fatalf("expected disconnected provider in %q", line)
	}
	for _, needle := range []string{"[", "]", "offline"} {
		if strings.Contains(line, needle) {
			t.Fatalf("unexpected %s in %q", needle, line)
		}
	}
}

func TestRenderLineWithoutTimestampOmitsClock(t *testing.T) {
	p := 71.0
	col := domain.Collection{
		GeneratedAt: time.Date(2026, 3, 8, 3, 0, 0, 0, time.UTC),
		Snapshots: []domain.ProviderSnapshot{
			{Provider: domain.ProviderOpenAI, Status: domain.StatusOK, Source: "codex-session", RemainingPercent: &p},
		},
	}
	line := RenderLineWithoutTimestamp(col, false)
	if strings.Contains(line, "03:00:00") {
		t.Fatalf("expected no timestamp in %q", line)
	}
	if !strings.Contains(line, "❀ 71%") {
		t.Fatalf("expected provider content in %q", line)
	}
}

func TestRenderLineShowsCodexFiveHourWindowAlongsideActiveWindow(t *testing.T) {
	p := 68.0
	col := domain.Collection{
		GeneratedAt: time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC),
		Snapshots: []domain.ProviderSnapshot{
			{
				Provider:         domain.ProviderOpenAI,
				Status:           domain.StatusOK,
				Source:           "codex-session",
				RemainingPercent: &p,
				Metadata: map[string]any{
					"primary_used_percent":     16.0,
					"primary_window_minutes":   300,
					"secondary_used_percent":   0.0,
					"secondary_window_minutes": 10080,
				},
			},
		},
	}

	line := RenderLineWithoutTimestamp(col, false)
	if !strings.Contains(line, "❀ 84% 100%") {
		t.Fatalf("expected codex summary in %q", line)
	}
}

func TestRenderLineShowsClaudeQuotaWindowsInOpenAIStyle(t *testing.T) {
	p := 77.0
	col := domain.Collection{
		GeneratedAt: time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC),
		Snapshots: []domain.ProviderSnapshot{
			{
				Provider:         domain.ProviderClaude,
				Status:           domain.StatusOK,
				Source:           "claude-quota",
				RemainingPercent: &p,
				Metadata: map[string]any{
					"primary_used_percent":     23.0,
					"primary_window_minutes":   300,
					"secondary_used_percent":   15.0,
					"secondary_window_minutes": 10080,
				},
			},
		},
	}

	line := RenderLineWithoutTimestamp(col, false)
	if !strings.Contains(line, "✽ 77% 85%") {
		t.Fatalf("expected claude quota summary in %q", line)
	}
}

func TestRenderLineShowsGeminiDualPercentLayout(t *testing.T) {
	p := 85.0
	geminiIcon := providerBrainIcon(domain.ProviderSnapshot{Provider: domain.ProviderGemini}, false)
	col := domain.Collection{
		GeneratedAt: time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC),
		Snapshots: []domain.ProviderSnapshot{
			{
				Provider:         domain.ProviderGemini,
				Status:           domain.StatusOK,
				Source:           "cli",
				RemainingPercent: &p,
				Metadata: map[string]any{
					"gemini_left_remaining_percent":  85.0,
					"gemini_right_remaining_percent": 89.0,
					"gemini_left_major_version_tag":  "3",
					"gemini_right_major_version_tag": "2",
				},
			},
		},
	}

	line := RenderLineWithoutTimestamp(col, false)
	if !strings.Contains(line, geminiIcon+" 3-85% 2-89%") {
		t.Fatalf("expected gemini dual summary in %q", line)
	}
}

func TestRenderLineShowsGeminiMajorTagsInTmuxColor(t *testing.T) {
	p := 85.0
	geminiIcon := providerBrainIcon(domain.ProviderSnapshot{Provider: domain.ProviderGemini, Status: domain.StatusOK, RemainingPercent: &p}, true)
	col := domain.Collection{
		GeneratedAt: time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC),
		Snapshots: []domain.ProviderSnapshot{
			{
				Provider:         domain.ProviderGemini,
				Status:           domain.StatusOK,
				Source:           "cli",
				RemainingPercent: &p,
				Metadata: map[string]any{
					"gemini_left_remaining_percent":  85.0,
					"gemini_right_remaining_percent": 89.0,
					"gemini_left_major_version_tag":  "3",
					"gemini_right_major_version_tag": "2",
				},
			},
		},
	}

	line := RenderLineWithoutTimestamp(col, true)
	for _, needle := range []string{
		geminiIcon,
		"#[fg=#9399b2,nobold]3-#[default]#[fg=#94e2d5,nobold]85#[default]#[fg=#a6adc8,nobold]%#[default]",
		"#[fg=#9399b2,nobold]2-#[default]#[fg=#94e2d5,nobold]89#[default]#[fg=#a6adc8,nobold]%#[default]",
	} {
		if !strings.Contains(line, needle) {
			t.Fatalf("expected %s in %q", needle, line)
		}
	}
}

func TestRenderLineUsesTmuxColorForConnectedAndDisconnectedBrains(t *testing.T) {
	p := 50.0
	disconnectedGeminiIcon := providerBrainIcon(domain.ProviderSnapshot{Provider: domain.ProviderGemini, Status: domain.StatusDegraded}, true)
	col := domain.Collection{
		GeneratedAt: time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC),
		Snapshots: []domain.ProviderSnapshot{
			{Provider: domain.ProviderOpenAI, Status: domain.StatusOK, RemainingPercent: &p},
			{Provider: domain.ProviderGemini, Status: domain.StatusDegraded, Reason: "native remaining quota unavailable"},
		},
	}

	line := RenderLineWithoutTimestamp(col, true)
	for _, needle := range []string{
		"#[fg=#cdd6f4,nobold]❀#[default] #[fg=#f9e2af,nobold]50#[default]#[fg=#a6adc8,nobold]%#[default]",
		disconnectedGeminiIcon,
	} {
		if !strings.Contains(line, needle) {
			t.Fatalf("expected %s in %q", needle, line)
		}
	}
	if strings.Contains(line, "offline") {
		t.Fatalf("expected offline label to be hidden in %q", line)
	}
}

func TestRenderLineShowsTimedOutProviderAsDisconnected(t *testing.T) {
	p := 50.0
	disconnectedGeminiIcon := providerBrainIcon(domain.ProviderSnapshot{Provider: domain.ProviderGemini, Status: domain.StatusError}, true)
	col := domain.Collection{
		GeneratedAt: time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC),
		Snapshots: []domain.ProviderSnapshot{
			{Provider: domain.ProviderOpenAI, Status: domain.StatusOK, RemainingPercent: &p},
			{Provider: domain.ProviderGemini, Status: domain.StatusError, Source: "timeout", Reason: "provider fetch exceeded 12s timeout"},
		},
	}

	line := RenderLineWithoutTimestamp(col, true)
	for _, needle := range []string{
		"#[fg=#cdd6f4,nobold]❀#[default] #[fg=#f9e2af,nobold]50#[default]#[fg=#a6adc8,nobold]%#[default]",
		disconnectedGeminiIcon,
	} {
		if !strings.Contains(line, needle) {
			t.Fatalf("expected %s in %q", needle, line)
		}
	}
}

func TestRenderLineColorsPercentThresholdsInTmux(t *testing.T) {
	high := 80.0
	green := 70.0
	yellow := 50.0
	peach := 40.0
	maroon := 20.0
	low := 10.0
	zero := 0.0

	col := domain.Collection{
		GeneratedAt: time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC),
		Snapshots: []domain.ProviderSnapshot{
			{Provider: domain.ProviderOpenAI, Status: domain.StatusOK, RemainingPercent: &high},
			{Provider: "other-green", Status: domain.StatusOK, RemainingPercent: &green},
			{Provider: domain.ProviderGemini, Status: domain.StatusOK, RemainingPercent: &yellow, Metadata: map[string]any{
				"gemini_left_remaining_percent":  peach,
				"gemini_right_remaining_percent": maroon,
				"gemini_left_major_version_tag":  "3",
				"gemini_right_major_version_tag": "2",
			}},
			{Provider: "other-low", Status: domain.StatusOK, RemainingPercent: &low},
			{Provider: "other-zero", Status: domain.StatusOK, RemainingPercent: &zero},
		},
	}

	line := RenderLineWithoutTimestamp(col, true)
	for _, needle := range []string{
		"#[fg=#cdd6f4,nobold]❀#[default] #[fg=#94e2d5,nobold]80#[default]#[fg=#a6adc8,nobold]%#[default]",
		"#[fg=#cdd6f4,nobold]#[default] #[fg=#a6e3a1,nobold]70#[default]#[fg=#a6adc8,nobold]%#[default]",
		"#[fg=#9399b2,nobold]3-#[default]#[fg=#fab387,nobold]40#[default]#[fg=#a6adc8,nobold]%#[default]",
		"#[fg=#9399b2,nobold]2-#[default]#[fg=#eba0ac,nobold]20#[default]#[fg=#a6adc8,nobold]%#[default]",
		"#[fg=#cdd6f4,nobold]#[default] #[fg=#f38ba8,nobold]10#[default]#[fg=#a6adc8,nobold]%#[default]",
		"#[fg=#cdd6f4,nobold]#[default] #[fg=#45475a,nobold]0#[default]#[fg=#a6adc8,nobold]%#[default]",
	} {
		if !strings.Contains(line, needle) {
			t.Fatalf("expected %s in %q", needle, line)
		}
	}
}
