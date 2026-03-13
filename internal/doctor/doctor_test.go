package doctor

import (
	"testing"

	"github.com/routerr/aubar/internal/config"
)

func TestRunIncludesBasicChecks(t *testing.T) {
	report := Run(nil, config.DefaultSettings())
	names := map[string]bool{}
	for _, c := range report.Checks {
		names[c.Name] = true
	}
	for _, name := range []string{
		"bin.tmux",
		"bin.codex",
		"bin.claude",
		"bin.gemini",
		"keyring.openai",
		"keyring.claude",
		"keyring.gemini",
		"settings.path",
		"cache.status_file",
	} {
		if !names[name] {
			t.Fatalf("missing doctor check %s", name)
		}
	}
}
