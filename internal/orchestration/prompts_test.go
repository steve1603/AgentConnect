package orchestration_test

import (
	"strings"
	"testing"

	"github.com/steve1603/AgentConnect/internal/orchestration"
)

var (
	testAnswers = []orchestration.Answer{
		{Label: "ChatGPT", Model: "gpt-5.6", Text: "Four."},
		{Label: "Claude", Model: "claude-sonnet-5", Text: "The answer is 4."},
	}
	testReviews = []orchestration.Answer{
		{Label: "Gemini", Model: "gemini-3.7-flash", Text: "Both are correct."},
	}
)

func TestClampMarksTruncation(t *testing.T) {
	clamped := orchestration.Clamp(strings.Repeat("x", 500), 100)
	if len(clamped) != 100 {
		t.Errorf("length = %d, want 100", len(clamped))
	}
	if !strings.HasSuffix(clamped, orchestration.TruncationNote) {
		t.Error("truncated text is not marked")
	}
}

func TestClampLeavesShortTextAlone(t *testing.T) {
	if got := orchestration.Clamp("short", 100); got != "short" {
		t.Errorf("got %q, want %q", got, "short")
	}
}

func TestRenderHistoryKeepsTheNewestTurns(t *testing.T) {
	var messages []orchestration.HistoryMessage
	for i := range 10 {
		messages = append(messages, orchestration.HistoryMessage{
			Role: "user", Content: "message " + string(rune('0'+i)),
		})
	}

	rendered := orchestration.RenderHistory(messages, 3, 10000)
	if !strings.Contains(rendered, "message 9") {
		t.Error("the newest turn was dropped")
	}
	if strings.Contains(rendered, "message 0") {
		t.Error("the oldest turn should have been dropped")
	}
	if got := strings.Count(rendered, "User:"); got != 3 {
		t.Errorf("turns = %d, want 3", got)
	}
}

func TestRenderHistoryLabelsRoles(t *testing.T) {
	rendered := orchestration.RenderHistory([]orchestration.HistoryMessage{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}, 10, 10000)

	if !strings.Contains(rendered, "User: hi") || !strings.Contains(rendered, "Assistant: hello") {
		t.Errorf("roles are not labelled:\n%s", rendered)
	}
}

func TestRenderHistoryIsEmptyForNoMessages(t *testing.T) {
	if got := orchestration.RenderHistory(nil, 5, 100); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestIndependentPromptIncludesRequestAndHistory(t *testing.T) {
	prompt := orchestration.BuildIndependentPrompt("What is 2+2?", "User: earlier question", 30000)
	if !containsAll(prompt, "What is 2+2?", "earlier question") {
		t.Errorf("prompt is missing the request or history:\n%s", prompt)
	}
}

func TestReviewPromptContainsEveryAnswerAndTheReviewer(t *testing.T) {
	prompt := orchestration.BuildReviewPrompt("What is 2+2?", testAnswers, "Gemini", "", 30000)
	if !containsAll(prompt, "ChatGPT", "Claude", "The answer is 4.", "You are Gemini") {
		t.Errorf("review prompt is incomplete:\n%s", prompt)
	}
}

func TestChairPromptContainsAnswersAndReviews(t *testing.T) {
	prompt := orchestration.BuildChairPrompt("What is 2+2?", testAnswers, testReviews, "", 30000)
	if !containsAll(prompt, "Independent answers", "Peer reviews", "Both are correct.") {
		t.Errorf("chair prompt is incomplete:\n%s", prompt)
	}
}

func TestChairPromptOmitsTheReviewSectionWhenThereAreNone(t *testing.T) {
	prompt := orchestration.BuildChairPrompt("What is 2+2?", testAnswers, nil, "", 30000)
	if strings.Contains(prompt, "Peer reviews") {
		t.Error("an empty review section was rendered")
	}
	if !strings.Contains(prompt, "Independent answers") {
		t.Error("the answers section is missing")
	}
}

func TestPromptsRespectTheCharacterBudget(t *testing.T) {
	long := strings.Repeat("y", 50000)
	prompts := map[string]string{
		"independent": orchestration.BuildIndependentPrompt(long, "", 1000),
		"review":      orchestration.BuildReviewPrompt(long, testAnswers, "Claude", "", 1000),
		"chair":       orchestration.BuildChairPrompt(long, testAnswers, testReviews, "", 1000),
		"direct":      orchestration.BuildDirectPrompt(long, "", 1000),
	}
	for name, prompt := range prompts {
		if len(prompt) > 1000 {
			t.Errorf("%s prompt = %d chars, want <= 1000", name, len(prompt))
		}
	}
}

func TestNoPromptRequestsHiddenReasoning(t *testing.T) {
	corpus := strings.ToLower(strings.Join([]string{
		orchestration.IndependentSystem,
		orchestration.ReviewSystem,
		orchestration.ChairSystem,
		orchestration.DirectSystem,
	}, " "))

	for _, banned := range []string{
		"chain of thought", "chain-of-thought", "hidden reasoning", "internal reasoning",
	} {
		if strings.Contains(corpus, banned) {
			t.Errorf("a system prompt asks for %q", banned)
		}
	}
}
