package provider

import (
	"sort"

	"github.com/raychang/ai-usage-bar/internal/config"
	"github.com/raychang/ai-usage-bar/internal/domain"
)

func BuildProviders(cfg config.Settings, cli CLIExecutor) []Provider {
	providers := []Provider{}
	if p, ok := cfg.Provider("openai"); ok && p.Enabled {
		providers = append(providers, NewOpenAIProvider(p, cli))
	}
	if p, ok := cfg.Provider("claude"); ok && p.Enabled {
		providers = append(providers, NewClaudeProvider(p, cli))
	}
	if p, ok := cfg.Provider("gemini"); ok && p.Enabled {
		providers = append(providers, NewGeminiProvider(p, cli))
	}
	sort.Slice(providers, func(i, j int) bool {
		order := map[domain.ProviderID]int{
			domain.ProviderOpenAI: 1,
			domain.ProviderClaude: 2,
			domain.ProviderGemini: 3,
		}
		return order[providers[i].ID()] < order[providers[j].ID()]
	})
	return providers
}
