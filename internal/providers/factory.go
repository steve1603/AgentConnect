package providers

import (
	"strings"

	"github.com/steve1603/AgentConnect/internal/config"
)

// Build returns every provider that has an API key configured, keyed by
// provider name. Per-request model overrides replace the .env default for
// that provider only.
func Build(cfg *config.Config, overrides map[string]string) map[string]Provider {
	built := make(map[string]Provider, len(config.Keys))

	for _, key := range config.Keys {
		apiKey := cfg.APIKeys[key]
		if apiKey == "" {
			continue
		}

		model := cfg.Models[key]
		if override := strings.TrimSpace(overrides[key]); override != "" {
			model = override
		}

		switch key {
		case config.OpenAI:
			built[key] = NewOpenAI(model, apiKey, cfg.RequestTimeout, cfg.ProviderRetries, cfg.MaxOutputTokens)
		case config.Anthropic:
			built[key] = NewAnthropic(model, apiKey, cfg.RequestTimeout, cfg.ProviderRetries, cfg.MaxOutputTokens)
		case config.Gemini:
			built[key] = NewGemini(model, apiKey, cfg.RequestTimeout, cfg.ProviderRetries, cfg.MaxOutputTokens)
		}
	}
	return built
}
