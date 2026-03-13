package provider

import (
	"context"
	"strings"
	"time"

	"github.com/routerr/aubar/internal/config"
	"github.com/routerr/aubar/internal/domain"
)

type OpenAIProvider struct {
	setting config.ProviderSetting
	cli     CLIExecutor
	client  HTTPClient
}

func NewOpenAIProvider(setting config.ProviderSetting, cli CLIExecutor) *OpenAIProvider {
	if cli == nil {
		cli = DefaultCLIExecutor{}
	}
	return &OpenAIProvider{
		setting: setting,
		cli:     cli,
		client:  defaultHTTPClient(time.Duration(setting.TimeoutSeconds) * time.Second),
	}
}

func (p *OpenAIProvider) ID() domain.ProviderID { return domain.ProviderOpenAI }

func (p *OpenAIProvider) MinInterval() time.Duration {
	return time.Duration(p.setting.MinIntervalSeconds) * time.Second
}

func (p *OpenAIProvider) FetchUsage(ctx context.Context) domain.ProviderSnapshot {
	var errs []string
	for _, src := range p.setting.SourceOrder {
		src = strings.ToLower(strings.TrimSpace(src))
		switch src {
		case "cli":
			s, err := p.fetchCLI(ctx)
			if err == nil {
				return s
			}
			errs = append(errs, "cli: "+err.Error())
		}
	}
	return errored(p.ID(), "combined", strings.Join(errs, " | "))
}

func (p *OpenAIProvider) fetchCLI(ctx context.Context) (domain.ProviderSnapshot, error) {
	return readCodexSessionSnapshot(p.ID())
}
