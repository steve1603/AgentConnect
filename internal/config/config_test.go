package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steve1603/AgentConnect/internal/config"
)

func writeEnv(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	return path
}

func TestLoadReadsDotEnv(t *testing.T) {
	path := writeEnv(t, `
# a comment
OPENAI_API_KEY=sk-from-file
ANTHROPIC_MODEL=claude-sonnet-5
APP_PORT=9001
OPEN_BROWSER=false
PROVIDER_RETRIES=1
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.APIKeys[config.OpenAI] != "sk-from-file" {
		t.Errorf("openai key = %q", cfg.APIKeys[config.OpenAI])
	}
	if cfg.Models[config.Anthropic] != "claude-sonnet-5" {
		t.Errorf("anthropic model = %q", cfg.Models[config.Anthropic])
	}
	if cfg.Port != 9001 || cfg.OpenBrowser || cfg.ProviderRetries != 1 {
		t.Errorf("port = %d openBrowser = %v retries = %d", cfg.Port, cfg.OpenBrowser, cfg.ProviderRetries)
	}
	// Unset values fall back to their defaults.
	if cfg.Models[config.Gemini] != "gemini-3.7-flash" || cfg.Host != "127.0.0.1" {
		t.Errorf("defaults not applied: model = %q host = %q", cfg.Models[config.Gemini], cfg.Host)
	}
}

func TestEnvironmentWinsOverDotEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-from-environment")
	path := writeEnv(t, "OPENAI_API_KEY=sk-from-file\n")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.APIKeys[config.OpenAI] != "sk-from-environment" {
		t.Errorf("key = %q, want the environment value", cfg.APIKeys[config.OpenAI])
	}
}

func TestQuotedValuesAreUnwrapped(t *testing.T) {
	path := writeEnv(t, "GEMINI_API_KEY=\"quoted-key\"\nOPENAI_MODEL='gpt-5.6'\n")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.APIKeys[config.Gemini] != "quoted-key" || cfg.Models[config.OpenAI] != "gpt-5.6" {
		t.Errorf("key = %q model = %q", cfg.APIKeys[config.Gemini], cfg.Models[config.OpenAI])
	}
}

func TestMissingDotEnvIsNotAnError(t *testing.T) {
	if _, err := config.Load(filepath.Join(t.TempDir(), "absent.env")); err != nil {
		t.Errorf("load: %v", err)
	}
}

func TestInvalidPortIsRejected(t *testing.T) {
	path := writeEnv(t, "APP_PORT=70000\n")
	if _, err := config.Load(path); err == nil {
		t.Error("expected an error for an out-of-range port")
	}
}

func TestConfiguredAndResolvedChair(t *testing.T) {
	path := writeEnv(t, "ANTHROPIC_API_KEY=x\nGEMINI_API_KEY=y\nDEFAULT_CHAIR=openai\n")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if got := cfg.Configured(); len(got) != 2 || got[0] != config.Anthropic || got[1] != config.Gemini {
		t.Errorf("configured = %v", got)
	}
	if !cfg.Ready() {
		t.Error("Ready() = false, want true")
	}
	// The default chair has no key, so the first configured provider is used.
	if got := cfg.ResolvedChair(); got != config.Anthropic {
		t.Errorf("resolved chair = %q, want %q", got, config.Anthropic)
	}
}

func TestNotReadyWithoutAnyKeys(t *testing.T) {
	path := writeEnv(t, "APP_PORT=8000\n")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Ready() || len(cfg.Configured()) != 0 {
		t.Errorf("configured = %v, ready = %v", cfg.Configured(), cfg.Ready())
	}
}
