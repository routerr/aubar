package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildShBuildsNativeBinaries(t *testing.T) {
	scriptPath := filepath.Join(".", "build.sh")
	if _, err := os.Stat(scriptPath); err != nil {
		t.Fatalf("build.sh should exist: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "native")
	cmd := exec.Command("sh", scriptPath, "--output-dir", outputDir)
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build.sh failed: %v\n%s", err, out)
	}

	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}

	for _, name := range []string{"aubar", "quota", "gemini-quota"} {
		path := filepath.Join(outputDir, name+suffix)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected binary %s to exist: %v\n%s", path, err, out)
		}
		if info.IsDir() {
			t.Fatalf("expected %s to be a file, not a directory", path)
		}
	}
}

func TestBuildPS1ExistsAndTargetsAllBinaries(t *testing.T) {
	body, err := os.ReadFile("build.ps1")
	if err != nil {
		t.Fatalf("build.ps1 should exist: %v", err)
	}

	text := string(body)
	for _, token := range []string{"aubar", "quota", "gemini-quota"} {
		if !strings.Contains(text, token) {
			t.Fatalf("build.ps1 should reference %q", token)
		}
	}
}

func TestTrackedTextFilesDoNotReferenceOldGitHubPath(t *testing.T) {
	legacyOwner := "raychang"
	legacyRepo := "ai-usage-bar"
	oldRefs := []string{
		"github.com/" + legacyOwner + "/" + legacyRepo,
		"https://github.com/" + legacyOwner + "/" + legacyRepo,
		"raw.githubusercontent.com/" + legacyOwner + "/" + legacyRepo,
		legacyOwner + "/" + legacyRepo,
	}
	allowedExt := map[string]bool{
		".go":  true,
		".md":  true,
		".mod": true,
		".ps1": true,
		".sh":  true,
		".txt": true,
	}

	var hits []string
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".gocache", ".gocache-buildtmp", ".gocache-docs", ".gocache-live", "$tmpdir":
				return filepath.SkipDir
			}
			return nil
		}
		if !allowedExt[filepath.Ext(path)] {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		for _, oldRef := range oldRefs {
			if strings.Contains(text, oldRef) {
				hits = append(hits, path)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository: %v", err)
	}
	if len(hits) > 0 {
		t.Fatalf("found old GitHub references in: %s", strings.Join(hits, ", "))
	}
}
