package runner

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/raychang/ai-usage-bar/internal/config"
	"github.com/raychang/ai-usage-bar/internal/domain"
	"github.com/raychang/ai-usage-bar/internal/provider"
)

type fakeProvider struct {
	id    domain.ProviderID
	count atomic.Int32
}

func (f *fakeProvider) ID() domain.ProviderID      { return f.id }
func (f *fakeProvider) MinInterval() time.Duration { return 10 * time.Second }
func (f *fakeProvider) FetchUsage(context.Context) domain.ProviderSnapshot {
	f.count.Add(1)
	return domain.ProviderSnapshot{Provider: f.id, Status: domain.StatusOK, Source: "test", ObservedAt: time.Now()}
}

type hangingProvider struct {
	id domain.ProviderID
}

func (h *hangingProvider) ID() domain.ProviderID      { return h.id }
func (h *hangingProvider) MinInterval() time.Duration { return 10 * time.Second }
func (h *hangingProvider) FetchUsage(context.Context) domain.ProviderSnapshot {
	select {}
}

func TestSchedulerRespectsMinInterval(t *testing.T) {
	cfg := config.DefaultSettings()
	cfg.Refresh.GlobalIntervalSeconds = 30
	p := &fakeProvider{id: domain.ProviderOpenAI}
	providers := []provider.Provider{p}
	s := NewService(cfg, providers)
	_ = s.Collect(context.Background())
	_ = s.Collect(context.Background())
	if got := p.count.Load(); got != 1 {
		t.Fatalf("expected one fetch due to min interval, got %d", got)
	}
}

func TestCollectTimesOutSlowProviderAndReturnsFastResults(t *testing.T) {
	cfg := config.DefaultSettings()
	cfg.Refresh.TimeoutSeconds = 1
	fast := &fakeProvider{id: domain.ProviderOpenAI}
	slow := &hangingProvider{id: domain.ProviderGemini}
	s := NewService(cfg, []provider.Provider{fast, slow})

	start := time.Now()
	col := s.Collect(context.Background())
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("collect took too long: %s", elapsed)
	}
	if len(col.Snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(col.Snapshots))
	}

	if col.Snapshots[0].Provider != domain.ProviderOpenAI || col.Snapshots[0].Status != domain.StatusOK {
		t.Fatalf("expected fast provider result first, got %+v", col.Snapshots[0])
	}
	if col.Snapshots[1].Provider != domain.ProviderGemini || col.Snapshots[1].Status != domain.StatusError {
		t.Fatalf("expected timed-out provider error, got %+v", col.Snapshots[1])
	}
	if col.Snapshots[1].Source != "timeout" {
		t.Fatalf("expected timeout source, got %+v", col.Snapshots[1])
	}
}
