package opencode

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
)

// Model contains display and selection metadata only; catalog settings may
// contain provider credentials and must never be forwarded to Discord or logs.
type Model struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerID"`
	Name       string `json:"name"`
}

func (m Model) String() string { return m.ProviderID + "/" + m.ID }

var ErrModelUnavailable = errors.New("model is not an available free tool-capable model")

func (c *Client) modelLocation() string {
	return "?" + url.Values{"location[directory]": {c.directory}}.Encode()
}

func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var response struct {
		Data []struct {
			Model
			Enabled      bool   `json:"enabled"`
			Status       string `json:"status"`
			Capabilities struct {
				Tools bool `json:"tools"`
			} `json:"capabilities"`
			Cost []struct {
				Input  *float64 `json:"input"`
				Output *float64 `json:"output"`
				Cache  struct {
					Read  float64 `json:"read"`
					Write float64 `json:"write"`
				} `json:"cache"`
			} `json:"cost"`
		} `json:"data"`
	}
	if err := c.request(ctx, "GET", "/api/model"+c.modelLocation(), nil, &response); err != nil {
		return nil, err
	}
	var models []Model
	for _, m := range response.Data {
		if !m.Enabled || m.Status != "active" || !m.Capabilities.Tools || m.ID == "" || m.ProviderID == "" || len(m.Cost) == 0 {
			continue
		}
		free := true
		for _, cost := range m.Cost {
			if cost.Input == nil || cost.Output == nil || *cost.Input != 0 || *cost.Output != 0 || cost.Cache.Read != 0 || cost.Cache.Write != 0 {
				free = false
			}
		}
		if free {
			models = append(models, m.Model)
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].String() < models[j].String() })
	return models, nil
}

// SelectModel updates one bot-wide choice. An already-running Ask keeps its
// snapshot; every subsequent Ask switches its session before sending a prompt.
func (c *Client) SelectModel(ctx context.Context, selector string) (Model, error) {
	models, err := c.Models(ctx)
	if err != nil {
		return Model{}, err
	}
	selector = strings.TrimSpace(selector)
	var matches []Model
	for _, model := range models {
		if strings.EqualFold(selector, model.String()) || strings.EqualFold(selector, model.ID) || strings.EqualFold(selector, model.Name) {
			matches = append(matches, model)
		}
	}
	if len(matches) != 1 {
		return Model{}, ErrModelUnavailable
	}
	c.modelMu.Lock()
	c.selectedModel = matches[0]
	c.modelMu.Unlock()
	return matches[0], nil
}

func (c *Client) CurrentModel(ctx context.Context) (string, error) {
	c.modelMu.RLock()
	model := c.selectedModel
	c.modelMu.RUnlock()
	if model.ID != "" {
		return model.String(), nil
	}
	var response struct {
		Data *Model `json:"data"`
	}
	if err := c.request(ctx, "GET", "/api/model/default"+c.modelLocation(), nil, &response); err != nil {
		return "", err
	}
	if response.Data == nil || response.Data.ID == "" {
		return "OpenCode 기본값 (사용 가능한 모델 없음)", nil
	}
	return response.Data.String() + " (OpenCode 기본값)", nil
}
