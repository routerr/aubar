package keyringx

import (
	"fmt"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

const serviceName = "aubar"

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
	return keyring.Set(serviceName, KeyForProvider(provider), value)
}

func Get(provider string) (string, error) {
	if strings.TrimSpace(provider) == "" {
		return "", fmt.Errorf("provider is required")
	}
	return keyring.Get(serviceName, KeyForProvider(provider))
}

func Delete(provider string) error {
	if strings.TrimSpace(provider) == "" {
		return fmt.Errorf("provider is required")
	}
	return keyring.Delete(serviceName, KeyForProvider(provider))
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
