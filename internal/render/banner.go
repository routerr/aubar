package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/raychang/ai-usage-bar/internal/domain"
)

const (
	brainIcon       = ""
	colorAvailable  = "#cdd6f4"
	colorUnavailable = "#45475a"
	colorHigh       = "#94e2d5"
	colorGreen      = "#a6e3a1"
	colorYellow     = "#f9e2af"
	colorPeach      = "#fab387"
	colorMaroon     = "#eba0ac"
	colorPink       = "#f38ba8"
	colorTag        = "#9399b2"
	colorText       = "#bac2de"
	colorPercentSign = "#a6adc8"
)

func RenderLine(col domain.Collection, tmuxColors bool) string {
	return renderLine(col, tmuxColors, true)
}

func RenderLineWithoutTimestamp(col domain.Collection, tmuxColors bool) string {
	return renderLine(col, tmuxColors, false)
}

func renderLine(col domain.Collection, tmuxColors bool, includeTimestamp bool) string {
	if len(col.Snapshots) == 0 {
		return "○ no providers configured"
	}
	var parts []string
	for _, s := range col.Snapshots {
		if chunk := renderProvider(s, tmuxColors); chunk != "" {
			parts = append(parts, chunk)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "○ waiting for data")
	}
	if includeTimestamp {
		parts = append(parts, col.GeneratedAt.Format("15:04:05"))
	}
	return strings.Join(parts, " | ")
}

func renderProvider(s domain.ProviderSnapshot, tmuxColors bool) string {
	brain := providerBrainIcon(s, tmuxColors)
	name := providerNameColored(s, tmuxColors)
	summary := providerSummary(s, tmuxColors)
	if !providerConnected(s) {
		if shouldShowDisconnectedProvider(s) {
			if summary != "" {
				return joinNonEmpty(brain, name, summary)
			}
			return joinNonEmpty(brain, name)
		}
		return ""
	}
	if summary == "" {
		return joinNonEmpty(brain, name)
	}
	return joinNonEmpty(brain, name, summary)
}

func providerSummary(s domain.ProviderSnapshot, tmuxColors bool) string {
	if s.Provider == domain.ProviderGemini {
		if pair, ok := geminiPairSummary(s, tmuxColors); ok {
			return pair
		}
	}
	if primary, secondary, ok := quotaWindowSummary(s); ok {
		parts := []string{formatPercent(primary.percent)}
		colors := []string{percentColor(primary.percent)}
		if secondary != nil {
			parts = append(parts, formatPercent(secondary.percent))
			colors = append(colors, percentColor(secondary.percent))
		}
		if !tmuxColors {
			return strings.Join(parts, " ")
		}
		return joinColoredPercents(parts, colors)
	}
	var text string
	if s.Provider == domain.ProviderClaude {
		text = claudeCostSummary(s)
	} else if s.RemainingPercent != nil {
		text = formatPercent(*s.RemainingPercent)
		if tmuxColors {
			return colorizePercent(*s.RemainingPercent)
		}
		return text
	}
	if text != "" && tmuxColors {
		return colorizeText(text, colorText)
	}
	return text
}

func geminiPairSummary(s domain.ProviderSnapshot, tmuxColors bool) (string, bool) {
	metadata := s.Metadata
	if len(metadata) == 0 {
		return "", false
	}
	left, leftOK := metadataFloat(metadata, "gemini_left_remaining_percent")
	right, rightOK := metadataFloat(metadata, "gemini_right_remaining_percent")
	if !leftOK && !rightOK {
		return "", false
	}
	if !leftOK {
		left = right
	}
	if !rightOK {
		right = left
	}
	leftTag, _ := metadata["gemini_left_major_version_tag"].(string)
	rightTag, _ := metadata["gemini_right_major_version_tag"].(string)
	if strings.TrimSpace(leftTag) == "" {
		leftTag = "?"
	}
	if strings.TrimSpace(rightTag) == "" {
		rightTag = "?"
	}
	leftPercent := formatPercent(left)
	rightPercent := formatPercent(right)
	if tmuxColors {
		versionColor := "#[fg=" + colorTag + ",nobold]"
		leftTag = versionColor + leftTag + "-#[default]"
		rightTag = versionColor + rightTag + "-#[default]"
		leftPercent = colorizePercent(left)
		rightPercent = colorizePercent(right)
	} else {
		leftTag = leftTag + "-"
		rightTag = rightTag + "-"
	}
	return fmt.Sprintf("%s%s %s%s", leftTag, leftPercent, rightTag, rightPercent), true
}

func quotaWindowSummary(s domain.ProviderSnapshot) (primary providerWindow, secondary *providerWindow, ok bool) {
	if len(s.Metadata) == 0 {
		return providerWindow{}, nil, false
	}

	primaryUsed, primaryOK := metadataFloat(s.Metadata, "primary_used_percent")
	primaryWindow, primaryWindowOK := metadataInt(s.Metadata, "primary_window_minutes")
	secondaryUsed, secondaryOK := metadataFloat(s.Metadata, "secondary_used_percent")
	secondaryWindow, secondaryWindowOK := metadataInt(s.Metadata, "secondary_window_minutes")
	if !primaryOK || !primaryWindowOK {
		return providerWindow{}, nil, false
	}

	primaryRemaining := clamp(100-primaryUsed, 0, 100)
	first := providerWindow{label: shortWindowLabel(primaryWindow), percent: primaryRemaining}

	if secondaryOK && secondaryWindowOK {
		second := providerWindow{
			label:   shortWindowLabel(secondaryWindow),
			percent: clamp(100-secondaryUsed, 0, 100),
		}
		return first, &second, true
	}

	return first, nil, true
}

type providerWindow struct {
	label   string
	percent float64
}

func providerColor(s domain.ProviderSnapshot) string {
	if providerConnected(s) {
		return colorAvailable
	}
	return colorUnavailable
}

func providerBrainIcon(s domain.ProviderSnapshot, tmuxColors bool) string {
	icon := brainIcon
	switch s.Provider {
	case domain.ProviderOpenAI:
		icon = "❀"
	case domain.ProviderClaude:
		icon = "✽"
	case domain.ProviderGemini:
		icon = ""
	}
	if !tmuxColors {
		return icon
	}
	return "#[fg=" + providerColor(s) + ",nobold]" + icon + "#[default]"
}

func percentColor(v float64) string {
	p := clamp(v, 0, 100)
	switch {
	case p <= 0:
		return colorUnavailable
	case p < 20:
		return colorPink
	case p < 40:
		return colorMaroon
	case p < 50:
		return colorPeach
	case p < 70:
		return colorYellow
	case p < 80:
		return colorGreen
	default:
		return colorHigh
	}
}

func colorizeText(text, color string) string {
	return "#[fg=" + color + ",nobold]" + text + "#[default]"
}

func colorizePercent(v float64) string {
	text := formatPercent(v)
	if strings.HasSuffix(text, "%") {
		number := strings.TrimSuffix(text, "%")
		return colorizeText(number, percentColor(v)) + colorizeText("%", colorPercentSign)
	}
	return colorizeText(text, percentColor(v))
}

func joinColoredPercents(parts, colors []string) string {
	out := make([]string, 0, len(parts))
	for i, part := range parts {
		if i < len(colors) && strings.TrimSpace(colors[i]) != "" {
			if strings.HasSuffix(part, "%") {
				number := strings.TrimSuffix(part, "%")
				out = append(out, colorizeText(number, colors[i])+colorizeText("%", colorPercentSign))
				continue
			}
			out = append(out, colorizeText(part, colors[i]))
			continue
		}
		out = append(out, colorizeText(part, colorText))
	}
	return strings.Join(out, " ")
}

func providerNameColored(s domain.ProviderSnapshot, tmuxColors bool) string {
	return ""
}

func providerConnected(s domain.ProviderSnapshot) bool {
	return s.RemainingPercent != nil
}

func providerName(id domain.ProviderID) string {
	switch id {
	case domain.ProviderOpenAI:
		return "O"
	case domain.ProviderClaude:
		return "C"
	case domain.ProviderGemini:
		return "G"
	default:
		value := string(id)
		if value == "" {
			return "?"
		}
		return strings.ToUpper(value[:1])
	}
}

func joinNonEmpty(parts ...string) string {
	compact := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		compact = append(compact, part)
	}
	return strings.Join(compact, " ")
}

func supportsNAQuotaDisplay(s domain.ProviderSnapshot) bool {
	reason := strings.ToLower(strings.TrimSpace(s.Reason))
	return strings.Contains(reason, "native remaining quota unavailable")
}

func shouldShowDisconnectedProvider(s domain.ProviderSnapshot) bool {
	if supportsNAQuotaDisplay(s) {
		return true
	}
	if s.Status == domain.StatusError {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(s.Source), "timeout")
}

func formatPercent(v float64) string {
	return fmt.Sprintf("%.0f%%", clamp(v, 0, 100))
}

func claudeCostSummary(s domain.ProviderSnapshot) string {
	total, totalOK := metadataFloat(s.Metadata, "claude_total_cost_usd")
	model, modelOK := metadataFloat(s.Metadata, "claude_model_cost_usd")
	switch {
	case totalOK && modelOK:
		return fmt.Sprintf("%s %s", formatTrailingUSD(total), formatTrailingUSD(model))
	case totalOK:
		return fmt.Sprintf("%s %s", formatTrailingUSD(total), formatTrailingUSD(total))
	case s.UsageUnit == "usd" && s.UsageValue > 0:
		return fmt.Sprintf("%s %s", formatTrailingUSD(s.UsageValue), formatTrailingUSD(s.UsageValue))
	default:
		return ""
	}
}

func formatTrailingUSD(v float64) string {
	return fmt.Sprintf("%.2f$", v)
}

func shortWindowLabel(minutes int) string {
	switch minutes {
	case 300:
		return "5"
	case 10080:
		return "W"
	}
	if minutes <= 0 {
		return "?"
	}
	if minutes%1440 == 0 {
		return fmt.Sprintf("%dd", minutes/1440)
	}
	if minutes%60 == 0 {
		return fmt.Sprintf("%dh", minutes/60)
	}
	return fmt.Sprintf("%dm", minutes)
}

func RenderJSON(col domain.Collection) string {
	return "" // handled by caller through marshaling
}

func metadataFloat(metadata map[string]any, key string) (float64, bool) {
	v, ok := metadata[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	default:
		return 0, false
	}
}

func metadataInt(metadata map[string]any, key string) (int, bool) {
	v, ok := metadataFloat(metadata, key)
	if !ok {
		return 0, false
	}
	return int(v), true
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func Freshness(generatedAt time.Time) string {
	delta := time.Since(generatedAt)
	if delta < time.Minute {
		return "fresh"
	}
	return fmt.Sprintf("%dm old", int(delta.Minutes()))
}
