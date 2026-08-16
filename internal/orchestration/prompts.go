package orchestration

import (
	"fmt"
	"strings"
)

// TruncationNote marks where a prompt was cut so the model knows the text is
// incomplete rather than simply short.
const TruncationNote = "\n\n[...truncated for length...]"

// System prompts for each phase. None of them requests a provider's hidden
// chain-of-thought: the roundtable collaborates on visible output only.
const (
	IndependentSystem = "You are one member of a three-model roundtable alongside two other AI " +
		"assistants. Answer the user's request directly, completely, and in your own voice. " +
		"You cannot see the other members' answers yet, so do not speculate about them. State " +
		"your reasoning and your assumptions in the answer itself, flag genuine uncertainty " +
		"rather than hiding it, and do not pad the response."

	ReviewSystem = "You are reviewing draft answers written independently by the members of an AI " +
		"roundtable, one of which is your own. Be a rigorous, specific critic: name factual " +
		"errors, missing requirements, unstated assumptions, and points where the drafts " +
		"genuinely disagree, and say which position is better supported and why. Credit stronger " +
		"ideas explicitly, including ones that are not yours. Do not rewrite the full answer and " +
		"do not summarise the drafts back; produce only the critique."

	ChairSystem = "You are the chair of an AI roundtable. You receive the user's request, each " +
		"member's independent answer, and each member's peer review. Produce the single final " +
		"answer the user will read. Resolve disagreements on the merits rather than by splitting " +
		"the difference, correct errors the reviews identified, and keep the strongest material " +
		"from any member. Write the answer directly to the user: do not mention the roundtable, " +
		"the members, the reviews, or this process, and do not describe your own selection " +
		"process. Where the members genuinely disagree on something consequential and the " +
		"evidence does not settle it, say so plainly as part of the answer."

	DirectSystem = "You are a helpful, accurate assistant. Answer the user's request directly " +
		"and completely."
)

// HistoryMessage is one prior turn, as stored and as rendered into prompts.
type HistoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Answer is one member's contribution, formatted into a later phase's prompt.
type Answer struct {
	Label string
	Model string
	Text  string
}

// Clamp trims text to at most limit characters, marking the cut.
func Clamp(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	keep := limit - len(TruncationNote)
	if keep < 0 {
		keep = 0
	}
	return text[:keep] + TruncationNote
}

// RenderHistory turns prior messages into a transcript. The tail of a
// conversation matters most, so both limits drop the oldest turns first.
func RenderHistory(messages []HistoryMessage, messageLimit, charLimit int) string {
	if len(messages) == 0 {
		return ""
	}
	if messageLimit > 0 && len(messages) > messageLimit {
		messages = messages[len(messages)-messageLimit:]
	}

	var lines []string
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		speaker := "Assistant"
		if message.Role == "user" {
			speaker = "User"
		}
		lines = append(lines, speaker+": "+content)
	}
	if len(lines) == 0 {
		return ""
	}

	transcript := strings.Join(lines, "\n\n")
	if charLimit > 0 && len(transcript) > charLimit {
		transcript = strings.TrimSpace(TruncationNote) + "\n\n" + transcript[len(transcript)-charLimit:]
	}
	return transcript
}

func withHistory(history, request string) string {
	if history == "" {
		return "The user's request:\n\n" + request
	}
	return "Conversation so far:\n\n" + history +
		"\n\n---\n\nThe user's new request:\n\n" + request
}

func formatAnswers(answers []Answer) string {
	blocks := make([]string, 0, len(answers))
	for _, answer := range answers {
		blocks = append(blocks, fmt.Sprintf("### Answer from %s (%s)\n\n%s",
			answer.Label, answer.Model, strings.TrimSpace(answer.Text)))
	}
	return strings.Join(blocks, "\n\n")
}

// BuildIndependentPrompt asks one model to answer without seeing the others.
func BuildIndependentPrompt(request, history string, maxChars int) string {
	return Clamp(withHistory(history, request), maxChars)
}

// BuildReviewPrompt asks one model to critique every first-round answer.
func BuildReviewPrompt(request string, answers []Answer, reviewerLabel, history string, maxChars int) string {
	prompt := withHistory(history, request) +
		"\n\n---\n\nThe roundtable produced these independent answers:\n\n" +
		formatAnswers(answers) +
		"\n\n---\n\nYou are " + reviewerLabel + ". Review every answer above, including your " +
		"own. For each one, identify concrete errors, omissions, and unsupported claims. Then " +
		"state where the answers disagree and which position you judge correct, and list the " +
		"strongest ideas that the final answer must keep."
	return Clamp(prompt, maxChars)
}

// BuildChairPrompt asks the chair to write the final answer from everything
// the roundtable produced.
func BuildChairPrompt(request string, answers []Answer, reviews []Answer, history string, maxChars int) string {
	sections := []string{
		withHistory(history, request),
		"---",
		"Independent answers from the roundtable:",
		formatAnswers(answers),
	}

	if len(reviews) > 0 {
		blocks := make([]string, 0, len(reviews))
		for _, review := range reviews {
			blocks = append(blocks, fmt.Sprintf("### Review by %s\n\n%s",
				review.Label, strings.TrimSpace(review.Text)))
		}
		sections = append(sections, "---", "Peer reviews:", strings.Join(blocks, "\n\n"))
	}

	sections = append(sections, "---", "Write the final answer for the user now, using everything above.")
	return Clamp(strings.Join(sections, "\n\n"), maxChars)
}

// BuildDirectPrompt is the single-model path, with no roundtable framing.
func BuildDirectPrompt(request, history string, maxChars int) string {
	return Clamp(withHistory(history, request), maxChars)
}
