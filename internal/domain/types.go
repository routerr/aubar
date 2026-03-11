package domain

import "time"

type ProviderID string

const (
	ProviderOpenAI ProviderID = "openai"
	ProviderClaude ProviderID = "claude"
	ProviderGemini ProviderID = "gemini"
)

type SnapshotStatus string

const (
	StatusOK       SnapshotStatus = "ok"
	StatusDegraded SnapshotStatus = "degraded"
	StatusError    SnapshotStatus = "error"
)

type ProviderSnapshot struct {
	Provider           ProviderID     `json:"provider"`
	Status             SnapshotStatus `json:"status"`
	Reason             string         `json:"reason,omitempty"`
	UsageValue         float64        `json:"usage_value,omitempty"`
	UsageUnit          string         `json:"usage_unit,omitempty"`
	LimitValue         *float64       `json:"limit_value,omitempty"`
	RemainingValue     *float64       `json:"remaining_value,omitempty"`
	RemainingPercent   *float64       `json:"remaining_percent,omitempty"`
	Source             string         `json:"source"`
	ObservedAt         time.Time      `json:"observed_at"`
	NextAllowedRefresh *time.Time     `json:"next_allowed_refresh,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

type Collection struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Snapshots   []ProviderSnapshot `json:"snapshots"`
}
