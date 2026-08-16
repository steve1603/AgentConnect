// Package providers adapts each model vendor to one interface, with shared
// retry, timing, and error-capture behaviour.
package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"
)

// Result is the outcome of one provider call in one phase of a turn.
type Result struct {
	Provider  string `json:"provider"`
	Label     string `json:"label"`
	Model     string `json:"model"`
	OK        bool   `json:"ok"`
	Text      string `json:"text,omitempty"`
	Error     string `json:"error,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
	Attempts  int    `json:"attempts"`
}

// Provider is one upstream model vendor.
type Provider interface {
	Key() string
	Label() string
	Model() string
	Complete(ctx context.Context, system, prompt string) Result
}

// Error is a provider failure. Retryable marks transient conditions
// (timeouts, 429, 5xx); authentication and validation failures are not.
type Error struct {
	Message   string
	Retryable bool
}

func (e *Error) Error() string { return e.Message }

func retryable(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...), Retryable: true}
}

func fatal(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...), Retryable: false}
}

// retryableStatus reports whether an HTTP status is worth another attempt.
func retryableStatus(status int) bool {
	return status == 408 || status == 429 || status >= 500
}

// generator performs one call to the vendor.
type generator func(ctx context.Context, system, prompt string) (string, error)

// base carries the settings and retry loop shared by every provider.
type base struct {
	key             string
	label           string
	model           string
	apiKey          string
	timeout         time.Duration
	retries         int
	maxOutputTokens int
	endpoint        string
	generate        generator
	// sleep is swappable so tests do not wait on real backoff.
	sleep func(ctx context.Context, d time.Duration)
}

func (b *base) Key() string   { return b.key }
func (b *base) Label() string { return b.label }
func (b *base) Model() string { return b.model }

// Complete runs the vendor call with retries and never returns an error: a
// failure is reported in the Result so one dead vendor cannot fail a turn.
func (b *base) Complete(ctx context.Context, system, prompt string) Result {
	started := time.Now()
	lastError := "unknown error"
	attempt := 0

	for attempt = 1; attempt <= b.retries+1; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, b.timeout)
		text, err := b.generate(callCtx, system, prompt)
		cancel()

		switch {
		case err != nil:
			lastError = err.Error()
			var provErr *Error
			if !errors.As(err, &provErr) || !provErr.Retryable {
				return b.failure(lastError, started, attempt)
			}
		case strings.TrimSpace(text) == "":
			lastError = "provider returned an empty response"
		default:
			return Result{
				Provider:  b.key,
				Label:     b.label,
				Model:     b.model,
				OK:        true,
				Text:      strings.TrimSpace(text),
				LatencyMS: time.Since(started).Milliseconds(),
				Attempts:  attempt,
			}
		}

		if ctx.Err() != nil {
			return b.failure("cancelled", started, attempt)
		}
		if attempt > b.retries {
			break
		}

		// Exponential backoff with jitter before the next attempt.
		delay := time.Duration(1<<(attempt-1)) * time.Second
		if delay > 8*time.Second {
			delay = 8 * time.Second
		}
		delay += time.Duration(rand.Int63n(int64(400 * time.Millisecond)))
		slog.Warn("provider attempt failed",
			"provider", b.key, "attempt", attempt, "of", b.retries+1,
			"error", lastError, "retry_in", delay)
		b.sleep(ctx, delay)
	}

	return b.failure(lastError, started, min(attempt, b.retries+1))
}

func (b *base) failure(message string, started time.Time, attempt int) Result {
	return Result{
		Provider:  b.key,
		Label:     b.label,
		Model:     b.model,
		OK:        false,
		Error:     message,
		LatencyMS: time.Since(started).Milliseconds(),
		Attempts:  attempt,
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
