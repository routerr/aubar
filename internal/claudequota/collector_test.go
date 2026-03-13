package claudequota

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOAuthTokenFromCapturedQuota(t *testing.T) {
	path := writeCapturedQuotaFixture(t, `{
  "api.anthropic.com/api/oauth/usage": {
    "request_headers": {
      "Authorization": "Bearer fixture-anthropic-token"
    },
    "data": {
      "five_hour": {"utilization": 23, "resets_at": "2026-03-10T10:00:00Z"},
      "seven_day": {"utilization": 15, "resets_at": "2026-03-17T10:00:00Z"}
    }
  }
}`)

	token, err := oauthTokenFromCapturedQuota(path)
	if err != nil {
		t.Fatalf("expected token, got error %v", err)
	}
	if token != "fixture-anthropic-token" {
		t.Fatalf("expected fixture token, got %q", token)
	}
}

func TestCachedSubscriptionQuota(t *testing.T) {
	path := writeCapturedQuotaFixture(t, `{
  "api.anthropic.com/api/oauth/usage": {
    "request_headers": {
      "Authorization": "Bearer fixture-anthropic-token"
    },
    "data": {
      "five_hour": {"utilization": 23, "resets_at": "2026-03-10T10:00:00Z"},
      "seven_day": {"utilization": 15, "resets_at": "2026-03-17T10:00:00Z"},
      "extra_usage": {"is_enabled": true, "monthly_limit": 5000, "used_credits": 461, "utilization": 9.22}
    }
  }
}`)

	quota, err := cachedSubscriptionQuota(path)
	if err != nil {
		t.Fatalf("expected cached quota, got error %v", err)
	}
	if quota.Source != "captured_quota_file" {
		t.Fatalf("expected captured quota source, got %+v", quota)
	}
	if quota.FiveHour == nil || quota.FiveHour.UtilizationPct != 23 {
		t.Fatalf("expected five hour quota, got %+v", quota)
	}
	if quota.SevenDay == nil || quota.SevenDay.UtilizationPct != 15 {
		t.Fatalf("expected seven day quota, got %+v", quota)
	}
	if quota.ExtraUsage == nil || quota.ExtraUsage.LimitUSD != 50 || quota.ExtraUsage.UsedUSD != 4.61 {
		t.Fatalf("expected extra usage conversion, got %+v", quota.ExtraUsage)
	}
}

func TestDefaultCapturedQuotaPathUsesEnvironmentOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "captured.json")
	t.Setenv(capturedQuotaEnvVar, path)

	got, err := defaultCapturedQuotaPath()
	if err != nil {
		t.Fatalf("expected default path, got error %v", err)
	}
	if got != path {
		t.Fatalf("expected override path %q, got %q", path, got)
	}
}

func writeCapturedQuotaFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "captured_quota.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
