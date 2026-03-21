package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/routerr/aubar/internal/config"
	"github.com/routerr/aubar/internal/domain"
	"github.com/routerr/aubar/internal/provider"
)

type Service struct {
	cfg         config.Settings
	providers   []provider.Provider
	mu          sync.Mutex
	last        map[domain.ProviderID]domain.ProviderSnapshot
	nextAllowed map[domain.ProviderID]time.Time
}

func NewService(cfg config.Settings, providers []provider.Provider) *Service {
	return &Service{
		cfg:         cfg,
		providers:   providers,
		last:        make(map[domain.ProviderID]domain.ProviderSnapshot),
		nextAllowed: make(map[domain.ProviderID]time.Time),
	}
}

func (s *Service) Collect(ctx context.Context) domain.Collection {
	return s.CollectStreaming(ctx, nil)
}

// CollectStreaming fetches all providers concurrently.  Each time a provider
// finishes, onSnapshot is called (if non-nil) with the partial collection
// assembled so far.  This lets callers write intermediate cache files so that
// tmux picks up results progressively instead of waiting for the slowest
// provider.
func (s *Service) CollectStreaming(ctx context.Context, onSnapshot func(domain.Collection)) domain.Collection {
	now := time.Now()

	type item struct {
		idx int
		s   domain.ProviderSnapshot
	}
	ch := make(chan item, len(s.providers))

	// slots holds the final snapshot for each provider index.
	// A nil entry means the provider hasn't reported yet.
	slots := make([]*domain.ProviderSnapshot, len(s.providers))
	remaining := 0

	for idx, p := range s.providers {
		s.mu.Lock()
		next, hasNext := s.nextAllowed[p.ID()]
		prev, hasPrev := s.last[p.ID()]
		s.mu.Unlock()
		if hasNext && now.Before(next) && hasPrev {
			clone := prev
			clone.Metadata = mergeMetadata(clone.Metadata, map[string]any{"cached": true})
			t := next
			clone.NextAllowedRefresh = &t
			slots[idx] = &clone
			continue
		}

		remaining++
		go func(i int, p provider.Provider) {
			snap := s.fetchWithTimeout(ctx, p, now)
			snap.ObservedAt = now
			nextAfter := now.Add(p.MinInterval())
			if snap.Status == domain.StatusError {
				nextAfter = now.Add(maxDur(p.MinInterval()*2, time.Duration(s.cfg.Refresh.GlobalIntervalSeconds)*time.Second))
			}
			s.mu.Lock()
			s.last[p.ID()] = snap
			s.nextAllowed[p.ID()] = nextAfter
			s.mu.Unlock()
			t := nextAfter
			snap.NextAllowedRefresh = &t
			ch <- item{idx: i, s: snap}
		}(idx, p)
	}

	// Drain results as they arrive, emitting partial collections.
	for range remaining {
		it := <-ch
		slots[it.idx] = &it.s

		if onSnapshot != nil {
			onSnapshot(s.buildPartialCollection(slots, now))
		}
	}

	return s.buildPartialCollection(slots, now)
}

// buildPartialCollection assembles a Collection from whichever slots are
// already populated, preserving provider order.
func (s *Service) buildPartialCollection(slots []*domain.ProviderSnapshot, generatedAt time.Time) domain.Collection {
	results := make([]domain.ProviderSnapshot, 0, len(slots))
	for _, snap := range slots {
		if snap != nil {
			results = append(results, *snap)
		}
	}
	return domain.Collection{GeneratedAt: generatedAt, Snapshots: results}
}

func (s *Service) Run(ctx context.Context, onTick func(domain.Collection)) {
	s.RunStreaming(ctx, onTick, nil)
}

// RunStreaming is like Run but also calls onSnapshot for each partial
// collection as individual providers finish, enabling progressive cache
// updates between ticks.
func (s *Service) RunStreaming(ctx context.Context, onTick func(domain.Collection), onSnapshot func(domain.Collection)) {
	interval := time.Duration(s.cfg.Refresh.GlobalIntervalSeconds) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	onTick(s.CollectStreaming(ctx, onSnapshot))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			onTick(s.CollectStreaming(ctx, onSnapshot))
		}
	}
}

func (s *Service) fetchWithTimeout(ctx context.Context, p provider.Provider, now time.Time) domain.ProviderSnapshot {
	timeout := time.Duration(s.cfg.Refresh.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan domain.ProviderSnapshot, 1)
	go func() {
		done <- p.FetchUsage(pctx)
	}()

	select {
	case snap := <-done:
		return snap
	case <-pctx.Done():
		return domain.ProviderSnapshot{
			Provider:   p.ID(),
			Status:     domain.StatusError,
			Source:     "timeout",
			Reason:     fmt.Sprintf("provider fetch exceeded %s timeout", timeout),
			ObservedAt: now,
		}
	}
}

func mergeMetadata(base map[string]any, extras map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for k, v := range extras {
		base[k] = v
	}
	return base
}

func maxDur(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
