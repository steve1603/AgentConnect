package orchestration_test

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/steve1603/AgentConnect/internal/config"
	"github.com/steve1603/AgentConnect/internal/orchestration"
	"github.com/steve1603/AgentConnect/internal/providers"
)

// fakeProvider returns canned text, or fails, without any network I/O, so the
// tests never spend API credits.
type fakeProvider struct {
	key        string
	label      string
	model      string
	reply      string
	failPhases map[string]bool
	alwaysFail bool
	delay      time.Duration

	mu    sync.Mutex
	calls []call
}

type call struct {
	system string
	prompt string
}

func newFake(key, label string) *fakeProvider {
	return &fakeProvider{
		key:        key,
		label:      label,
		model:      key + "-model",
		reply:      label + " says hello.",
		failPhases: map[string]bool{},
	}
}

func (f *fakeProvider) Key() string   { return f.key }
func (f *fakeProvider) Label() string { return f.label }
func (f *fakeProvider) Model() string { return f.model }

func (f *fakeProvider) Complete(ctx context.Context, system, prompt string) providers.Result {
	f.mu.Lock()
	f.calls = append(f.calls, call{system: system, prompt: prompt})
	f.mu.Unlock()

	if f.delay > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(f.delay):
		}
	}

	result := providers.Result{
		Provider: f.key,
		Label:    f.label,
		Model:    f.model,
		Attempts: 1,
	}

	phase := phaseOf(system)
	if f.alwaysFail || f.failPhases[phase] {
		result.Error = f.label + " failed during " + phase
		return result
	}

	result.OK = true
	switch phase {
	case "review":
		result.Text = f.label + " review: the drafts broadly agree."
	case "chair":
		result.Text = "FINAL from " + f.label
	default:
		result.Text = f.reply
	}
	return result
}

// callsMade returns a copy of the calls this provider received.
func (f *fakeProvider) callsMade() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

// phaseOf identifies the phase from the system prompt the engine passed in.
func phaseOf(system string) string {
	switch system {
	case orchestration.ReviewSystem:
		return "review"
	case orchestration.ChairSystem:
		return "chair"
	case orchestration.DirectSystem:
		return "direct"
	default:
		return "independent"
	}
}

func testConfig() *config.Config {
	return &config.Config{
		Models:              map[string]string{},
		APIKeys:             map[string]string{"openai": "x", "anthropic": "x", "gemini": "x"},
		RequestTimeout:      time.Second,
		ProviderRetries:     0,
		MaxPromptChars:      30000,
		MaxOutputTokens:     4096,
		HistoryMessageLimit: 24,
		HistoryCharLimit:    60000,
		DefaultChair:        config.OpenAI,
	}
}

func fakeSet() (map[string]providers.Provider, map[string]*fakeProvider) {
	openai := newFake("openai", "ChatGPT")
	anthropic := newFake("anthropic", "Claude")
	gemini := newFake("gemini", "Gemini")

	byKey := map[string]*fakeProvider{"openai": openai, "anthropic": anthropic, "gemini": gemini}
	set := map[string]providers.Provider{"openai": openai, "anthropic": anthropic, "gemini": gemini}
	return set, byKey
}

// runTurn drains the engine's events, returning them alongside the outcome.
func runTurn(
	engine *orchestration.Engine, req orchestration.Request,
) ([]orchestration.Event, *orchestration.Outcome, error) {
	var events []orchestration.Event
	outcome, err := engine.Run(context.Background(), req, func(event orchestration.Event) {
		events = append(events, event)
	})
	return events, outcome, err
}

func phasesSeen(events []orchestration.Event) map[string]bool {
	seen := map[string]bool{}
	for _, event := range events {
		if event.Type == "provider" {
			seen[event.Phase] = true
		}
	}
	return seen
}

func okProviders(results []providers.Result) []string {
	var keys []string
	for _, result := range results {
		if result.OK {
			keys = append(keys, result.Provider)
		}
	}
	return keys
}

func containsAll(text string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			return false
		}
	}
	return true
}
