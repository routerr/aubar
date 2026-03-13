package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/routerr/aubar/internal/domain"
)

type CLIExecutor interface {
	Run(ctx context.Context, command string, timeout time.Duration) (stdout string, stderr string, err error)
}

type DefaultCLIExecutor struct{}

func (DefaultCLIExecutor) Run(ctx context.Context, command string, timeout time.Duration) (string, string, error) {
	return runCLI(ctx, command, timeout, nil)
}

type KeyringCLIExecutor struct {
	KeyringGet func(provider string) (string, error)
}

func (e KeyringCLIExecutor) Run(ctx context.Context, command string, timeout time.Duration) (string, string, error) {
	var env []string
	if strings.Contains(command, "quota") && e.KeyringGet != nil {
		if token, err := e.KeyringGet("claude-oauth"); err == nil && token != "" {
			env = append(env, "CLAUDE_OAUTH_TOKEN="+token)
		}
	}
	return runCLI(ctx, command, timeout, env)
}

func runCLI(ctx context.Context, command string, timeout time.Duration, extraEnv []string) (string, string, error) {
	if strings.TrimSpace(command) == "" {
		return "", "", errors.New("empty command")
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "sh", "-lc", command)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	return out.String(), errOut.String(), err
}

func runCLIWithRetry(ctx context.Context, exec CLIExecutor, command string, timeout time.Duration, attempts int) (string, string, int, error) {
	if attempts < 1 {
		attempts = 1
	}
	var out string
	var errOut string
	var err error
	for i := 1; i <= attempts; i++ {
		out, errOut, err = exec.Run(ctx, command, timeout)
		if err == nil {
			return out, errOut, i, nil
		}
		select {
		case <-ctx.Done():
			return out, errOut, i, ctx.Err()
		case <-time.After(time.Duration(i) * 200 * time.Millisecond):
		}
	}
	return out, errOut, attempts, err
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func defaultHTTPClient(timeout time.Duration) HTTPClient {
	return &http.Client{Timeout: timeout}
}

func readJSONBody[T any](resp *http.Response) (T, error) {
	var v T
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return v, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	// Limit read to 2MB to prevent memory exhaustion (GO-HTTPCLIENT-001)
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return v, err
	}
	return v, nil
}

func degraded(provider domain.ProviderID, source, reason string) domain.ProviderSnapshot {
	return domain.ProviderSnapshot{
		Provider:   provider,
		Status:     domain.StatusDegraded,
		Source:     source,
		Reason:     reason,
		ObservedAt: time.Now(),
	}
}

func errored(provider domain.ProviderID, source, reason string) domain.ProviderSnapshot {
	return domain.ProviderSnapshot{
		Provider:   provider,
		Status:     domain.StatusError,
		Source:     source,
		Reason:     reason,
		ObservedAt: time.Now(),
	}
}

func usesRuntimeCollector(command string, defaults ...string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return true
	}
	for _, candidate := range defaults {
		if command == strings.TrimSpace(candidate) {
			return true
		}
	}
	return false
}

func okSnapshot(provider domain.ProviderID, source, unit string, usage float64, limit *float64) domain.ProviderSnapshot {
	s := domain.ProviderSnapshot{
		Provider:   provider,
		Status:     domain.StatusOK,
		Source:     source,
		UsageValue: usage,
		UsageUnit:  unit,
		ObservedAt: time.Now(),
	}
	if limit != nil {
		s.LimitValue = limit
		remaining := *limit - usage
		s.RemainingValue = &remaining
		if *limit > 0 {
			p := (remaining / *limit) * 100
			s.RemainingPercent = &p
		}
	}
	return s
}
