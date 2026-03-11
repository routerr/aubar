package runner

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/raychang/ai-usage-bar/internal/config"
	"github.com/raychang/ai-usage-bar/internal/domain"
	"github.com/raychang/ai-usage-bar/internal/provider"
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
	now := time.Now()
	results := make([]domain.ProviderSnapshot, 0, len(s.providers))
	var wg sync.WaitGroup
	type item struct {
		idx int
		s   domain.ProviderSnapshot
	}
	ch := make(chan item, len(s.providers))

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
			ch <- item{idx: idx, s: clone}
			continue
		}

		wg.Add(1)
		go func(i int, p provider.Provider) {
			defer wg.Done()
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
	wg.Wait()
	close(ch)

	buffer := make([]item, 0, len(s.providers))
	for it := range ch {
		buffer = append(buffer, it)
	}
	sort.Slice(buffer, func(i, j int) bool {
		return buffer[i].idx < buffer[j].idx
	})
	for _, it := range buffer {
		results = append(results, it.s)
	}

	return domain.Collection{GeneratedAt: now, Snapshots: results}
}

func (s *Service) Run(ctx context.Context, onTick func(domain.Collection)) {
	interval := time.Duration(s.cfg.Refresh.GlobalIntervalSeconds) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	onTick(s.Collect(ctx))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			onTick(s.Collect(ctx))
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
