package aigateway

import (
	"context"
	"fmt"

	or "github.com/revrost/go-openrouter"
)

type OpenRouterProvider struct {
	provider Provider

	sdk *or.Client
}

func NewOpenRouterProvider(provider Provider) *OpenRouterProvider {
	return &OpenRouterProvider{
		provider: provider,
		sdk:      or.NewClient(provider.GetAPIKey()),
	}
}

func (p *OpenRouterProvider) GetName() string {
	return p.provider.GetName()
}

func (p *OpenRouterProvider) GetBaseURL() string {
	return p.provider.GetBaseURL()
}

func (p *OpenRouterProvider) GetAPIKey() string {
	return p.provider.GetAPIKey()
}

func (p *OpenRouterProvider) ListModels(ctx context.Context) ([]*Model, error) {
	listed, err := p.sdk.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("list models request failed: %w", err)
	}

	models := make([]*Model, 0, len(listed))
	for _, item := range listed {
		models = append(models, &Model{Name: item.ID})
	}
	return models, nil
}

type OpenRouterCredits struct {
	Data struct {
		Label      string  `json:"label"`
		Usage      float64 `json:"usage"`
		Limit      float64 `json:"limit"`
		IsFreeTier bool    `json:"is_free_tier"`
		RateLimit  struct {
			Requests int    `json:"requests"`
			Interval string `json:"interval"`
		} `json:"rate_limit"`
	} `json:"data"`
}

func (p *OpenRouterProvider) CheckCredits(ctx context.Context) (*OpenRouterCredits, error) {
	resp, err := p.sdk.GetCurrentAPIKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("check credits request failed: %w", err)
	}

	credits := &OpenRouterCredits{}
	credits.Data.Label = resp.Data.Label
	credits.Data.Usage = resp.Data.Usage
	credits.Data.Limit = resp.Data.Limit
	credits.Data.IsFreeTier = resp.Data.IsFreeTier
	if resp.Data.RateLimit != nil {
		credits.Data.RateLimit.Requests = resp.Data.RateLimit.Requests
		credits.Data.RateLimit.Interval = resp.Data.RateLimit.Interval
	}
	return credits, nil
}
