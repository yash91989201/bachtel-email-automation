package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// DiscoverModels fetches the model list of the selected preset with the
// supplied key and endpoint. It performs one bounded GET, returns every
// syntactically valid listed identifier as a selectable choice and never
// writes a connection, stages a proposal, validates compatibility or starts
// work.
func (s *Store) DiscoverModels(ctx context.Context, ownerID string, proposal Proposal) ([]ModelOption, error) {
	if strings.TrimSpace(ownerID) == "" {
		return nil, fmt.Errorf("%w: a connection owner is required", ErrInvalidSetup)
	}
	cfg, apiKey, err := discoveryProposal(proposal, s.opts.AllowPrivate)
	if err != nil {
		return nil, err
	}
	target, err := modelListURL(cfg, s.opts.AllowPrivate)
	if err != nil {
		return nil, err
	}
	raw, err := fetchJSON(ctx, s.opts, target, apiKey)
	if err != nil {
		return nil, err
	}
	return parseModelOptions(raw)
}

// discoveryProposal checks the discovery inputs. A model and format are not
// required: fetching a list must work before the operator chooses one.
func discoveryProposal(proposal Proposal, allowPrivate bool) (Config, string, error) {
	cfg := proposal.Config
	cfg.Provider = Provider(strings.TrimSpace(string(cfg.Provider)))
	cfg.APIRoot = strings.TrimRight(strings.TrimSpace(cfg.APIRoot), "/")
	if cfg.Provider == ProviderCustom {
		return Config{}, "", fmt.Errorf("%w: custom endpoints are configured manually", ErrInvalidSetup)
	}
	preset, ok := PresetFor(cfg.Provider)
	if !ok {
		return Config{}, "", fmt.Errorf("%w: choose a supported provider", ErrInvalidSetup)
	}
	if cfg.APIRoot == "" {
		cfg.APIRoot = preset.DefaultRoot
	}
	if cfg.APIRoot == "" {
		return Config{}, "", fmt.Errorf("%w: an API root is required", ErrInvalidSetup)
	}
	if _, err := apiRoot(cfg, allowPrivate); err != nil {
		return Config{}, "", err
	}
	apiKey := strings.TrimSpace(proposal.APIKey)
	if apiKey == "" || len(apiKey) > maxAPIKeyLength || strings.IndexFunc(apiKey, unicode.IsControl) >= 0 {
		return Config{}, "", fmt.Errorf("%w: enter the API key", ErrInvalidSetup)
	}
	return cfg, apiKey, nil
}

// modelListURL derives the documented model-list URL from the configured
// root. The OpenCode Go default root ends in /openai/v1 and lists models at
// /v1/models; every other root, including overrides and Xiaomi, appends
// /models. An override therefore never falls back to the official host.
func modelListURL(cfg Config, allowPrivate bool) (string, error) {
	base, err := apiRoot(cfg, allowPrivate)
	if err != nil {
		return "", err
	}
	if parent, ok := strings.CutSuffix(base, "/openai/v1"); ok {
		return parent + "/v1/models", nil
	}
	return base + "/models", nil
}

// parseModelOptions reads the documented OpenAI-style data list. Malformed
// entries are skipped and identifiers are deduplicated in response order.
// Every remaining identifier is an unvalidated choice: an empty list is a
// fact, not an error.
func parseModelOptions(raw []byte) ([]ModelOption, error) {
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Data == nil {
		return nil, ErrDiscoveryUnavailable
	}
	options := make([]ModelOption, 0, len(payload.Data))
	seen := make(map[string]struct{}, len(payload.Data))
	for _, entry := range payload.Data {
		id := strings.TrimSpace(entry.ID)
		if !validModelID(id) {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		options = append(options, ModelOption{ID: id})
	}
	return options, nil
}
