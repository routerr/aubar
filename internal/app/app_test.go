package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/raychang/ai-usage-bar/internal/config"
	"github.com/raychang/ai-usage-bar/internal/auth"
	"github.com/raychang/ai-usage-bar/internal/domain"
)

func TestOnceWritesCaches(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	statusFile := filepath.Join(dir, "status.txt")
	jsonFile := filepath.Join(dir, "snapshot.json")

	cfg := config.DefaultSettings()
	cfg.Tmux.StatusFile = statusFile
	cfg.Tmux.JSONCacheFile = jsonFile
	for name, p := range cfg.Providers {
		p.Enabled = false
		cfg.Providers[name] = p
	}
	if err := cfg.Save(settingsPath); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"once", "--settings", settingsPath})
	if code != 0 {
		t.Fatalf("expected code 0, got %d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(statusFile); err != nil {
		t.Fatalf("status file missing: %v", err)
	}
	if _, err := os.Stat(jsonFile); err != nil {
		t.Fatalf("json cache file missing: %v", err)
	}
	if strings.Contains(stdout.String(), "#[fg=") {
		t.Fatalf("expected plain stdout, got %q", stdout.String())
	}
}

func TestShowUsesCachedSnapshot(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	jsonFile := filepath.Join(dir, "snapshot.json")
	statusFile := filepath.Join(dir, "status.txt")

	cfg := config.DefaultSettings()
	cfg.Tmux.JSONCacheFile = jsonFile
	cfg.Tmux.StatusFile = statusFile
	for name, p := range cfg.Providers {
		p.Enabled = false
		cfg.Providers[name] = p
	}
	if err := cfg.Save(settingsPath); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	raw := `{"generated_at":"2026-03-08T01:00:00Z","snapshots":[{"provider":"openai","status":"ok","remaining_percent":70},{"provider":"claude","status":"degraded","reason":"n/a","source":"api","observed_at":"2026-03-08T01:00:00Z"}]}`
	if err := os.WriteFile(jsonFile, []byte(raw), 0o600); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"show", "--settings", settingsPath})
	if code != 0 {
		t.Fatalf("expected code 0, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "❀ 70%") || strings.Contains(stdout.String(), "#[fg=") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestKeySetRejectsInvalidCredential(t *testing.T) {
	prevValidator := credentialValidator
	prevSetter := keySetter
	credentialValidator = func(_ context.Context, provider, key string) auth.Result {
		return auth.Result{
			Provider: provider,
			Message:  "bad key",
			Help: auth.Help{
				GetKeyURL:    "https://example.com/key",
				Instructions: []string{"Create the right key"},
			},
		}
	}
	keySetter = func(provider, value string) error {
		t.Fatalf("key should not be stored when validation fails")
		return nil
	}
	defer func() {
		credentialValidator = prevValidator
		keySetter = prevSetter
	}()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"key", "set", "openai", "--value", "bad"})
	if code != 1 {
		t.Fatalf("expected code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "credential check failed") || !strings.Contains(stderr.String(), "https://example.com/key") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestKeySetStoresValidatedCredential(t *testing.T) {
	prevValidator := credentialValidator
	prevSetter := keySetter
	called := false
	credentialValidator = func(_ context.Context, provider, key string) auth.Result {
		return auth.Result{
			Provider: provider,
			OK:       true,
			Warning:  "works but quota is N/A",
		}
	}
	keySetter = func(provider, value string) error {
		called = true
		if provider != "gemini" || value != "AIza123" {
			t.Fatalf("unexpected storage request: %s %s", provider, value)
		}
		return nil
	}
	defer func() {
		credentialValidator = prevValidator
		keySetter = prevSetter
	}()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"key", "set", "gemini", "--value", "AIza123"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d stderr=%s", code, stderr.String())
	}
	if !called {
		t.Fatalf("expected key setter to be called")
	}
	if !strings.Contains(stdout.String(), "stored key") || !strings.Contains(stdout.String(), "N/A") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunDetachesByDefault(t *testing.T) {
	dir := t.TempDir()
	isolateCacheEnv(t, dir)
	settingsPath := filepath.Join(dir, "settings.json")
	cfg := config.DefaultSettings()
	for name, p := range cfg.Providers {
		p.Enabled = false
		cfg.Providers[name] = p
	}
	if err := cfg.Save(settingsPath); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	called := false
	prev := detachedStarter
	detachedStarter = func(_ *App, opts runOptions) error {
		called = true
		if opts.Foreground {
			t.Fatalf("expected detached mode by default")
		}
		return nil
	}
	defer func() { detachedStarter = prev }()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"run", "--settings", settingsPath})
	if code != 0 {
		t.Fatalf("expected code 0, got %d stderr=%s", code, stderr.String())
	}
	if !called {
		t.Fatalf("expected detached starter to be called")
	}
	if !strings.Contains(stdout.String(), "background") {
		t.Fatalf("expected background message, got %q", stdout.String())
	}
}

func TestRestartDetachesByDefault(t *testing.T) {
	dir := t.TempDir()
	isolateCacheEnv(t, dir)
	settingsPath := filepath.Join(dir, "settings.json")
	cfg := config.DefaultSettings()
	for name, p := range cfg.Providers {
		p.Enabled = false
		cfg.Providers[name] = p
	}
	if err := cfg.Save(settingsPath); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	tmpPID := filepath.Join(os.TempDir(), config.AppName, "aubar.pid")
	if err := os.MkdirAll(filepath.Dir(tmpPID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(tmpPID, []byte("6161\n"), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(tmpPID) })

	var signaled int
	called := false
	prevSignaler := processSignaler
	prevStarter := detachedStarter
	processSignaler = func(pid int) error {
		signaled = pid
		return nil
	}
	detachedStarter = func(_ *App, opts runOptions) error {
		called = true
		if opts.Foreground {
			t.Fatalf("expected detached restart by default")
		}
		return nil
	}
	defer func() {
		processSignaler = prevSignaler
		detachedStarter = prevStarter
	}()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"restart", "--settings", settingsPath})
	if code != 0 {
		t.Fatalf("expected code 0, got %d stderr=%s", code, stderr.String())
	}
	if signaled != 6161 {
		t.Fatalf("expected pid 6161, got %d", signaled)
	}
	if !called {
		t.Fatalf("expected detached starter to run")
	}
	if !strings.Contains(stdout.String(), "restarted") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestStopUsesPIDFile(t *testing.T) {
	dir := t.TempDir()
	isolateCacheEnv(t, dir)
	tmpPID := filepath.Join(config.DefaultCacheDir(), "aubar.pid")
	if err := os.MkdirAll(filepath.Dir(tmpPID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(tmpPID, []byte("4242\n"), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(tmpPID) })

	var signaled int
	prevSignaler := processSignaler
	prevPattern := patternStopper
	processSignaler = func(pid int) error {
		signaled = pid
		return nil
	}
	patternStopper = func() (bool, error) {
		t.Fatalf("pattern fallback should not be used when pid file exists")
		return false, nil
	}
	defer func() {
		processSignaler = prevSignaler
		patternStopper = prevPattern
	}()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"stop"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d stderr=%s", code, stderr.String())
	}
	if signaled != 4242 {
		t.Fatalf("expected pid 4242, got %d", signaled)
	}
	if !strings.Contains(stdout.String(), "4242") {
		t.Fatalf("expected pid in output, got %q", stdout.String())
	}
}

func TestStopFallsBackWhenNoPIDFile(t *testing.T) {
	dir := t.TempDir()
	isolateCacheEnv(t, dir)
	for _, path := range pidFilePaths() {
		_ = os.Remove(path)
	}

	prevSignaler := processSignaler
	prevPattern := patternStopper
	processSignaler = func(pid int) error {
		t.Fatalf("signaler should not be used when pid file is absent")
		return nil
	}
	patternCalled := false
	patternStopper = func() (bool, error) {
		patternCalled = true
		return false, nil
	}
	defer func() {
		processSignaler = prevSignaler
		patternStopper = prevPattern
	}()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"stop"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d stderr=%s", code, stderr.String())
	}
	if !patternCalled {
		t.Fatalf("expected pattern fallback to run")
	}
	if !strings.Contains(stdout.String(), "no running") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestStopTreatsFinishedProcessAsStalePID(t *testing.T) {
	dir := t.TempDir()
	isolateCacheEnv(t, dir)
	tmpPID := filepath.Join(config.DefaultCacheDir(), "aubar.pid")
	if err := os.MkdirAll(filepath.Dir(tmpPID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(tmpPID, []byte("6262\n"), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}

	prevSignaler := processSignaler
	prevPattern := patternStopper
	processSignaler = func(pid int) error {
		if pid != 6262 {
			t.Fatalf("unexpected pid %d", pid)
		}
		return os.ErrProcessDone
	}
	patternStopper = func() (bool, error) {
		t.Fatalf("pattern fallback should not run for a stale pid file")
		return false, nil
	}
	defer func() {
		processSignaler = prevSignaler
		patternStopper = prevPattern
	}()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"stop"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no running") {
		t.Fatalf("expected stale pid to be treated as already stopped, got %q", stdout.String())
	}
	if _, err := os.Stat(tmpPID); !os.IsNotExist(err) {
		t.Fatalf("expected stale pid file to be removed, stat err=%v", err)
	}
}

func TestStatusRunningFromPIDFile(t *testing.T) {
	dir := t.TempDir()
	isolateCacheEnv(t, dir)
	tmpPID := filepath.Join(config.DefaultCacheDir(), "aubar.pid")
	tmpStatus := filepath.Join(config.DefaultCacheDir(), "status.txt")
	if err := os.MkdirAll(filepath.Dir(tmpPID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(tmpPID, []byte("5151\n"), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	if err := os.WriteFile(tmpStatus, []byte("AUBAR"), 0o600); err != nil {
		t.Fatalf("write status: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(tmpPID)
		_ = os.Remove(tmpStatus)
	})

	prevChecker := processChecker
	processChecker = func(pid int) (bool, error) {
		if pid != 5151 {
			t.Fatalf("unexpected pid %d", pid)
		}
		return true, nil
	}
	defer func() { processChecker = prevChecker }()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"status"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "running") || !strings.Contains(stdout.String(), "5151") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestStatusTreatsFinishedProcessAsStale(t *testing.T) {
	dir := t.TempDir()
	isolateCacheEnv(t, dir)
	tmpPID := filepath.Join(config.DefaultCacheDir(), "aubar.pid")
	if err := os.MkdirAll(filepath.Dir(tmpPID), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(tmpPID, []byte("7272\n"), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(tmpPID) })

	prevChecker := processChecker
	processChecker = func(pid int) (bool, error) {
		if pid != 7272 {
			t.Fatalf("unexpected pid %d", pid)
		}
		return false, os.ErrProcessDone
	}
	defer func() { processChecker = prevChecker }()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"status"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "stale pid 7272") {
		t.Fatalf("expected stale status, got %q", stdout.String())
	}
}

func TestStatusStoppedWithoutPIDFile(t *testing.T) {
	dir := t.TempDir()
	isolateCacheEnv(t, dir)
	for _, path := range pidFilePaths() {
		_ = os.Remove(path)
	}
	prevChecker := processChecker
	processChecker = func(pid int) (bool, error) {
		t.Fatalf("checker should not be called without pid file")
		return false, nil
	}
	defer func() { processChecker = prevChecker }()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"status"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "stopped") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestStatusUsesConfiguredStatusFilePath(t *testing.T) {
	dir := t.TempDir()
	isolateCacheEnv(t, dir)
	settingsPath := filepath.Join(dir, "settings.json")
	customStatus := filepath.Join(dir, "custom", "status.txt")

	cfg := config.DefaultSettings()
	cfg.Tmux.StatusFile = customStatus
	if err := cfg.Save(settingsPath); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(customStatus), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(customStatus, []byte("AUBAR"), 0o600); err != nil {
		t.Fatalf("write status: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"status", "--json", "--settings", settingsPath})
	if code != 0 {
		t.Fatalf("expected code 0, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), customStatus) {
		t.Fatalf("expected configured status file in output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "\"status_file_seen\": true") {
		t.Fatalf("expected status file to be detected, got %q", stdout.String())
	}
}

func TestOncePrintsDiagnostics(t *testing.T) {
	dir := t.TempDir()
	isolateCacheEnv(t, dir)
	settingsPath := filepath.Join(dir, "settings.json")
	cfg := config.DefaultSettings()
	for name, p := range cfg.Providers {
		p.Enabled = false
		cfg.Providers[name] = p
	}
	cfg.Providers["gemini"] = config.ProviderSetting{
		Enabled:            true,
		SourceOrder:        []string{"cli"},
		TimeoutSeconds:     1,
		MinIntervalSeconds: 30,
		CredentialRef:      "provider/gemini",
	}
	if err := cfg.Save(settingsPath); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	t.Setenv("GEMINI_API_KEY", "")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	a := &App{Stdout: stdout, Stderr: stderr}
	code := a.Run([]string{"once", "--settings", settingsPath})
	if code != 0 {
		t.Fatalf("expected code 0, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "diagnosis: Gemini: CLI execution failed") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestTmuxInstructions(t *testing.T) {
	cfg := config.DefaultSettings()
	cfg.Tmux.Position = "top"
	cfg.Tmux.StatusFile = "/tmp/aubar/status.txt"

	out := tmuxInstructions(cfg)
	if !strings.Contains(out, "status-position top") {
		t.Fatalf("missing position: %q", out)
	}
	if !strings.Contains(out, "cat \"/tmp/aubar/status.txt\"") {
		t.Fatalf("missing status file path: %q", out)
	}
	if !strings.Contains(out, "echo \"○ booting\"") {
		t.Fatalf("missing boot fallback: %q", out)
	}
	if !strings.Contains(out, "aubar run") {
		t.Fatalf("missing run instruction: %q", out)
	}
}

func TestStabilizeCollectionReusesRecentClaudeQuotaSnapshot(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultSettings()
	cfg.Tmux.JSONCacheFile = filepath.Join(dir, "snapshot.json")

	remaining := 77.0
	prevObservedAt := time.Date(2026, 3, 8, 16, 0, 0, 0, time.UTC)
	previous := domain.Collection{
		GeneratedAt: prevObservedAt,
		Snapshots: []domain.ProviderSnapshot{
			{
				Provider:         domain.ProviderClaude,
				Status:           domain.StatusOK,
				Source:           "claude-quota",
				RemainingPercent: &remaining,
				ObservedAt:       prevObservedAt,
				Metadata: map[string]any{
					"primary_used_percent":   23.0,
					"secondary_used_percent": 15.0,
				},
			},
		},
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Tmux.JSONCacheFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeCaches(cfg, previous); err != nil {
		t.Fatalf("write cached collection: %v", err)
	}

	current := domain.Collection{
		GeneratedAt: prevObservedAt.Add(10 * time.Minute),
		Snapshots: []domain.ProviderSnapshot{
			{
				Provider:   domain.ProviderClaude,
				Status:     domain.StatusDegraded,
				Source:     "cli",
				Reason:     "native remaining quota unavailable",
				ObservedAt: prevObservedAt.Add(10 * time.Minute),
			},
		},
	}

	stable := stabilizeCollection(cfg, current)
	if stable.Snapshots[0].Source != "claude-quota" {
		t.Fatalf("expected cached claude quota snapshot, got %+v", stable.Snapshots[0])
	}
	if stable.Snapshots[0].RemainingPercent == nil || *stable.Snapshots[0].RemainingPercent != 77 {
		t.Fatalf("expected cached remaining percent, got %+v", stable.Snapshots[0])
	}
	if stable.Snapshots[0].Metadata["cached"] != true {
		t.Fatalf("expected cached metadata marker, got %+v", stable.Snapshots[0].Metadata)
	}
}

func TestStabilizeCollectionDoesNotReuseStaleClaudeQuotaSnapshot(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultSettings()
	cfg.Tmux.JSONCacheFile = filepath.Join(dir, "snapshot.json")

	remaining := 77.0
	prevObservedAt := time.Date(2026, 3, 8, 16, 0, 0, 0, time.UTC)
	previous := domain.Collection{
		GeneratedAt: prevObservedAt,
		Snapshots: []domain.ProviderSnapshot{
			{
				Provider:         domain.ProviderClaude,
				Status:           domain.StatusOK,
				Source:           "claude-quota",
				RemainingPercent: &remaining,
				ObservedAt:       prevObservedAt,
				Metadata: map[string]any{
					"primary_used_percent":   23.0,
					"secondary_used_percent": 15.0,
				},
			},
		},
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Tmux.JSONCacheFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeCaches(cfg, previous); err != nil {
		t.Fatalf("write cached collection: %v", err)
	}

	current := domain.Collection{
		GeneratedAt: prevObservedAt.Add(maxCachedClaudeQuotaAge + time.Minute),
		Snapshots: []domain.ProviderSnapshot{
			{
				Provider:   domain.ProviderClaude,
				Status:     domain.StatusDegraded,
				Source:     "cli",
				Reason:     "native remaining quota unavailable",
				ObservedAt: prevObservedAt.Add(maxCachedClaudeQuotaAge + time.Minute),
			},
		},
	}

	stable := stabilizeCollection(cfg, current)
	if stable.Snapshots[0].Source != "cli" {
		t.Fatalf("expected stale cached quota to be ignored, got %+v", stable.Snapshots[0])
	}
}

func isolateCacheEnv(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, ".cache"))
}
