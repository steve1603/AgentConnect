// Package config loads runtime settings from .env and the environment.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Provider keys, in the order they are displayed and iterated.
const (
	OpenAI    = "openai"
	Anthropic = "anthropic"
	Gemini    = "gemini"
)

// Keys lists every supported provider in display order.
var Keys = []string{OpenAI, Anthropic, Gemini}

// Labels maps a provider key to the name shown in the UI.
var Labels = map[string]string{
	OpenAI:    "ChatGPT",
	Anthropic: "Claude",
	Gemini:    "Gemini",
}

// Config holds everything the app reads at startup. API keys live here and
// are never included in any HTTP response.
type Config struct {
	APIKeys map[string]string
	Models  map[string]string

	Host         string
	Port         int
	DatabasePath string
	LogLevel     string
	OpenBrowser  bool

	RequestTimeout      time.Duration
	ProviderRetries     int
	MaxPromptChars      int
	MaxOutputTokens     int
	HistoryMessageLimit int
	HistoryCharLimit    int
	DefaultChair        string
}

// lookup resolves one setting. The process environment wins over .env, so a
// value exported in the shell always overrides the file.
type lookup struct {
	file map[string]string
}

func (l lookup) str(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	if value, ok := l.file[key]; ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func (l lookup) num(key string, fallback int) int {
	if value, err := strconv.Atoi(l.str(key, "")); err == nil {
		return value
	}
	return fallback
}

func (l lookup) flag(key string, fallback bool) bool {
	if value, err := strconv.ParseBool(l.str(key, "")); err == nil {
		return value
	}
	return fallback
}

// Load reads envPath (if present) and merges it with the environment. It does
// not mutate the process environment.
func Load(envPath string) (*Config, error) {
	file, err := readDotEnv(envPath)
	if err != nil {
		return nil, err
	}
	get := lookup{file: file}

	cfg := &Config{
		APIKeys: map[string]string{
			OpenAI:    get.str("OPENAI_API_KEY", ""),
			Anthropic: get.str("ANTHROPIC_API_KEY", ""),
			Gemini:    get.str("GEMINI_API_KEY", ""),
		},
		Models: map[string]string{
			OpenAI:    get.str("OPENAI_MODEL", "gpt-5.6"),
			Anthropic: get.str("ANTHROPIC_MODEL", "claude-sonnet-5"),
			Gemini:    get.str("GEMINI_MODEL", "gemini-3.7-flash"),
		},
		Host:                get.str("APP_HOST", "127.0.0.1"),
		Port:                get.num("APP_PORT", 8000),
		DatabasePath:        get.str("DATABASE_PATH", "data/roundtable.db"),
		LogLevel:            get.str("LOG_LEVEL", "info"),
		OpenBrowser:         get.flag("OPEN_BROWSER", true),
		RequestTimeout:      time.Duration(get.num("REQUEST_TIMEOUT_SECONDS", 120)) * time.Second,
		ProviderRetries:     get.num("PROVIDER_RETRIES", 2),
		MaxPromptChars:      get.num("MAX_PROMPT_CHARS", 30000),
		MaxOutputTokens:     get.num("MAX_OUTPUT_TOKENS", 16000),
		HistoryMessageLimit: get.num("HISTORY_MESSAGE_LIMIT", 24),
		HistoryCharLimit:    get.num("HISTORY_CHAR_LIMIT", 60000),
		DefaultChair:        get.str("DEFAULT_CHAIR", OpenAI),
	}

	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("APP_PORT must be between 1 and 65535, got %d", cfg.Port)
	}
	if cfg.ProviderRetries < 0 || cfg.ProviderRetries > 5 {
		return nil, fmt.Errorf("PROVIDER_RETRIES must be between 0 and 5, got %d", cfg.ProviderRetries)
	}
	return cfg, nil
}

// Configured lists the providers that have an API key.
func (c *Config) Configured() []string {
	var keys []string
	for _, key := range Keys {
		if c.APIKeys[key] != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

// Ready reports whether at least one provider can be called.
func (c *Config) Ready() bool { return len(c.Configured()) > 0 }

// ResolvedChair returns the configured default chair, falling back to the
// first provider that actually has a key.
func (c *Config) ResolvedChair() string {
	configured := c.Configured()
	for _, key := range configured {
		if key == c.DefaultChair {
			return c.DefaultChair
		}
	}
	if len(configured) > 0 {
		return configured[0]
	}
	return c.DefaultChair
}

// Address is the host:port the server listens on.
func (c *Config) Address() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }

// readDotEnv parses KEY=VALUE lines. A missing file is not an error.
func readDotEnv(path string) (map[string]string, error) {
	values := map[string]string{}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return values, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		values[strings.TrimSpace(key)] = unquote(strings.TrimSpace(value))
	}
	return values, scanner.Err()
}

// unquote strips a matching pair of quotes wrapping the whole value.
func unquote(value string) string {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}
	return value
}
