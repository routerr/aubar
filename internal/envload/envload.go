package envload

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func Load(paths ...string) error {
	var errs []error
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if err := loadOne(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func DefaultEnvPaths(configDir string) []string {
	cwd, _ := os.Getwd()
	paths := []string{}
	if cwd != "" {
		paths = append(paths, filepath.Join(cwd, ".env"))
	}
	if configDir != "" {
		paths = append(paths, filepath.Join(configDir, ".env"))
	}
	return paths
}

func loadOne(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		if k == "" {
			continue
		}
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
	return s.Err()
}
