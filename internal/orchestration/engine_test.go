package orchestration_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/steve1603/AgentConnect/internal/orchestration"
)

func TestFullRoundtableRunsThreePhases(t *testing.T) {
	set, fakes := fakeSet()
	engine := orchestration.New(set, testConfig())

	events, outcome, err := runTurn(engine, orchestration.Request{
		Text: "What is 2+2?", Mode: orchestration.ModeFull, Chair: "openai",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{"independent": true, "review": true, "chair": true}
	if got := phasesSeen(events); !reflect.DeepEqual(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	if outcome.FinalText != "FINAL from ChatGPT" {
		t.Errorf("final text = %q", outcome.FinalText)
	}
	if outcome.Chair != "openai" || outcome.ChairFallback {
		t.Errorf("chair = %q fallback = %v, want openai/false", outcome.Chair, outcome.ChairFallback)
	}
	if len(outcome.Answers) != 3 || len(outcome.Reviews) != 3 {
		t.Errorf("answers = %d, reviews = %d, want 3 and 3", len(outcome.Answers), len(outcome.Reviews))
	}
	if len(outcome.Failures) != 0 {
		t.Errorf("failures = %v, want none", outcome.Failures)
	}

	// Each reviewer must see every first-round answer, not just its own.
	reviewPrompt := fakes["anthropic"].callsMade()[1].prompt
	if !containsAll(reviewPrompt, "ChatGPT", "Claude", "Gemini", "You are Claude") {
		t.Errorf("review prompt is missing peers or the reviewer identity:\n%s", reviewPrompt)
	}
}

func TestFailedProviderIsIsolated(t *testing.T) {
	set, fakes := fakeSet()
	fakes["gemini"].alwaysFail = true
	engine := orchestration.New(set, testConfig())

	events, outcome, err := runTurn(engine, orchestration.Request{
		Text: "What is 2+2?", Mode: orchestration.ModeFull, Chair: "anthropic",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.FinalText != "FINAL from Claude" {
		t.Errorf("final text = %q", outcome.FinalText)
	}
	if got := okProviders(outcome.Answers); !reflect.DeepEqual(got, []string{"openai", "anthropic"}) {
		t.Errorf("successful answers = %v", got)
	}
	if len(outcome.Failures) != 1 || outcome.Failures[0].Provider != "gemini" {
		t.Errorf("failures = %+v, want one gemini failure", outcome.Failures)
	}

	// The failed provider never reached the review round, and its answer is
	// not quoted to the others.
	if calls := fakes["gemini"].callsMade(); len(calls) != 1 {
		t.Errorf("gemini calls = %d, want 1", len(calls))
	}
	if reviewPrompt := fakes["openai"].callsMade()[1].prompt; containsAll(reviewPrompt, "Gemini") {
		t.Errorf("review prompt quotes the failed provider:\n%s", reviewPrompt)
	}

	var errored []string
	for _, event := range events {
		if event.Type == "provider" && event.Status == "error" {
			errored = append(errored, event.Provider)
		}
	}
	if !reflect.DeepEqual(errored, []string{"gemini"}) {
		t.Errorf("error events = %v, want [gemini]", errored)
	}
}

func TestChairFailureFallsBackToAnotherProvider(t *testing.T) {
	set, fakes := fakeSet()
	fakes["openai"].failPhases["chair"] = true
	engine := orchestration.New(set, testConfig())

	_, outcome, err := runTurn(engine, orchestration.Request{
		Text: "What is 2+2?", Mode: orchestration.ModeFull, Chair: "openai",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.ChairRequested != "openai" || outcome.Chair != "anthropic" || !outcome.ChairFallback {
		t.Errorf("requested = %q chair = %q fallback = %v",
			outcome.ChairRequested, outcome.Chair, outcome.ChairFallback)
	}
	if outcome.FinalText != "FINAL from Claude" {
		t.Errorf("final text = %q", outcome.FinalText)
	}

	// The failed chair attempt is recorded rather than swallowed.
	var sawOpenAIFailure bool
	for _, failure := range outcome.Failures {
		if failure.Provider == "openai" {
			sawOpenAIFailure = true
		}
	}
	if !sawOpenAIFailure {
		t.Error("the failed chair attempt is missing from the trace")
	}
}

func TestAllChairsFailingReturnsTheStrongestAnswer(t *testing.T) {
	set, fakes := fakeSet()
	for _, fake := range fakes {
		fake.failPhases["chair"] = true
	}
	engine := orchestration.New(set, testConfig())

	events, outcome, err := runTurn(engine, orchestration.Request{
		Text: "What is 2+2?", Mode: orchestration.ModeFull, Chair: "openai",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.FinalText != "ChatGPT says hello." {
		t.Errorf("final text = %q, want the first successful answer", outcome.FinalText)
	}
	if outcome.Chair != "" || !outcome.ChairFallback {
		t.Errorf("chair = %q fallback = %v, want empty/true", outcome.Chair, outcome.ChairFallback)
	}

	var sawFailedPhase bool
	for _, event := range events {
		if event.Type == "phase" && event.Phase == "chair" && event.Status == "failed" {
			sawFailedPhase = true
		}
	}
	if !sawFailedPhase {
		t.Error("no chair-failed event was emitted")
	}
}

func TestPanelModeSkipsPeerReview(t *testing.T) {
	set, _ := fakeSet()
	engine := orchestration.New(set, testConfig())

	events, outcome, err := runTurn(engine, orchestration.Request{
		Text: "What is 2+2?", Mode: orchestration.ModePanel, Chair: "gemini",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{"independent": true, "chair": true}
	if got := phasesSeen(events); !reflect.DeepEqual(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}
	if len(outcome.Reviews) != 0 {
		t.Errorf("reviews = %d, want 0", len(outcome.Reviews))
	}
	if outcome.Chair != "gemini" || outcome.FinalText != "FINAL from Gemini" {
		t.Errorf("chair = %q final = %q", outcome.Chair, outcome.FinalText)
	}
}

func TestDirectModeUsesOneProvider(t *testing.T) {
	set, fakes := fakeSet()
	engine := orchestration.New(set, testConfig())

	_, outcome, err := runTurn(engine, orchestration.Request{
		Text: "What is 2+2?", Mode: orchestration.ModeDirect, Chair: "anthropic",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if outcome.Mode != orchestration.ModeDirect || outcome.FinalText != "Claude says hello." {
		t.Errorf("mode = %q final = %q", outcome.Mode, outcome.FinalText)
	}
	if len(fakes["anthropic"].callsMade()) != 1 {
		t.Errorf("anthropic calls = %d, want 1", len(fakes["anthropic"].callsMade()))
	}
	if len(fakes["openai"].callsMade()) != 0 || len(fakes["gemini"].callsMade()) != 0 {
		t.Error("direct mode called providers other than the selected one")
	}
}

func TestReviewIsSkippedWhenOnlyOneProviderAnswers(t *testing.T) {
	set, fakes := fakeSet()
	fakes["anthropic"].alwaysFail = true
	fakes["gemini"].alwaysFail = true
	engine := orchestration.New(set, testConfig())

	events, outcome, err := runTurn(engine, orchestration.Request{
		Text: "What is 2+2?", Mode: orchestration.ModeFull, Chair: "openai",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var skipped int
	for _, event := range events {
		if event.Type == "phase" && event.Status == "skipped" {
			skipped++
		}
	}
	if skipped != 1 {
		t.Errorf("skipped events = %d, want 1", skipped)
	}
	if len(outcome.Reviews) != 0 || outcome.FinalText != "FINAL from ChatGPT" {
		t.Errorf("reviews = %d final = %q", len(outcome.Reviews), outcome.FinalText)
	}
}

func TestTurnFailsWhenEveryProviderFails(t *testing.T) {
	set, fakes := fakeSet()
	for _, fake := range fakes {
		fake.alwaysFail = true
	}
	engine := orchestration.New(set, testConfig())

	if _, _, err := runTurn(engine, orchestration.Request{Text: "hi", Mode: orchestration.ModeFull}); !errors.Is(err, orchestration.ErrNoAnswer) {
		t.Fatalf("error = %v, want ErrNoAnswer", err)
	}
}

func TestNoProvidersConfiguredIsAnError(t *testing.T) {
	engine := orchestration.New(nil, testConfig())
	if _, _, err := runTurn(engine, orchestration.Request{Text: "hi"}); err == nil {
		t.Fatal("expected an error when no providers are configured")
	}
}

func TestHistoryReachesTheProviders(t *testing.T) {
	set, fakes := fakeSet()
	engine := orchestration.New(set, testConfig())

	_, _, err := runTurn(engine, orchestration.Request{
		Text: "What is my name?",
		History: []orchestration.HistoryMessage{
			{Role: "user", Content: "My name is Ada."},
			{Role: "assistant", Content: "Nice to meet you, Ada."},
		},
		Mode: orchestration.ModePanel,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	prompt := fakes["openai"].callsMade()[0].prompt
	if !containsAll(prompt, "My name is Ada.", "What is my name?") {
		t.Errorf("history did not reach the prompt:\n%s", prompt)
	}
}

func TestUnknownModeFallsBackToFull(t *testing.T) {
	set, _ := fakeSet()
	engine := orchestration.New(set, testConfig())

	_, outcome, err := runTurn(engine, orchestration.Request{Text: "hi", Mode: "nonsense"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Mode != orchestration.ModeFull {
		t.Errorf("mode = %q, want full", outcome.Mode)
	}
}

func TestUnknownChairFallsBackToTheDefault(t *testing.T) {
	set, _ := fakeSet()
	engine := orchestration.New(set, testConfig())

	_, outcome, err := runTurn(engine, orchestration.Request{
		Text: "hi", Mode: orchestration.ModePanel, Chair: "nonexistent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Chair != "openai" {
		t.Errorf("chair = %q, want the configured default", outcome.Chair)
	}
}

func TestProvidersRunInParallel(t *testing.T) {
	set, fakes := fakeSet()
	for _, fake := range fakes {
		fake.delay = 150 * time.Millisecond
	}
	engine := orchestration.New(set, testConfig())

	started := time.Now()
	if _, _, err := runTurn(engine, orchestration.Request{
		Text: "hi", Mode: orchestration.ModePanel, Chair: "openai",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	elapsed := time.Since(started)

	// Three parallel answers plus one sequential chair call is ~2 delays;
	// running them serially would take at least 4.
	if elapsed > 3*150*time.Millisecond {
		t.Errorf("elapsed = %s, want the first round to run in parallel", elapsed)
	}
}
