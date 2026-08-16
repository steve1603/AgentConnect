// Package orchestration runs one roundtable turn across the configured
// providers.
//
// A full turn has three phases:
//
//  1. Independent - every provider answers in parallel, blind to the others.
//  2. Peer review - every provider that succeeded reviews all the answers.
//  3. Chair - the selected provider writes the single final answer.
//
// A provider that fails is isolated: the turn continues with whoever is left.
// If the chair itself fails, another successful provider takes over.
package orchestration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/steve1603/AgentConnect/internal/config"
	"github.com/steve1603/AgentConnect/internal/providers"
)

// Turn modes.
const (
	ModeFull   = "full"
	ModePanel  = "panel"
	ModeDirect = "direct"
)

// Modes lists every supported mode, in display order.
var Modes = []string{ModeFull, ModePanel, ModeDirect}

// ErrNoAnswer means no provider produced anything usable for this turn.
var ErrNoAnswer = errors.New("no provider produced an answer")

// Event is one progress update emitted while a turn runs. Only the fields
// relevant to a given event type are populated.
type Event struct {
	Type      string   `json:"type"`
	Phase     string   `json:"phase,omitempty"`
	Status    string   `json:"status,omitempty"`
	Provider  string   `json:"provider,omitempty"`
	Label     string   `json:"label,omitempty"`
	Model     string   `json:"model,omitempty"`
	LatencyMS int64    `json:"latency_ms,omitempty"`
	Attempts  int      `json:"attempts,omitempty"`
	Error     string   `json:"error,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	Providers []string `json:"providers,omitempty"`
	Succeeded []string `json:"succeeded,omitempty"`
	Result    *Outcome `json:"result,omitempty"`
	Message   string   `json:"message,omitempty"`
	MessageID int64    `json:"message_id,omitempty"`
}

// Outcome is everything a completed turn produced, including the full trace.
type Outcome struct {
	Mode           string             `json:"mode"`
	FinalText      string             `json:"final_text"`
	Chair          string             `json:"chair,omitempty"`
	ChairRequested string             `json:"chair_requested,omitempty"`
	ChairFallback  bool               `json:"chair_fallback"`
	Answers        []providers.Result `json:"answers"`
	Reviews        []providers.Result `json:"reviews"`
	ChairResult    *providers.Result  `json:"chair_result,omitempty"`
	Failures       []providers.Result `json:"failures"`
}

// Request describes one turn.
type Request struct {
	Text    string
	History []HistoryMessage
	Mode    string
	Chair   string
}

// Engine runs turns against a fixed set of providers.
type Engine struct {
	providers map[string]providers.Provider
	cfg       *config.Config
}

// New builds an engine over the given providers.
func New(p map[string]providers.Provider, cfg *config.Config) *Engine {
	return &Engine{providers: p, cfg: cfg}
}

// Run executes one turn, passing every progress event to emit. It returns the
// outcome, or an error if the turn could not produce an answer at all.
func (e *Engine) Run(ctx context.Context, req Request, emit func(Event)) (*Outcome, error) {
	if len(e.providers) == 0 {
		return nil, errors.New("no providers are configured; add at least one API key to .env")
	}

	mode := req.Mode
	if mode != ModeFull && mode != ModePanel && mode != ModeDirect {
		mode = ModeFull
	}

	chair := req.Chair
	if _, ok := e.providers[chair]; !ok {
		chair = e.cfg.ResolvedChair()
	}
	if _, ok := e.providers[chair]; !ok {
		chair = e.order()[0]
	}

	history := RenderHistory(req.History, e.cfg.HistoryMessageLimit, e.cfg.HistoryCharLimit)
	maxChars := e.cfg.MaxPromptChars

	if mode == ModeDirect {
		return e.runDirect(ctx, req.Text, history, chair, maxChars, emit)
	}

	// -- Phase 1: independent answers -------------------------------------
	order := e.order()
	emit(Event{Type: "phase", Phase: "independent", Status: "start", Providers: order})

	prompt := BuildIndependentPrompt(req.Text, history, maxChars)
	jobs := make(map[string]job, len(order))
	for _, key := range order {
		jobs[key] = job{system: IndependentSystem, prompt: prompt}
	}
	answers := e.runParallel(ctx, "independent", order, jobs, emit)

	succeeded := successesInOrder(answers)
	emit(Event{
		Type: "phase", Phase: "independent", Status: "done",
		Succeeded: keysOf(succeeded),
	})

	if len(succeeded) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoAnswer, describeFailures(answers))
	}

	// -- Phase 2: peer review ---------------------------------------------
	var reviews []providers.Result
	switch {
	case mode == ModeFull && len(succeeded) > 1:
		reviewers := keysOf(succeeded)
		emit(Event{Type: "phase", Phase: "review", Status: "start", Providers: reviewers})

		payload := toAnswers(succeeded)
		reviewJobs := make(map[string]job, len(reviewers))
		for _, key := range reviewers {
			reviewJobs[key] = job{
				system: ReviewSystem,
				prompt: BuildReviewPrompt(req.Text, payload, e.providers[key].Label(), history, maxChars),
			}
		}
		reviews = e.runParallel(ctx, "review", reviewers, reviewJobs, emit)

		emit(Event{
			Type: "phase", Phase: "review", Status: "done",
			Succeeded: keysOf(successesInOrder(reviews)),
		})
	case mode == ModeFull:
		emit(Event{
			Type: "phase", Phase: "review", Status: "skipped",
			Reason: "only one provider produced an answer",
		})
	}

	// -- Phase 3: chair synthesis -----------------------------------------
	chairPrompt := BuildChairPrompt(
		req.Text, toAnswers(succeeded), toAnswers(successesInOrder(reviews)), history, maxChars,
	)

	var (
		chairResult   *providers.Result
		chairUsed     string
		chairAttempts []providers.Result
	)

	for _, candidate := range chairCandidates(chair, succeeded) {
		provider := e.providers[candidate]
		emit(Event{
			Type: "provider", Phase: "chair", Provider: candidate,
			Label: provider.Label(), Model: provider.Model(), Status: "running",
		})

		result := provider.Complete(ctx, ChairSystem, chairPrompt)
		chairAttempts = append(chairAttempts, result)
		emit(providerEvent("chair", result))

		if result.OK {
			chairResult = &result
			chairUsed = candidate
			break
		}
	}

	outcome := &Outcome{
		Mode:           mode,
		ChairRequested: chair,
		Answers:        answers,
		Reviews:        reviews,
		Failures:       failuresOf(answers, reviews, chairAttempts),
	}

	if chairResult == nil {
		// Every chair candidate failed. Return the strongest single answer
		// rather than losing the turn.
		emit(Event{
			Type: "phase", Phase: "chair", Status: "failed",
			Reason: "no provider could synthesise; returning the strongest single answer",
		})
		outcome.FinalText = succeeded[0].Text
		outcome.ChairFallback = true
	} else {
		outcome.FinalText = chairResult.Text
		outcome.Chair = chairUsed
		outcome.ChairResult = chairResult
		outcome.ChairFallback = chairUsed != chair
	}

	emit(Event{Type: "complete", Result: outcome})
	return outcome, nil
}

func (e *Engine) runDirect(
	ctx context.Context, request, history, key string, maxChars int, emit func(Event),
) (*Outcome, error) {
	provider := e.providers[key]
	emit(Event{
		Type: "provider", Phase: "direct", Provider: key,
		Label: provider.Label(), Model: provider.Model(), Status: "running",
	})

	result := provider.Complete(ctx, DirectSystem, BuildDirectPrompt(request, history, maxChars))
	emit(providerEvent("direct", result))

	if !result.OK {
		return nil, fmt.Errorf("%w: %s failed: %s", ErrNoAnswer, result.Label, result.Error)
	}

	outcome := &Outcome{
		Mode:           ModeDirect,
		FinalText:      result.Text,
		Chair:          key,
		ChairRequested: key,
		Answers:        []providers.Result{result},
		Reviews:        []providers.Result{},
		Failures:       []providers.Result{},
	}
	emit(Event{Type: "complete", Result: outcome})
	return outcome, nil
}

type job struct {
	system string
	prompt string
}

// runParallel calls several providers concurrently, emitting an event as each
// one starts and again as each one lands. Results come back in the stable
// order given by keys, not in completion order.
func (e *Engine) runParallel(
	ctx context.Context, phase string, keys []string, jobs map[string]job, emit func(Event),
) []providers.Result {
	for _, key := range keys {
		provider := e.providers[key]
		emit(Event{
			Type: "provider", Phase: phase, Provider: key,
			Label: provider.Label(), Model: provider.Model(), Status: "running",
		})
	}

	type landed struct {
		index  int
		result providers.Result
	}

	landings := make(chan landed, len(keys))
	var wg sync.WaitGroup
	for index, key := range keys {
		wg.Add(1)
		go func(index int, key string) {
			defer wg.Done()
			spec := jobs[key]
			landings <- landed{index: index, result: e.providers[key].Complete(ctx, spec.system, spec.prompt)}
		}(index, key)
	}
	go func() {
		wg.Wait()
		close(landings)
	}()

	results := make([]providers.Result, len(keys))
	for l := range landings {
		results[l.index] = l.result
		emit(providerEvent(phase, l.result))
	}
	return results
}

// order lists the configured providers in canonical display order.
func (e *Engine) order() []string {
	var keys []string
	for _, key := range config.Keys {
		if _, ok := e.providers[key]; ok {
			keys = append(keys, key)
		}
	}
	return keys
}

func providerEvent(phase string, result providers.Result) Event {
	status := "ok"
	if !result.OK {
		status = "error"
	}
	return Event{
		Type:      "provider",
		Phase:     phase,
		Provider:  result.Provider,
		Label:     result.Label,
		Model:     result.Model,
		Status:    status,
		LatencyMS: result.LatencyMS,
		Attempts:  result.Attempts,
		Error:     result.Error,
	}
}

// successesInOrder keeps the successful results in canonical provider order.
func successesInOrder(results []providers.Result) []providers.Result {
	byKey := make(map[string]providers.Result, len(results))
	for _, result := range results {
		if result.OK {
			byKey[result.Provider] = result
		}
	}
	var ordered []providers.Result
	for _, key := range config.Keys {
		if result, ok := byKey[key]; ok {
			ordered = append(ordered, result)
		}
	}
	return ordered
}

func keysOf(results []providers.Result) []string {
	keys := make([]string, 0, len(results))
	for _, result := range results {
		keys = append(keys, result.Provider)
	}
	return keys
}

func toAnswers(results []providers.Result) []Answer {
	answers := make([]Answer, 0, len(results))
	for _, result := range results {
		answers = append(answers, Answer{Label: result.Label, Model: result.Model, Text: result.Text})
	}
	return answers
}

func failuresOf(groups ...[]providers.Result) []providers.Result {
	failures := []providers.Result{}
	for _, group := range groups {
		for _, result := range group {
			if !result.OK {
				failures = append(failures, result)
			}
		}
	}
	return failures
}

// chairCandidates is the chair to try first, then the fallbacks. A requested
// chair that already failed this turn is demoted behind the providers that
// answered, but is still tried, since its failure may be transient.
func chairCandidates(requested string, succeeded []providers.Result) []string {
	var others []string
	requestedSucceeded := false
	for _, result := range succeeded {
		if result.Provider == requested {
			requestedSucceeded = true
			continue
		}
		others = append(others, result.Provider)
	}
	if requestedSucceeded {
		return append([]string{requested}, others...)
	}
	return append(others, requested)
}

func describeFailures(results []providers.Result) string {
	parts := make([]string, 0, len(results))
	for _, result := range results {
		if !result.OK {
			parts = append(parts, result.Label+": "+result.Error)
		}
	}
	return strings.Join(parts, "; ")
}
