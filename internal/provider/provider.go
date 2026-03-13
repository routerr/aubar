package provider

import (
	"context"
	"time"

	"github.com/routerr/aubar/internal/domain"
)

type Provider interface {
	ID() domain.ProviderID
	FetchUsage(ctx context.Context) domain.ProviderSnapshot
	MinInterval() time.Duration
}
