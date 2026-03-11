package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const capturedQuotaEnvVar = "CLAUDE_CAPTURED_QUOTA_PATH"

// ── Pricing (USD per million tokens) ─────────────────────────────────────────

type modelPricing struct {
	Input       float64
	Output      float64
	CacheCreate float64
	CacheRead   float64
}

var pricingTable = map[string]modelPricing{
	"claude-opus-4-6":           {15.00, 75.00, 18.75, 1.50},
	"claude-opus-4-5":           {15.00, 75.00, 18.75, 1.50},
	"claude-sonnet-4-6":         {3.00, 15.00, 3.75, 0.30},
	"claude-sonnet-4-5":         {3.00, 15.00, 3.75, 0.30},
	"claude-sonnet-3-7":         {3.00, 15.00, 3.75, 0.30},
	"claude-sonnet-3-5":         {3.00, 15.00, 3.75, 0.30},
	"claude-haiku-4-5":          {0.80, 4.00, 1.00, 0.08},
	"claude-haiku-4-5-20251001": {0.80, 4.00, 1.00, 0.08},
	"claude-haiku-3-5":          {0.80, 4.00, 1.00, 0.08},
	"claude-haiku-3":            {0.25, 1.25, 0.30, 0.03},
}

func pricingFor(model string) (modelPricing, bool) {
	if p, ok := pricingTable[model]; ok {
		return p, true
	}
	for prefix, p := range pricingTable {
		if strings.HasPrefix(model, prefix) {
			return p, true
		}
	}
	return modelPricing{}, false
}

func calcCost(p modelPricing, input, output, cacheCreate, cacheRead int64) float64 {
	const m = 1_000_000.0
	return float64(input)*p.Input/m +
		float64(output)*p.Output/m +
		float64(cacheCreate)*p.CacheCreate/m +
		float64(cacheRead)*p.CacheRead/m
}

// ── Subscription quota types ──────────────────────────────────────────────────

type QuotaWindow struct {
	UtilizationPct float64 `json:"utilization_pct"`
	ResetsAt       string  `json:"resets_at"`
}

type ExtraUsage struct {
	IsEnabled      bool    `json:"is_enabled"`
	MonthlyLimit   int64   `json:"monthly_limit_cents"`
	UsedCredits    float64 `json:"used_credits_cents"`
	UtilizationPct float64 `json:"utilization_pct"`
	LimitUSD       float64 `json:"limit_usd"`
	UsedUSD        float64 `json:"used_usd"`
}

type SubscriptionQuota struct {
	Source     string       `json:"source"`
	FiveHour   *QuotaWindow `json:"five_hour"`
	SevenDay   *QuotaWindow `json:"seven_day"`
	ExtraUsage *ExtraUsage  `json:"extra_usage,omitempty"`
}

// ── Output types ──────────────────────────────────────────────────────────────

type RateLimit struct {
	Source                string  `json:"source"`
	Note                  string  `json:"note"`
	RequestsLimit         *int64  `json:"requests_limit"`
	RequestsRemaining     *int64  `json:"requests_remaining"`
	RequestsResetAt       *string `json:"requests_reset_at"`
	InputTokensLimit      *int64  `json:"input_tokens_limit"`
	InputTokensRemaining  *int64  `json:"input_tokens_remaining"`
	InputTokensResetAt    *string `json:"input_tokens_reset_at"`
	OutputTokensLimit     *int64  `json:"output_tokens_limit"`
	OutputTokensRemaining *int64  `json:"output_tokens_remaining"`
	OutputTokensResetAt   *string `json:"output_tokens_reset_at"`
	TokensLimit           *int64  `json:"tokens_limit,omitempty"`
	TokensRemaining       *int64  `json:"tokens_remaining,omitempty"`
	TokensResetAt         *string `json:"tokens_reset_at,omitempty"`
}

type ModelUsage struct {
	ModelID           string  `json:"model_id"`
	InputTokens       int64   `json:"input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	CacheCreateTokens int64   `json:"cache_creation_input_tokens"`
	CacheReadTokens   int64   `json:"cache_read_input_tokens"`
	CostUSD           float64 `json:"cost_usd"`
}

type UsagePeriod struct {
	Window            string       `json:"window"`
	StartingAt        string       `json:"starting_at"`
	EndingAt          string       `json:"ending_at"`
	TotalCostUSD      float64      `json:"total_cost_usd"`
	TotalInputTokens  int64        `json:"total_input_tokens"`
	TotalOutputTokens int64        `json:"total_output_tokens"`
	Models            []ModelUsage `json:"models"`
}

type APIError struct {
	Period  string `json:"period"`
	Message string `json:"message"`
	Note    string `json:"note,omitempty"`
}

type Output struct {
	GeneratedAt       string             `json:"generated_at"`
	APIKeyPrefix      string             `json:"api_key_prefix,omitempty"`
	Source            string             `json:"source"`
	SubscriptionQuota *SubscriptionQuota `json:"subscription_quota"`
	RateLimit         *RateLimit         `json:"rate_limit"`
	Last5Hours        *UsagePeriod       `json:"last_5_hours"`
	Last7Days         *UsagePeriod       `json:"last_7_days"`
	Errors            []APIError         `json:"errors,omitempty"`
}

// ── JSONL parsing types ───────────────────────────────────────────────────────

type jsonlUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

type jsonlMessage struct {
	Model string     `json:"model"`
	Usage jsonlUsage `json:"usage"`
}

type jsonlEntry struct {
	Type      string       `json:"type"`
	Timestamp string       `json:"timestamp"`
	Message   jsonlMessage `json:"message"`
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func headerInt64(h http.Header, key string) *int64 {
	s := h.Get(key)
	if s == "" {
		return nil
	}
	var v int64
	if _, err := fmt.Sscan(s, &v); err != nil {
		return nil
	}
	return &v
}

func headerString(h http.Header, key string) *string {
	s := h.Get(key)
	if s == "" {
		return nil
	}
	return &s
}

func maskKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:12] + "..."
}

// ── OAuth token from macOS Keychain ──────────────────────────────────────────

type keychainCreds struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
	} `json:"claudeAiOauth"`
}

func oauthTokenFromKeychain() (string, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-s", "Claude Code-credentials", "-w").Output()
	if err != nil {
		return "", fmt.Errorf("keychain lookup failed: %w", err)
	}
	var creds keychainCreds
	if err := json.Unmarshal(bytes.TrimSpace(out), &creds); err != nil {
		return "", fmt.Errorf("failed to parse keychain credentials: %w", err)
	}
	if creds.ClaudeAiOauth.AccessToken == "" {
		return "", fmt.Errorf("no OAuth access token found in keychain")
	}
	return creds.ClaudeAiOauth.AccessToken, nil
}

type capturedQuotaEntry struct {
	RequestHeaders map[string]string `json:"request_headers"`
	Data           json.RawMessage   `json:"data"`
}

func defaultCapturedQuotaPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv(capturedQuotaEnvVar)); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "captured_quota.json"), nil
}

func readCapturedQuotaEntry(path string) (capturedQuotaEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return capturedQuotaEntry{}, err
	}
	var payload map[string]capturedQuotaEntry
	if err := json.Unmarshal(raw, &payload); err != nil {
		return capturedQuotaEntry{}, fmt.Errorf("failed to parse captured quota file: %w", err)
	}
	entry, ok := payload["api.anthropic.com/api/oauth/usage"]
	if ok {
		return entry, nil
	}
	for _, entry := range payload {
		if len(entry.Data) == 0 {
			continue
		}
		if token, ok := bearerTokenFromHeaders(entry.RequestHeaders); ok && token != "" {
			return entry, nil
		}
	}
	return capturedQuotaEntry{}, fmt.Errorf("oauth usage capture missing from %s", path)
}

func bearerTokenFromHeaders(headers map[string]string) (string, bool) {
	for key, value := range headers {
		if !strings.EqualFold(key, "authorization") {
			continue
		}
		value = strings.TrimSpace(value)
		if !strings.HasPrefix(strings.ToLower(value), "bearer ") {
			continue
		}
		token := strings.TrimSpace(value[len("Bearer "):])
		if token == "" {
			return "", false
		}
		return token, true
	}
	return "", false
}

func oauthTokenFromCapturedQuota(path string) (string, error) {
	entry, err := readCapturedQuotaEntry(path)
	if err != nil {
		return "", err
	}
	token, ok := bearerTokenFromHeaders(entry.RequestHeaders)
	if !ok {
		return "", fmt.Errorf("authorization bearer token missing from %s", path)
	}
	return token, nil
}

func cachedSubscriptionQuota(path string) (*SubscriptionQuota, error) {
	entry, err := readCapturedQuotaEntry(path)
	if err != nil {
		return nil, err
	}
	if len(entry.Data) == 0 {
		return nil, fmt.Errorf("captured oauth usage data missing from %s", path)
	}
	var payload oauthUsageResp
	if err := json.Unmarshal(entry.Data, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse captured oauth usage data: %w", err)
	}
	quota := subscriptionQuotaFromOAuthUsage(payload)
	quota.Source = "captured_quota_file"
	return quota, nil
}

func resolveOAuthToken(capturedQuotaPath string) (string, error) {
	token, err := oauthTokenFromKeychain()
	if err == nil {
		return token, nil
	}
	keychainErr := err
	token, err = oauthTokenFromCapturedQuota(capturedQuotaPath)
	if err == nil {
		return token, nil
	}
	return "", fmt.Errorf("%v; captured quota fallback failed: %w", keychainErr, err)
}

// ── Subscription quota fetch ──────────────────────────────────────────────────

type oauthUsageResp struct {
	FiveHour *struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	} `json:"five_hour"`
	SevenDay *struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	} `json:"seven_day"`
	ExtraUsage *struct {
		IsEnabled    bool    `json:"is_enabled"`
		MonthlyLimit int64   `json:"monthly_limit"`
		UsedCredits  float64 `json:"used_credits"`
		Utilization  float64 `json:"utilization"`
	} `json:"extra_usage"`
}

func fetchSubscriptionQuota(client *http.Client, oauthToken string) (*SubscriptionQuota, *APIError) {
	req, err := http.NewRequest(http.MethodGet,
		"https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, &APIError{Period: "subscription_quota", Message: "failed to build request: " + err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+oauthToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-code/2.1.71")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, &APIError{Period: "subscription_quota", Message: "request failed: " + err.Error()}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32768))
	if resp.StatusCode >= 400 {
		return nil, &APIError{
			Period:  "subscription_quota",
			Message: fmt.Sprintf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
		}
	}

	var r oauthUsageResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, &APIError{Period: "subscription_quota", Message: "failed to parse response: " + err.Error()}
	}

	q := subscriptionQuotaFromOAuthUsage(r)
	q.Source = "api.anthropic.com/api/oauth/usage"
	return q, nil
}

func subscriptionQuotaFromOAuthUsage(r oauthUsageResp) *SubscriptionQuota {
	q := &SubscriptionQuota{}
	if r.FiveHour != nil {
		q.FiveHour = &QuotaWindow{
			UtilizationPct: r.FiveHour.Utilization,
			ResetsAt:       r.FiveHour.ResetsAt,
		}
	}
	if r.SevenDay != nil {
		q.SevenDay = &QuotaWindow{
			UtilizationPct: r.SevenDay.Utilization,
			ResetsAt:       r.SevenDay.ResetsAt,
		}
	}
	if r.ExtraUsage != nil {
		q.ExtraUsage = &ExtraUsage{
			IsEnabled:      r.ExtraUsage.IsEnabled,
			MonthlyLimit:   r.ExtraUsage.MonthlyLimit,
			UsedCredits:    r.ExtraUsage.UsedCredits,
			UtilizationPct: r.ExtraUsage.Utilization,
			LimitUSD:       float64(r.ExtraUsage.MonthlyLimit) / 100.0,
			UsedUSD:        r.ExtraUsage.UsedCredits / 100.0,
		}
	}
	return q
}

// ── Rate limit probe ──────────────────────────────────────────────────────────

func probeRateLimit(client *http.Client, key string) (*RateLimit, *APIError) {
	body := `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`
	req, err := http.NewRequest(http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewBufferString(body))
	if err != nil {
		return nil, &APIError{Period: "rate_limit", Message: "failed to build probe: " + err.Error()}
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, &APIError{Period: "rate_limit", Message: "probe failed: " + err.Error()}
	}
	resp.Body.Close()

	h := resp.Header
	rl := &RateLimit{
		Source:                "probe_response_headers",
		Note:                  "Per-minute window; resets each minute",
		RequestsLimit:         headerInt64(h, "anthropic-ratelimit-requests-limit"),
		RequestsRemaining:     headerInt64(h, "anthropic-ratelimit-requests-remaining"),
		RequestsResetAt:       headerString(h, "anthropic-ratelimit-requests-reset"),
		InputTokensLimit:      headerInt64(h, "anthropic-ratelimit-input-tokens-limit"),
		InputTokensRemaining:  headerInt64(h, "anthropic-ratelimit-input-tokens-remaining"),
		InputTokensResetAt:    headerString(h, "anthropic-ratelimit-input-tokens-reset"),
		OutputTokensLimit:     headerInt64(h, "anthropic-ratelimit-output-tokens-limit"),
		OutputTokensRemaining: headerInt64(h, "anthropic-ratelimit-output-tokens-remaining"),
		OutputTokensResetAt:   headerString(h, "anthropic-ratelimit-output-tokens-reset"),
		TokensLimit:           headerInt64(h, "anthropic-ratelimit-tokens-limit"),
		TokensRemaining:       headerInt64(h, "anthropic-ratelimit-tokens-remaining"),
		TokensResetAt:         headerString(h, "anthropic-ratelimit-tokens-reset"),
	}
	if resp.StatusCode == 429 {
		return rl, &APIError{Period: "rate_limit",
			Message: fmt.Sprintf("http %d: rate limited; headers still captured", resp.StatusCode)}
	}
	if resp.StatusCode >= 400 {
		return rl, &APIError{Period: "rate_limit",
			Message: fmt.Sprintf("http %d: probe returned error status", resp.StatusCode)}
	}
	return rl, nil
}

// ── Local JSONL scanning ──────────────────────────────────────────────────────

type msgRecord struct {
	ts          time.Time
	model       string
	input       int64
	output      int64
	cacheCreate int64
	cacheRead   int64
}

func scanClaudeDir(claudeDir string) ([]msgRecord, error) {
	projectsDir := filepath.Join(claudeDir, "projects")
	var records []msgRecord
	err := filepath.WalkDir(projectsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		recs, _ := parseJSONL(path)
		records = append(records, recs...)
		return nil
	})
	return records, err
}

func parseJSONL(path string) ([]msgRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var records []msgRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry jsonlEntry
		if err := json.Unmarshal(line, &entry); err != nil || entry.Type != "assistant" {
			continue
		}
		msg := entry.Message
		if msg.Model == "" || (msg.Usage.InputTokens == 0 && msg.Usage.OutputTokens == 0 &&
			msg.Usage.CacheCreationInputTokens == 0 && msg.Usage.CacheReadInputTokens == 0) {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, entry.Timestamp)
			if err != nil {
				continue
			}
		}
		records = append(records, msgRecord{
			ts:          ts,
			model:       msg.Model,
			input:       msg.Usage.InputTokens,
			output:      msg.Usage.OutputTokens,
			cacheCreate: msg.Usage.CacheCreationInputTokens,
			cacheRead:   msg.Usage.CacheReadInputTokens,
		})
	}
	return records, scanner.Err()
}

func aggregatePeriod(records []msgRecord, window string, start, end time.Time) *UsagePeriod {
	type acc struct{ input, output, cacheCreate, cacheRead int64 }
	byModel := map[string]*acc{}
	for _, r := range records {
		if r.ts.Before(start) || r.ts.After(end) {
			continue
		}
		a := byModel[r.model]
		if a == nil {
			a = &acc{}
			byModel[r.model] = a
		}
		a.input += r.input
		a.output += r.output
		a.cacheCreate += r.cacheCreate
		a.cacheRead += r.cacheRead
	}

	period := &UsagePeriod{
		Window:     window,
		StartingAt: start.UTC().Format(time.RFC3339),
		EndingAt:   end.UTC().Format(time.RFC3339),
		Models:     []ModelUsage{},
	}
	for modelID, a := range byModel {
		pricing, _ := pricingFor(modelID)
		cost := calcCost(pricing, a.input, a.output, a.cacheCreate, a.cacheRead)
		period.Models = append(period.Models, ModelUsage{
			ModelID:           modelID,
			InputTokens:       a.input,
			OutputTokens:      a.output,
			CacheCreateTokens: a.cacheCreate,
			CacheReadTokens:   a.cacheRead,
			CostUSD:           cost,
		})
		period.TotalInputTokens += a.input
		period.TotalOutputTokens += a.output
		period.TotalCostUSD += cost
	}
	return period
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	var (
		flagKey       = flag.String("key", "", "Anthropic API key for rate limit probe (overrides ANTHROPIC_API_KEY / CLAUDE_API_KEY)")
		flagTimeout   = flag.Int("timeout", 15, "HTTP timeout in seconds")
		flagClaudeDir = flag.String("claude-dir", "", "Claude Code data directory (default: ~/.claude)")
		flagNoProbe   = flag.Bool("no-probe", false, "Skip rate limit probe")
		flagNoQuota   = flag.Bool("no-quota", false, "Skip subscription quota fetch (no OAuth token needed)")
	)
	flag.Parse()

	claudeDir := *flagClaudeDir
	if claudeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error: cannot determine home directory:", err)
			os.Exit(1)
		}
		claudeDir = filepath.Join(home, ".claude")
	}

	key := *flagKey
	if key == "" {
		key = os.Getenv("ANTHROPIC_API_KEY")
	}
	if key == "" {
		key = os.Getenv("CLAUDE_API_KEY")
	}

	now := time.Now().UTC()
	out := Output{
		GeneratedAt: now.Format(time.RFC3339),
		Source:      "claude_code_local_logs",
		Errors:      []APIError{},
	}
	if key != "" {
		out.APIKeyPrefix = maskKey(key)
	}

	client := &http.Client{Timeout: time.Duration(*flagTimeout) * time.Second}
	var wg sync.WaitGroup
	var mu sync.Mutex

	// ── Subscription quota via OAuth token ────────────────────────────────────
	if !*flagNoQuota {
		wg.Add(1)
		go func() {
			defer wg.Done()
			capturedQuotaPath, err := defaultCapturedQuotaPath()
			if err != nil {
				mu.Lock()
				out.Errors = append(out.Errors, APIError{
					Period:  "subscription_quota",
					Message: "failed to determine captured quota path: " + err.Error(),
				})
				mu.Unlock()
				return
			}
			oauthToken, err := resolveOAuthToken(capturedQuotaPath)
			if err != nil {
				cachedQuota, cachedErr := cachedSubscriptionQuota(capturedQuotaPath)
				mu.Lock()
				if cachedErr == nil {
					out.SubscriptionQuota = cachedQuota
					out.Errors = append(out.Errors, APIError{
						Period:  "subscription_quota",
						Message: err.Error(),
						Note:    "Using cached captured quota because live OAuth token lookup failed.",
					})
				} else {
					out.Errors = append(out.Errors, APIError{
						Period:  "subscription_quota",
						Message: err.Error(),
						Note:    "OAuth token unavailable; subscription quota skipped. Use -no-quota to suppress.",
					})
				}
				mu.Unlock()
				return
			}
			q, apiErr := fetchSubscriptionQuota(client, oauthToken)
			mu.Lock()
			defer mu.Unlock()
			if apiErr != nil {
				cachedQuota, cachedErr := cachedSubscriptionQuota(capturedQuotaPath)
				if cachedErr == nil {
					out.SubscriptionQuota = cachedQuota
					apiErr.Note = "Using cached captured quota because live OAuth quota fetch failed."
				}
				out.Errors = append(out.Errors, *apiErr)
				return
			}
			out.SubscriptionQuota = q
		}()
	}

	// ── Rate limit probe via API key ──────────────────────────────────────────
	if !*flagNoProbe && key != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rl, probeErr := probeRateLimit(client, key)
			mu.Lock()
			defer mu.Unlock()
			out.RateLimit = rl
			if probeErr != nil {
				out.Errors = append(out.Errors, *probeErr)
			}
		}()
	}

	// ── Local JSONL scan ──────────────────────────────────────────────────────
	wg.Add(1)
	go func() {
		defer wg.Done()
		records, err := scanClaudeDir(claudeDir)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			out.Errors = append(out.Errors, APIError{
				Period:  "local_scan",
				Message: "error scanning Claude Code logs: " + err.Error(),
			})
			out.Last5Hours = emptyPeriod("last_5_hours", now.Add(-5*time.Hour), now)
			out.Last7Days = emptyPeriod("last_7_days", now.Add(-7*24*time.Hour), now)
			return
		}
		out.Last5Hours = aggregatePeriod(records, "last_5_hours", now.Add(-5*time.Hour), now)
		out.Last7Days = aggregatePeriod(records, "last_7_days", now.Add(-7*24*time.Hour), now)
	}()

	wg.Wait()

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "error encoding output:", err)
		os.Exit(1)
	}
}

func emptyPeriod(window string, start, end time.Time) *UsagePeriod {
	return &UsagePeriod{
		Window:     window,
		StartingAt: start.UTC().Format(time.RFC3339),
		EndingAt:   end.UTC().Format(time.RFC3339),
		Models:     []ModelUsage{},
	}
}
