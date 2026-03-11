package doctor

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/raychang/ai-usage-bar/internal/config"
	"github.com/raychang/ai-usage-bar/internal/keyringx"
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type Report struct {
	Checks []Check `json:"checks"`
}

func Run(_ context.Context, cfg config.Settings) Report {
	checks := []Check{}
	checks = append(checks, checkBinary("tmux"))
	checks = append(checks, checkBinary("codex"))
	checks = append(checks, checkBinary("claude"))
	checks = append(checks, checkBinary("gemini"))
	checks = append(checks, checkKeyring("openai"))
	checks = append(checks, checkKeyring("claude"))
	checks = append(checks, checkKeyring("gemini"))
	checks = append(checks, Check{Name: "settings.path", Status: "ok", Detail: config.DefaultSettingsPath()})
	checks = append(checks, Check{Name: "cache.status_file", Status: "ok", Detail: cfg.Tmux.StatusFile})
	return Report{Checks: checks}
}

func checkBinary(name string) Check {
	path, err := exec.LookPath(name)
	if err != nil {
		return Check{Name: "bin." + name, Status: "warn", Detail: "not found in PATH"}
	}
	return Check{Name: "bin." + name, Status: "ok", Detail: path}
}

func checkKeyring(provider string) Check {
	_, err := keyringx.Get(provider)
	if err != nil {
		detail := strings.TrimSpace(err.Error())
		if detail == "" {
			detail = "missing"
		}
		return Check{Name: "keyring." + provider, Status: "warn", Detail: detail}
	}
	return Check{Name: "keyring." + provider, Status: "ok", Detail: "secret exists"}
}

func Healthy(r Report) bool {
	for _, c := range r.Checks {
		if c.Status == "error" {
			return false
		}
	}
	return true
}

func Timestamp() string {
	return time.Now().Format(time.RFC3339)
}
