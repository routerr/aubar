package provider

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCLI struct {
	attempt int
}

func (f *fakeCLI) Run(_ context.Context, _ string, _ time.Duration) (string, string, error) {
	f.attempt++
	if f.attempt == 1 {
		return "", "temporary", errors.New("boom")
	}
	return `{"usage":12.3,"limit":50}`, "", nil
}

func TestRunCLIWithRetry(t *testing.T) {
	cli := &fakeCLI{}
	out, _, attempts, err := runCLIWithRetry(context.Background(), cli, "fake", time.Second, 2)
	if err != nil {
		t.Fatalf("expected success on retry, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if out == "" {
		t.Fatalf("expected output")
	}
}
