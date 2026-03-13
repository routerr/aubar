package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/routerr/aubar/internal/claudequota"
)

func main() {
	var (
		flagKey       = flag.String("key", "", "Anthropic API key for rate limit probe (overrides ANTHROPIC_API_KEY / CLAUDE_API_KEY)")
		flagTimeout   = flag.Int("timeout", 15, "HTTP timeout in seconds")
		flagClaudeDir = flag.String("claude-dir", "", "Claude Code data directory (default: ~/.claude)")
		flagNoProbe   = flag.Bool("no-probe", false, "Skip rate limit probe")
		flagNoQuota   = flag.Bool("no-quota", false, "Skip subscription quota fetch (no OAuth token needed)")
	)
	flag.Parse()

	out := claudequota.Collect(context.Background(), claudequota.Options{
		APIKey:    *flagKey,
		Timeout:   time.Duration(*flagTimeout) * time.Second,
		ClaudeDir: *flagClaudeDir,
		NoProbe:   *flagNoProbe,
		NoQuota:   *flagNoQuota,
	})
	if err := claudequota.PrintJSON(os.Stdout, out); err != nil {
		fmt.Fprintln(os.Stderr, "error encoding output:", err)
		os.Exit(1)
	}
}
