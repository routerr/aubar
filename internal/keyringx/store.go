package keyringx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	AppName     = "ai-usage-bar"
	serviceName = "aubar"
)

func KeyForProvider(provider string) string {
	return "provider/" + provider
}

func Set(provider, value string) error {
	if strings.TrimSpace(provider) == "" {
		return fmt.Errorf("provider is required")
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("secret is required")
	}

	err := keyring.Set(serviceName, KeyForProvider(provider), value)
	if err != nil {
		// Fallback to file on Linux/systems without a working keyring service
		return setFileFallback(provider, value)
	}
	return nil
}

func Get(provider string) (string, error) {
	if strings.TrimSpace(provider) == "" {
		return "", fmt.Errorf("provider is required")
	}

	val, err := keyring.Get(serviceName, KeyForProvider(provider))
	if err == nil {
		return val, nil
	}

	// Fallback to file
	return getFileFallback(provider)
}

func Delete(provider string) error {
	if strings.TrimSpace(provider) == "" {
		return fmt.Errorf("provider is required")
	}

	_ = keyring.Delete(serviceName, KeyForProvider(provider))
	return deleteFileFallback(provider)
}

func ResolveCredential(provider string, envNames []string) (string, string) {
	for _, k := range envNames {
		if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
			return v, "env:" + k
		}
	}
	v, err := Get(provider)
	if err == nil && strings.TrimSpace(v) != "" {
		return v, "keyring"
	}
	return "", ""
}

func keysDir() string {
	if d, err := os.UserConfigDir(); err == nil && d != "" {
		return filepath.Join(d, AppName, ".keys")
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return filepath.Join(os.TempDir(), AppName, ".keys")
	}
	return filepath.Join(home, ".config", AppName, ".keys")
}

func keyFilePath(provider string) string {
	// Sanitize provider name for filename
	safeName := strings.ReplaceAll(provider, "/", "_")
	return filepath.Join(keysDir(), safeName)
}

func setFileFallback(provider, value string) error {
	dir := keysDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create keys dir: %w", err)
	}
	path := keyFilePath(provider)
	return os.WriteFile(path, []byte(value), 0o600)
}

func getFileFallback(provider string) (string, error) {
	path := keyFilePath(provider)
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func deleteFileFallback(provider string) error {
	path := keyFilePath(provider)
	return os.Remove(path)
}
