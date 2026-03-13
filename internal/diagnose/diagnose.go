package diagnose

import (
	"fmt"
	"strings"

	"github.com/routerr/aubar/internal/auth"
	"github.com/routerr/aubar/internal/domain"
)

func Collection(col domain.Collection) []string {
	lines := []string{}
	for _, snap := range col.Snapshots {
		lines = append(lines, forSnapshot(snap)...)
	}
	return lines
}

func forSnapshot(snap domain.ProviderSnapshot) []string {
	provider := string(snap.Provider)
	help := auth.ProviderHelp(provider)
	reason := strings.ToLower(strings.TrimSpace(snap.Reason))
	lines := []string{}

	if snap.Status == domain.StatusOK {
		return lines
	}
	if strings.Contains(reason, "missing openai_api_key") || strings.Contains(reason, "missing codex_api_key") ||
		strings.Contains(reason, "missing anthropic_api_key") || strings.Contains(reason, "missing claude_api_key") ||
		strings.Contains(reason, "missing gemini_api_key") || strings.Contains(reason, "missing google_api_key") {
		lines = append(lines, fmt.Sprintf("%s: missing credential. Run `aubar key set %s` or export the provider env var.", label(provider), provider))
		lines = append(lines, fmt.Sprintf("%s: get a key at %s", label(provider), help.GetKeyURL))
		return lines
	}
	if strings.Contains(reason, "http 401") || strings.Contains(reason, "rejected") {
		lines = append(lines, fmt.Sprintf("%s: the saved credential was rejected by the provider.", label(provider)))
		lines = append(lines, helpLines(help)...)
		return lines
	}
	if strings.Contains(reason, "http 403") || strings.Contains(reason, "http 404") {
		lines = append(lines, fmt.Sprintf("%s: the provider refused access to the retrieval endpoint.", label(provider)))
		lines = append(lines, helpLines(help)...)
		return lines
	}
	if strings.Contains(reason, "no codex session rate-limit telemetry") {
		lines = append(lines, "OpenAI: Codex session mode could not find local rate-limit telemetry yet.")
		lines = append(lines, "OpenAI: open Codex locally and complete at least one prompt so Aubar can read the latest usage window from ~/.codex/sessions.")
		return lines
	}
	if strings.Contains(reason, "native remaining quota unavailable") {
		lines = append(lines, fmt.Sprintf("%s: usage data is reachable, but the source did not return a native remaining quota value. Aubar will show N/A.", label(provider)))
		return lines
	}
	if strings.Contains(reason, "no such file or directory") || strings.Contains(reason, "command not found") || strings.Contains(reason, "exit status 127") {
		lines = append(lines, fmt.Sprintf("%s: CLI execution failed. Ensure the provider CLI or any explicitly configured override command is installed and in your PATH.", label(provider)))
		return lines
	}
	if reason != "" {
		lines = append(lines, fmt.Sprintf("%s: %s", label(provider), snap.Reason))
	}
	return lines
}

func helpLines(help auth.Help) []string {
	lines := []string{}
	if help.GetKeyURL != "" {
		lines = append(lines, fmt.Sprintf("%s: get/create the key at %s", label(help.Provider), help.GetKeyURL))
	}
	if help.DocsURL != "" {
		lines = append(lines, fmt.Sprintf("%s: docs %s", label(help.Provider), help.DocsURL))
	}
	for _, step := range help.Instructions {
		lines = append(lines, fmt.Sprintf("%s: %s", label(help.Provider), step))
	}
	return lines
}

func label(provider string) string {
	switch provider {
	case "openai":
		return "OpenAI"
	case "claude":
		return "Claude"
	case "gemini":
		return "Gemini"
	default:
		return provider
	}
}
