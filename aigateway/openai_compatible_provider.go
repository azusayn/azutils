package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

type OpenAICompatibleProvider struct {
	BaseProvider
}

func NewOpenAICompatibleProvider(base BaseProvider) *OpenAICompatibleProvider {
	return &OpenAICompatibleProvider{BaseProvider: base}
}

type ListModelsResponse struct {
	Data []openAIModel `json:"data"`
}

type openAIModel struct {
	ID string `json:"id"`
}

func (p *OpenAICompatibleProvider) ListModels(ctx context.Context) ([]*Model, error) {
	if p.BaseURL == "" {
		return nil, errors.New("list models requires a base URL")
	}
	endpoint, err := url.JoinPath(p.BaseURL, "models")
	if err != nil {
		return nil, fmt.Errorf("list models URL is invalid: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("list models request could not be created: %w", err)
	}
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list models returned status %d", resp.StatusCode)
	}

	var listModelsResp ListModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&listModelsResp); err != nil {
		return nil, fmt.Errorf("list models response is invalid: %w", err)
	}

	models := make([]*Model, 0, len(listModelsResp.Data))
	for _, item := range listModelsResp.Data {
		models = append(models, &Model{Name: item.ID})
	}
	return models, nil
}
