package providers

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// NewAnthropic builds a provider backed by the Anthropic Messages API,
// using the official Go SDK.
func NewAnthropic(model, apiKey string, timeout time.Duration, retries, maxTokens int) Provider {
	p := &base{
		key:             "anthropic",
		label:           "Claude",
		model:           model,
		apiKey:          apiKey,
		timeout:         timeout,
		retries:         retries,
		maxOutputTokens: maxTokens,
		sleep:           sleepWithContext,
	}

	// Retries are handled by the shared retry loop, so the SDK's own retry
	// layer is turned off to avoid multiplying attempts.
	client := anthropic.NewClient(
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(0),
		option.WithRequestTimeout(timeout),
	)

	p.generate = func(ctx context.Context, system, prompt string) (string, error) {
		return anthropicGenerate(ctx, &client, p, system, prompt)
	}
	return p
}

func anthropicGenerate(ctx context.Context, client *anthropic.Client, p *base, system, prompt string) (string, error) {
	// Sampling parameters are rejected on current Claude models, and thinking
	// depth is left at the model's adaptive default, so neither is sent.
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(p.model),
		MaxTokens: int64(p.maxOutputTokens),
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
	}
	if strings.TrimSpace(system) != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}

	message, err := client.Messages.New(ctx, params)
	if err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			detail := "HTTP " + strconv.Itoa(apiErr.StatusCode) + ": " + apiErr.Error()
			if retryableStatus(apiErr.StatusCode) {
				return "", retryable("%s", detail)
			}
			return "", fatal("%s", detail)
		}
		if ctx.Err() == context.DeadlineExceeded {
			return "", retryable("request timed out after %s", p.timeout)
		}
		return "", retryable("connection error: %v", err)
	}

	if message.StopReason == anthropic.StopReasonRefusal {
		return "", fatal("the model declined this request")
	}

	// Thinking blocks are skipped: their text is empty by default, and the
	// trace only ever stores normal visible output.
	var chunks []string
	for _, block := range message.Content {
		if block.Type == "text" && block.Text != "" {
			chunks = append(chunks, block.Text)
		}
	}
	return strings.Join(chunks, "\n"), nil
}
