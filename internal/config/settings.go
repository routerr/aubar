package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	AppName = "ai-usage-bar"
)

type Settings struct {
	Version   int                        `json:"version"`
	Refresh   RefreshSettings            `json:"refresh"`
	Providers map[string]ProviderSetting `json:"providers"`
	Tmux      TmuxSettings               `json:"tmux"`
	Theme     ThemeSettings              `json:"theme"`
}

type RefreshSettings struct {
	GlobalIntervalSeconds int `json:"global_interval_seconds"`
	TimeoutSeconds        int `json:"timeout_seconds"`
}

type ProviderSetting struct {
	Enabled            bool     `json:"enabled"`
	SourceOrder        []string `json:"source_order"`
	CLICommand         string   `json:"cli_command,omitempty"`
	TimeoutSeconds     int      `json:"timeout_seconds"`
	MinIntervalSeconds int      `json:"min_interval_seconds"`
	CredentialRef      string   `json:"credential_ref,omitempty"`
}

type TmuxSettings struct {
	Enabled            bool   `json:"enabled"`
	StatusFile         string `json:"status_file"`
	JSONCacheFile      string `json:"json_cache_file"`
	UseTmuxColorFormat bool   `json:"use_tmux_color_format"`
	Position           string `json:"position"`
}

type ThemeSettings struct {
	Name     string `json:"name"`
	DarkMode bool   `json:"dark_mode"`
}

func DefaultSettings() Settings {
	statusFile := filepath.Join(DefaultCacheDir(), "status.txt")
	jsonCacheFile := filepath.Join(DefaultCacheDir(), "snapshot.json")

	return Settings{
		Version: 1,
		Refresh: RefreshSettings{
			GlobalIntervalSeconds: 30,
			TimeoutSeconds:        12,
		},
		Providers: map[string]ProviderSetting{
			"openai": {
				Enabled:            true,
				SourceOrder:        []string{"cli"},
				CLICommand:         "",
				TimeoutSeconds:     10,
				MinIntervalSeconds: 30,
				CredentialRef:      "provider/openai",
			},
			"claude": {
				Enabled:            true,
				SourceOrder:        []string{"cli"},
				CLICommand:         "",
				TimeoutSeconds:     10,
				MinIntervalSeconds: 60,
				CredentialRef:      "provider/claude",
			},
			"gemini": {
				Enabled:            true,
				SourceOrder:        []string{"cli"},
				CLICommand:         "",
				TimeoutSeconds:     10,
				MinIntervalSeconds: 60,
				CredentialRef:      "provider/gemini",
			},
		},
		Tmux: TmuxSettings{
			Enabled:            true,
			StatusFile:         statusFile,
			JSONCacheFile:      jsonCacheFile,
			UseTmuxColorFormat: true,
			Position:           "top",
		},
		Theme: ThemeSettings{
			Name:     "cozy-dark",
			DarkMode: true,
		},
	}
}

func DefaultConfigDir() string {
	if d, err := os.UserConfigDir(); err == nil && d != "" {
		return filepath.Join(d, AppName)
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return "."
	}
	return filepath.Join(home, ".config", AppName)
}

func DefaultSettingsPath() string {
	return filepath.Join(DefaultConfigDir(), "settings.json")
}

func DefaultCacheDir() string {
	if d, err := os.UserCacheDir(); err == nil && d != "" {
		return filepath.Join(d, AppName)
	}
	return filepath.Join(os.TempDir(), AppName)
}

func Load(path string) (Settings, error) {
	if path == "" {
		path = DefaultSettingsPath()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := DefaultSettings()
			return cfg, nil
		}
		return Settings{}, fmt.Errorf("read settings: %w", err)
	}
	cfg := DefaultSettings()
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Settings{}, fmt.Errorf("parse settings: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Settings{}, err
	}
	return cfg, nil
}

func (s Settings) Save(path string) error {
	if path == "" {
		path = DefaultSettingsPath()
	}
	if err := s.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	return nil
}

func (s Settings) Validate() error {
	if s.Refresh.GlobalIntervalSeconds < 5 {
		return errors.New("refresh.global_interval_seconds must be >= 5")
	}
	if s.Refresh.TimeoutSeconds < 1 {
		return errors.New("refresh.timeout_seconds must be >= 1")
	}
	for name, p := range s.Providers {
		if p.MinIntervalSeconds < 5 {
			return fmt.Errorf("providers.%s.min_interval_seconds must be >= 5", name)
		}
		if p.TimeoutSeconds < 1 {
			return fmt.Errorf("providers.%s.timeout_seconds must be >= 1", name)
		}
		if len(p.SourceOrder) == 0 {
			return fmt.Errorf("providers.%s.source_order cannot be empty", name)
		}
		for _, src := range p.SourceOrder {
			if src != "cli" {
				return fmt.Errorf("providers.%s.source_order includes invalid source %q", name, src)
			}
		}
	}
	if s.Tmux.Position != "top" && s.Tmux.Position != "bottom" {
		return errors.New("tmux.position must be top or bottom")
	}
	if strings.TrimSpace(s.Tmux.StatusFile) == "" {
		return errors.New("tmux.status_file cannot be empty")
	}
	if strings.TrimSpace(s.Tmux.JSONCacheFile) == "" {
		return errors.New("tmux.json_cache_file cannot be empty")
	}
	if !s.Theme.DarkMode {
		return errors.New("only dark mode is supported in v1")
	}
	return nil
}

func (s Settings) Provider(name string) (ProviderSetting, bool) {
	p, ok := s.Providers[name]
	return p, ok
}

func (s *Settings) UpsertProvider(name string, p ProviderSetting) {
	if s.Providers == nil {
		s.Providers = make(map[string]ProviderSetting)
	}
	if len(p.SourceOrder) == 0 {
		p.SourceOrder = []string{"cli"}
	}
	if !slices.Contains([]string{"openai", "claude", "gemini"}, name) {
		p.Enabled = false
	}
	s.Providers[name] = p
}
