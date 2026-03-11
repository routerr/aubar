package provider

import (
	"context"
	"time"

	"github.com/raychang/ai-usage-bar/internal/domain"
)

type Provider interface {
	ID() domain.ProviderID
	FetchUsage(ctx context.Context) domain.ProviderSnapshot
	MinInterval() time.Duration
}
