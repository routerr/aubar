package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/raychang/ai-usage-bar/internal/cache"
	"github.com/raychang/ai-usage-bar/internal/config"
	"github.com/raychang/ai-usage-bar/internal/credentials"
	"github.com/raychang/ai-usage-bar/internal/diagnose"
	"github.com/raychang/ai-usage-bar/internal/doctor"
	"github.com/raychang/ai-usage-bar/internal/domain"
	"github.com/raychang/ai-usage-bar/internal/envload"
	"github.com/raychang/ai-usage-bar/internal/keyringx"
	"github.com/raychang/ai-usage-bar/internal/provider"
	"github.com/raychang/ai-usage-bar/internal/render"
	"github.com/raychang/ai-usage-bar/internal/runner"
	"github.com/raychang/ai-usage-bar/internal/tui"
)

type App struct {
	Stdout io.Writer
	Stderr io.Writer
}

var detachedStarter = (*App).spawnDetached
var processSignaler = signalProcess
var patternStopper = stopByPattern
var processChecker = isProcessRunning
var credentialValidator = credentials.ValidateCredential
var keySetter = keyringx.Set

type runOptions struct {
	SettingsPath string
	Foreground   bool
	Print        bool
	JSON         bool
}

type statusReport struct {
	State          string `json:"state"`
	PID            *int   `json:"pid,omitempty"`
	PIDFile        string `json:"pid_file,omitempty"`
	StatusFile     string `json:"status_file"`
	StatusFileAge  string `json:"status_file_age,omitempty"`
	StatusFileSeen bool   `json:"status_file_seen"`
	Message        string `json:"message"`
}

func New() *App {
	return &App{Stdout: os.Stdout, Stderr: os.Stderr}
}

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.help()
		return 0
	}
	switch args[0] {
	case "help", "-h", "--help":
		a.help()
		return 0
	case "once":
		return a.cmdOnce(args[1:])
	case "run":
		return a.cmdRun(args[1:])
	case "doctor":
		return a.cmdDoctor(args[1:])
	case "show":
		return a.cmdShow(args[1:])
	case "status":
		return a.cmdStatus(args[1:])
	case "restart":
		return a.cmdRestart(args[1:])
	case "stop":
		return a.cmdStop(args[1:])
	case "key":
		return a.cmdKey(args[1:])
	case "setup":
		return a.cmdSetup(args[1:])
	case "tui":
		return a.cmdSetup(args[1:])
	case "tmux":
		a.printTmuxSnippet()
		return 0
	default:
		fmt.Fprintf(a.Stderr, "unknown command: %s\n", args[0])
		a.help()
		return 2
	}
}

func (a *App) help() {
	fmt.Fprint(a.Stdout, `aubar - AI usage banner

Usage:
  aubar setup
  aubar run [--foreground] [--print] [--json] [--settings PATH]
  aubar once [--json] [--settings PATH]
  aubar doctor [--json] [--settings PATH]
  aubar show [--settings PATH]
  aubar status [--json] [--settings PATH]
  aubar restart [--foreground] [--print] [--json] [--settings PATH]
  aubar stop
  aubar key set <openai|claude|gemini> [--value SECRET]
  aubar key delete <openai|claude|gemini>
  aubar tui  # alias for setup
  aubar tmux
`)
}

func (a *App) cmdOnce(args []string) int {
	fs := flag.NewFlagSet("once", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	jsonOut := fs.Bool("json", false, "output JSON")
	settingsPath := fs.String("settings", config.DefaultSettingsPath(), "settings file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig(*settingsPath)
	if err != nil {
		fmt.Fprintf(a.Stderr, "load config: %v\n", err)
		return 1
	}
	col := collectOnce(cfg)
	if *jsonOut {
		b, _ := json.MarshalIndent(col, "", "  ")
		fmt.Fprintln(a.Stdout, string(b))
	} else {
		fmt.Fprintln(a.Stdout, render.RenderLine(col, false))
	}
	if err := writeCaches(cfg, col); err != nil {
		fmt.Fprintf(a.Stderr, "cache write warning: %v\n", err)
	}
	a.printDiagnostics(col)
	return 0
}

func (a *App) cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	jsonOut := fs.Bool("json", false, "stream JSON lines")
	foreground := fs.Bool("foreground", false, "run in the current terminal")
	printOut := fs.Bool("print", false, "print banner lines while running in foreground")
	settingsPath := fs.String("settings", config.DefaultSettingsPath(), "settings file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	opts := runOptions{
		SettingsPath: *settingsPath,
		Foreground:   *foreground,
		Print:        *printOut,
		JSON:         *jsonOut,
	}
	if opts.JSON {
		opts.Foreground = true
		opts.Print = true
	}
	cfg, err := loadConfig(*settingsPath)
	if err != nil {
		fmt.Fprintf(a.Stderr, "load config: %v\n", err)
		return 1
	}
	if !opts.Foreground {
		if err := detachedStarter(a, opts); err != nil {
			fmt.Fprintf(a.Stderr, "start background updater: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.Stdout, "aubar updater started in background\n")
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	r := runner.NewService(cfg, provider.BuildProviders(cfg, provider.DefaultCLIExecutor{}))
	firstTick := true
	r.Run(ctx, func(col domain.Collection) {
		col = stabilizeCollection(cfg, col)
		if opts.JSON {
			b, _ := json.Marshal(col)
			fmt.Fprintln(a.Stdout, string(b))
		} else if opts.Print {
			fmt.Fprintln(a.Stdout, render.RenderLine(col, false))
		}
		if err := writeCaches(cfg, col); err != nil {
			fmt.Fprintf(a.Stderr, "cache write warning: %v\n", err)
		}
		if firstTick {
			a.printDiagnostics(col)
			firstTick = false
		}
	})
	return 0
}

func (a *App) cmdDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	jsonOut := fs.Bool("json", false, "output JSON")
	settingsPath := fs.String("settings", config.DefaultSettingsPath(), "settings file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig(*settingsPath)
	if err != nil {
		fmt.Fprintf(a.Stderr, "load config: %v\n", err)
		return 1
	}
	report := doctor.Run(context.Background(), cfg)
	if *jsonOut {
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Fprintln(a.Stdout, string(b))
		return 0
	}
	fmt.Fprintf(a.Stdout, "Doctor %s\n", doctor.Timestamp())
	fmt.Fprintln(a.Stdout, "Legend: standalone Aubar banners append a timestamp; the tmux cache line uses the same provider layout without the clock.")
	for _, c := range report.Checks {
		fmt.Fprintf(a.Stdout, "- %-20s %-5s %s\n", c.Name, strings.ToUpper(c.Status), c.Detail)
	}
	return 0
}

func (a *App) cmdShow(args []string) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	settingsPath := fs.String("settings", config.DefaultSettingsPath(), "settings file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := loadConfig(*settingsPath)
	if err != nil {
		fmt.Fprintf(a.Stderr, "load config: %v\n", err)
		return 1
	}
	col, err := readCachedCollection(cfg)
	if err != nil {
		fmt.Fprintf(a.Stderr, "read cached data: %v\n", err)
		return 1
	}
	fmt.Fprintln(a.Stdout, render.RenderLine(col, false))
	return 0
}

func (a *App) cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	jsonOut := fs.Bool("json", false, "output JSON")
	settingsPath := fs.String("settings", config.DefaultSettingsPath(), "settings file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	report := buildStatusReport(*settingsPath)
	if *jsonOut {
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Fprintln(a.Stdout, string(b))
		return 0
	}

	switch report.State {
	case "running":
		fmt.Fprintf(a.Stdout, "aubar updater is running (pid %d)", *report.PID)
	case "stale":
		fmt.Fprintf(a.Stdout, "aubar updater is not running (stale pid %d)", *report.PID)
	default:
		fmt.Fprint(a.Stdout, "aubar updater is stopped")
	}
	if report.StatusFileSeen && report.StatusFileAge != "" {
		fmt.Fprintf(a.Stdout, "; cache updated %s ago", report.StatusFileAge)
	}
	fmt.Fprintln(a.Stdout)
	return 0
}

func (a *App) cmdRestart(args []string) int {
	fs := flag.NewFlagSet("restart", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	jsonOut := fs.Bool("json", false, "stream JSON lines")
	foreground := fs.Bool("foreground", false, "run in the current terminal")
	printOut := fs.Bool("print", false, "print banner lines while running in foreground")
	settingsPath := fs.String("settings", config.DefaultSettingsPath(), "settings file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	opts := runOptions{
		SettingsPath: *settingsPath,
		Foreground:   *foreground,
		Print:        *printOut,
		JSON:         *jsonOut,
	}
	if opts.JSON {
		opts.Foreground = true
		opts.Print = true
	}

	stopped, _, err := stopUpdater()
	if err != nil {
		fmt.Fprintf(a.Stderr, "restart updater: %v\n", err)
		return 1
	}

	cfg, err := loadConfig(*settingsPath)
	if err != nil {
		fmt.Fprintf(a.Stderr, "load config: %v\n", err)
		return 1
	}
	if !opts.Foreground {
		if err := detachedStarter(a, opts); err != nil {
			fmt.Fprintf(a.Stderr, "restart updater: %v\n", err)
			return 1
		}
		if stopped {
			fmt.Fprintln(a.Stdout, "aubar updater restarted in background")
		} else {
			fmt.Fprintln(a.Stdout, "aubar updater started in background")
		}
		return 0
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	r := runner.NewService(cfg, provider.BuildProviders(cfg, provider.DefaultCLIExecutor{}))
	r.Run(ctx, func(col domain.Collection) {
		if opts.JSON {
			b, _ := json.Marshal(col)
			fmt.Fprintln(a.Stdout, string(b))
		} else if opts.Print {
			fmt.Fprintln(a.Stdout, render.RenderLine(col, false))
		}
		if err := writeCaches(cfg, col); err != nil {
			fmt.Fprintf(a.Stderr, "cache write warning: %v\n", err)
		}
	})
	return 0
}

func (a *App) cmdStop(args []string) int {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	stopped, pid, err := stopUpdater()
	if err != nil {
		fmt.Fprintf(a.Stderr, "stop updater: %v\n", err)
		return 1
	}
	if !stopped {
		fmt.Fprintln(a.Stdout, "no running aubar updater found")
		return 0
	}
	if pid != nil {
		fmt.Fprintf(a.Stdout, "stopped aubar updater (pid %d)\n", *pid)
		return 0
	}
	fmt.Fprintln(a.Stdout, "stopped aubar updater")
	return 0
}

func (a *App) cmdKey(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(a.Stderr, "usage: aubar key set|delete <provider>")
		return 2
	}
	action := args[0]
	providerName := strings.ToLower(strings.TrimSpace(args[1]))
	if providerName != "openai" && providerName != "claude" && providerName != "gemini" {
		fmt.Fprintln(a.Stderr, "provider must be one of: openai, claude, gemini")
		return 2
	}
	switch action {
	case "set":
		fs := flag.NewFlagSet("key set", flag.ContinueOnError)
		fs.SetOutput(a.Stderr)
		value := fs.String("value", "", "secret value")
		if err := fs.Parse(args[2:]); err != nil {
			return 2
		}
		secret := strings.TrimSpace(*value)
		if secret == "" {
			fmt.Fprintf(a.Stdout, "Enter secret for %s: ", providerName)
			reader := bufio.NewReader(os.Stdin)
			raw, err := reader.ReadString('\n')
			if err != nil {
				fmt.Fprintf(a.Stderr, "read secret: %v\n", err)
				return 1
			}
			secret = strings.TrimSpace(raw)
		}
		if secret == "" {
			fmt.Fprintln(a.Stderr, "empty secret")
			return 1
		}
		check := credentialValidator(context.Background(), providerName, secret)
		if !check.OK {
			fmt.Fprintf(a.Stderr, "credential check failed for %s: %s\n", providerName, check.Message)
			for _, line := range helpLines(check.Help) {
				fmt.Fprintln(a.Stderr, line)
			}
			return 1
		}
		if err := keySetter(providerName, secret); err != nil {
			fmt.Fprintf(a.Stderr, "store key: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.Stdout, "stored key for %s in keyring\n", providerName)
		if check.Warning != "" {
			fmt.Fprintf(a.Stdout, "note: %s\n", check.Warning)
		}
		return 0
	case "delete":
		if err := keyringx.Delete(providerName); err != nil {
			fmt.Fprintf(a.Stderr, "delete key: %v\n", err)
			return 1
		}
		fmt.Fprintf(a.Stdout, "deleted key for %s\n", providerName)
		return 0
	default:
		fmt.Fprintln(a.Stderr, "usage: aubar key set|delete <provider>")
		return 2
	}
}

func (a *App) cmdSetup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	settingsPath := fs.String("settings", config.DefaultSettingsPath(), "settings file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := loadConfig(*settingsPath)
	if err != nil {
		fmt.Fprintf(a.Stderr, "load config: %v\n", err)
		return 1
	}
	updated, err := tui.Run(cfg, *settingsPath)
	if err != nil {
		if errors.Is(err, tui.ErrAborted) {
			return 0
		}
		fmt.Fprintf(a.Stderr, "tui error: %v\n", err)
		return 1
	}
	if err := updated.Save(*settingsPath); err != nil {
		fmt.Fprintf(a.Stderr, "save settings: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Stdout, "saved settings to %s\n", *settingsPath)
	if updated.Tmux.Enabled {
		fmt.Fprintln(a.Stdout, tmuxInstructions(updated))
	}
	return 0
}

func loadConfig(path string) (config.Settings, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return config.Settings{}, err
	}
	_ = envload.Load(envload.DefaultEnvPaths(filepath.Dir(path))...)
	return cfg, nil
}

func collectOnce(cfg config.Settings) domain.Collection {
	r := runner.NewService(cfg, provider.BuildProviders(cfg, provider.DefaultCLIExecutor{}))
	return stabilizeCollection(cfg, r.Collect(context.Background()))
}

func writeCaches(cfg config.Settings, col domain.Collection) error {
	line := render.RenderLineWithoutTimestamp(col, cfg.Tmux.UseTmuxColorFormat)
	if err := cache.WriteAtomic(cfg.Tmux.StatusFile, []byte(line), 0o600); err != nil {
		if !errors.Is(err, os.ErrPermission) {
			return err
		}
		fallbackDir := filepath.Join(os.TempDir(), config.AppName)
		if e2 := cache.WriteAtomic(filepath.Join(fallbackDir, "status.txt"), []byte(line), 0o600); e2 != nil {
			return errors.Join(err, e2)
		}
	}
	if err := cache.WriteJSON(cfg.Tmux.JSONCacheFile, col); err != nil {
		if !errors.Is(err, os.ErrPermission) {
			return err
		}
		fallbackDir := filepath.Join(os.TempDir(), config.AppName)
		if e2 := cache.WriteJSON(filepath.Join(fallbackDir, "snapshot.json"), col); e2 != nil {
			return errors.Join(err, e2)
		}
	}
	return nil
}

const maxCachedClaudeQuotaAge = 30 * time.Minute

func stabilizeCollection(cfg config.Settings, col domain.Collection) domain.Collection {
	prev, err := readCachedCollection(cfg)
	if err != nil {
		return col
	}

	prevByProvider := make(map[domain.ProviderID]domain.ProviderSnapshot, len(prev.Snapshots))
	for _, snap := range prev.Snapshots {
		prevByProvider[snap.Provider] = snap
	}

	stable := col
	stable.Snapshots = make([]domain.ProviderSnapshot, len(col.Snapshots))
	copy(stable.Snapshots, col.Snapshots)
	for i, current := range stable.Snapshots {
		previous, ok := prevByProvider[current.Provider]
		if !ok || !shouldReuseCachedClaudeQuota(previous, current, col.GeneratedAt) {
			continue
		}
		preserved := previous
		preserved.Metadata = mergeMetadataCopy(previous.Metadata, map[string]any{
			"cached":                  true,
			"cached_preserved_at":     col.GeneratedAt.Format(time.RFC3339),
			"cached_preserved_reason": "subscription quota temporarily unavailable",
		})
		preserved.NextAllowedRefresh = current.NextAllowedRefresh
		stable.Snapshots[i] = preserved
	}

	return stable
}

func shouldReuseCachedClaudeQuota(previous, current domain.ProviderSnapshot, now time.Time) bool {
	if current.Provider != domain.ProviderClaude || current.RemainingPercent != nil {
		return false
	}
	// Note: We reuse cached quota if the current attempt (CLI) failed or didn't return quota,
	// and we have a recently cached 'claude-quota' snapshot.
	if previous.Provider != domain.ProviderClaude || previous.RemainingPercent == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(previous.Source), "claude-quota") {
		return false
	}
	if previous.ObservedAt.IsZero() || now.Sub(previous.ObservedAt) > maxCachedClaudeQuotaAge {
		return false
	}
	_, primaryOK := previous.Metadata["primary_used_percent"]
	_, secondaryOK := previous.Metadata["secondary_used_percent"]
	return primaryOK && secondaryOK
}

func mergeMetadataCopy(base map[string]any, extras map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(extras))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range extras {
		merged[k] = v
	}
	return merged
}

func readCachedCollection(cfg config.Settings) (domain.Collection, error) {
	var lastErr error
	for _, path := range jsonCachePaths(cfg) {
		raw, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			continue
		}
		var col domain.Collection
		if err := json.Unmarshal(raw, &col); err != nil {
			lastErr = err
			continue
		}
		return col, nil
	}
	if lastErr == nil {
		lastErr = os.ErrNotExist
	}
	return domain.Collection{}, lastErr
}

func (a *App) printDiagnostics(col domain.Collection) {
	for _, line := range diagnose.Collection(col) {
		fmt.Fprintf(a.Stderr, "diagnosis: %s\n", line)
	}
}

func (a *App) printTmuxSnippet() {
	fmt.Fprintln(a.Stdout, "# tmux snippet for the default Aubar cache line")
	fmt.Fprintln(a.Stdout, "set -g status-position top")
	fmt.Fprintf(a.Stdout, "set -g status-right '#(cat %q 2>/dev/null || echo \"○ booting\")'\n", filepath.Join(config.DefaultCacheDir(), "status.txt"))
	fmt.Fprintln(a.Stdout, "# start updater once per login shell: aubar run")
}

func (a *App) spawnDetached(opts runOptions) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logFile, err := openBackgroundLog()
	if err != nil {
		return err
	}
	args := []string{"run", "--foreground", "--settings", opts.SettingsPath}
	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	_ = writePID(cmd.Process.Pid)
	_ = logFile.Close()
	return nil
}

func backgroundLogPath() string {
	return filepath.Join(runtimeCacheDir(), "aubar.log")
}

func writePID(pid int) error {
	var lastErr error
	for _, path := range pidFilePaths() {
		if err := cache.WriteAtomic(path, []byte(fmt.Sprintf("%d\n", pid)), 0o600); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

func runtimeCacheDir() string {
	for _, dir := range runtimeCacheDirs() {
		if err := os.MkdirAll(dir, 0o755); err == nil {
			return dir
		}
	}
	return filepath.Join(os.TempDir(), config.AppName)
}

func runtimeCacheDirs() []string {
	return []string{
		config.DefaultCacheDir(),
		filepath.Join(os.TempDir(), config.AppName),
	}
}

func jsonCachePaths(cfg config.Settings) []string {
	paths := []string{}
	if strings.TrimSpace(cfg.Tmux.JSONCacheFile) != "" {
		paths = append(paths, cfg.Tmux.JSONCacheFile)
	}
	fallback := filepath.Join(os.TempDir(), config.AppName, "snapshot.json")
	if len(paths) == 0 || paths[0] != fallback {
		paths = append(paths, fallback)
	}
	return paths
}

func pidFilePaths() []string {
	paths := make([]string, 0, len(runtimeCacheDirs()))
	for _, dir := range runtimeCacheDirs() {
		paths = append(paths, filepath.Join(dir, "aubar.pid"))
	}
	return paths
}

func openBackgroundLog() (*os.File, error) {
	var lastErr error
	for _, dir := range runtimeCacheDirs() {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			lastErr = err
			continue
		}
		path := filepath.Join(dir, "aubar.log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			return f, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func buildStatusReport(settingsPath string) statusReport {
	report := statusReport{
		State:      "stopped",
		StatusFile: firstExistingStatusFile(settingsPath),
		Message:    "no running aubar updater found",
	}
	if age, ok := statusFileAge(report.StatusFile); ok {
		report.StatusFileSeen = true
		report.StatusFileAge = age
	}

	pid, path, err := findPID()
	if err != nil {
		return report
	}

	report.PID = &pid
	report.PIDFile = path
	running, err := processChecker(pid)
	if err != nil {
		if isFinishedProcessError(err) {
			report.State = "stale"
			report.Message = fmt.Sprintf("stale pid file for pid %d", pid)
			return report
		}
		report.State = "unknown"
		report.Message = err.Error()
		return report
	}
	if running {
		report.State = "running"
		report.Message = fmt.Sprintf("running with pid %d", pid)
		return report
	}
	report.State = "stale"
	report.Message = fmt.Sprintf("stale pid file for pid %d", pid)
	return report
}

func firstExistingStatusFile(settingsPath string) string {
	paths := statusFileCandidates(settingsPath)
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return paths[0]
}

func statusFileCandidates(settingsPath string) []string {
	paths := []string{}
	if cfg, err := loadConfig(settingsPath); err == nil && strings.TrimSpace(cfg.Tmux.StatusFile) != "" {
		paths = append(paths, cfg.Tmux.StatusFile)
	}
	defaultPath := filepath.Join(config.DefaultCacheDir(), "status.txt")
	tempPath := filepath.Join(os.TempDir(), config.AppName, "status.txt")
	for _, candidate := range []string{defaultPath, tempPath} {
		if !slices.Contains(paths, candidate) {
			paths = append(paths, candidate)
		}
	}
	return paths
}

func statusFileAge(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	age := time.Since(info.ModTime()).Round(time.Second)
	if age < 0 {
		age = 0
	}
	return age.String(), true
}

func findPID() (int, string, error) {
	var lastErr error
	for _, path := range pidFilePaths() {
		raw, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			continue
		}
		pidText := strings.TrimSpace(string(raw))
		if pidText == "" {
			lastErr = fmt.Errorf("empty pid file: %s", path)
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(pidText, "%d", &pid); err != nil {
			lastErr = err
			continue
		}
		return pid, path, nil
	}
	if lastErr == nil {
		lastErr = os.ErrNotExist
	}
	return 0, "", lastErr
}

func signalProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}

func isFinishedProcessError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(err.Error())), "process already finished")
}

func isProcessRunning(pid int) (bool, error) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true, nil
	}
	if isFinishedProcessError(err) {
		return false, nil
	}
	if errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	return false, err
}

func stopByPattern() (bool, error) {
	cmd := exec.Command("pkill", "-f", "aubar run --foreground")
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	for _, path := range pidFilePaths() {
		_ = os.Remove(path)
	}
	return true, nil
}

func stopUpdater() (bool, *int, error) {
	pid, path, err := findPID()
	if err == nil {
		err = processSignaler(pid)
		if err == nil || isFinishedProcessError(err) {
			_ = os.Remove(path)
			return err == nil, &pid, nil
		}
		return false, nil, err
	}

	stopped, err := patternStopper()
	if err != nil {
		return false, nil, err
	}
	if !stopped {
		return false, nil, nil
	}
	return true, nil, nil
}

func helpLines(help credentials.Help) []string {
	lines := []string{}
	if help.GetKeyURL != "" {
		lines = append(lines, "Get key: "+help.GetKeyURL)
	}
	if help.DocsURL != "" {
		lines = append(lines, "Docs: "+help.DocsURL)
	}
	for _, step := range help.Instructions {
		lines = append(lines, "How: "+step)
	}
	return lines
}

func tmuxInstructions(cfg config.Settings) string {
	return fmt.Sprintf("tmux next steps:\nset -g status-position %s\nset -g status-right '#(cat %q 2>/dev/null || echo \"○ booting\")'\nrun: aubar run",
		cfg.Tmux.Position,
		cfg.Tmux.StatusFile,
	)
}
