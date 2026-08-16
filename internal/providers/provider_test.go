package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stubbed builds a provider whose vendor call is a stub, with backoff waits
// removed so retry behaviour can be tested instantly.
func stubbed(retries int, generate generator) *base {
	return &base{
		key:      "stub",
		label:    "Stub",
		model:    "stub-model",
		timeout:  time.Second,
		retries:  retries,
		generate: generate,
		sleep:    func(context.Context, time.Duration) {},
	}
}

func TestCompleteSucceedsOnTheFirstAttempt(t *testing.T) {
	var calls atomic.Int32
	provider := stubbed(2, func(context.Context, string, string) (string, error) {
		calls.Add(1)
		return "  an answer  ", nil
	})

	result := provider.Complete(context.Background(), "system", "prompt")
	if !result.OK || result.Text != "an answer" {
		t.Errorf("result = %+v, want a trimmed successful answer", result)
	}
	if calls.Load() != 1 || result.Attempts != 1 {
		t.Errorf("calls = %d attempts = %d, want 1 and 1", calls.Load(), result.Attempts)
	}
}

func TestCompleteRetriesTransientFailures(t *testing.T) {
	var calls atomic.Int32
	provider := stubbed(2, func(context.Context, string, string) (string, error) {
		if calls.Add(1) < 3 {
			return "", retryable("rate limited")
		}
		return "recovered", nil
	})

	result := provider.Complete(context.Background(), "system", "prompt")
	if !result.OK || result.Text != "recovered" {
		t.Errorf("result = %+v, want success after retries", result)
	}
	if result.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", result.Attempts)
	}
}

func TestCompleteDoesNotRetryFatalFailures(t *testing.T) {
	var calls atomic.Int32
	provider := stubbed(2, func(context.Context, string, string) (string, error) {
		calls.Add(1)
		return "", fatal("authentication failed")
	})

	result := provider.Complete(context.Background(), "system", "prompt")
	if result.OK {
		t.Fatal("an authentication failure should not succeed")
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1: fatal errors must not be retried", calls.Load())
	}
	if !strings.Contains(result.Error, "authentication failed") {
		t.Errorf("error = %q", result.Error)
	}
}

func TestCompleteGivesUpAfterTheRetryBudget(t *testing.T) {
	var calls atomic.Int32
	provider := stubbed(2, func(context.Context, string, string) (string, error) {
		calls.Add(1)
		return "", retryable("still overloaded")
	})

	result := provider.Complete(context.Background(), "system", "prompt")
	if result.OK {
		t.Fatal("expected failure")
	}
	if calls.Load() != 3 || result.Attempts != 3 {
		t.Errorf("calls = %d attempts = %d, want 3 (1 try + 2 retries)", calls.Load(), result.Attempts)
	}
}

func TestCompleteTreatsAnEmptyResponseAsRetryable(t *testing.T) {
	var calls atomic.Int32
	provider := stubbed(1, func(context.Context, string, string) (string, error) {
		if calls.Add(1) == 1 {
			return "   ", nil
		}
		return "second time lucky", nil
	})

	result := provider.Complete(context.Background(), "system", "prompt")
	if !result.OK || result.Text != "second time lucky" {
		t.Errorf("result = %+v", result)
	}
}

func TestRetryableStatus(t *testing.T) {
	for status, want := range map[int]bool{
		400: false, 401: false, 403: false, 404: false,
		408: true, 429: true, 500: true, 503: true,
	} {
		if got := retryableStatus(status); got != want {
			t.Errorf("retryableStatus(%d) = %v, want %v", status, got, want)
		}
	}
}

// -- OpenAI -----------------------------------------------------------------

func TestOpenAIReadsOutputText(t *testing.T) {
	var seen struct {
		auth string
		body openAIRequest
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.auth = r.Header.Get("Authorization")
		payload, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(payload, &seen.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output_text":"four"}`))
	}))
	defer server.Close()

	provider := NewOpenAI("gpt-5.6", "sk-test", time.Second, 0, 4096).(*base)
	provider.endpoint = server.URL

	result := provider.Complete(context.Background(), "be brief", "what is 2+2?")
	if !result.OK || result.Text != "four" {
		t.Fatalf("result = %+v", result)
	}
	if seen.auth != "Bearer sk-test" {
		t.Errorf("authorization = %q", seen.auth)
	}
	if seen.body.Model != "gpt-5.6" || seen.body.Instructions != "be brief" || seen.body.Input != "what is 2+2?" {
		t.Errorf("request = %+v", seen.body)
	}
}

func TestOpenAIFallsBackToWalkingOutputItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"output":[
			{"type":"reasoning","content":[]},
			{"type":"message","content":[{"type":"output_text","text":"walked"}]}
		]}`))
	}))
	defer server.Close()

	provider := NewOpenAI("gpt-5.6", "sk-test", time.Second, 0, 4096).(*base)
	provider.endpoint = server.URL

	if result := provider.Complete(context.Background(), "", "hi"); result.Text != "walked" {
		t.Errorf("result = %+v, want the text from the message item", result)
	}
}

func TestOpenAIClassifiesErrorStatuses(t *testing.T) {
	cases := []struct {
		status      int
		wantRetries int32
	}{
		{status: http.StatusUnauthorized, wantRetries: 1},
		{status: http.StatusTooManyRequests, wantRetries: 3},
		{status: http.StatusInternalServerError, wantRetries: 3},
	}

	for _, testCase := range cases {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(testCase.status)
			_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
		}))

		provider := NewOpenAI("gpt-5.6", "sk-test", time.Second, 2, 4096).(*base)
		provider.endpoint = server.URL
		provider.sleep = func(context.Context, time.Duration) {}

		result := provider.Complete(context.Background(), "", "hi")
		server.Close()

		if result.OK {
			t.Errorf("status %d unexpectedly succeeded", testCase.status)
		}
		if calls.Load() != testCase.wantRetries {
			t.Errorf("status %d: calls = %d, want %d", testCase.status, calls.Load(), testCase.wantRetries)
		}
		if !strings.Contains(result.Error, "nope") {
			t.Errorf("status %d: error = %q, want the vendor message", testCase.status, result.Error)
		}
	}
}

// -- Gemini -----------------------------------------------------------------

func TestGeminiReadsModelOutputSteps(t *testing.T) {
	var seen struct {
		key      string
		revision string
		body     geminiRequest
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.key = r.Header.Get("x-goog-api-key")
		seen.revision = r.Header.Get("Api-Revision")
		payload, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(payload, &seen.body)
		_, _ = w.Write([]byte(`{"id":"int_1","status":"completed","steps":[
			{"type":"user_input","content":[{"type":"text","text":"ignored"}]},
			{"type":"model_output","content":[{"type":"text","text":"the answer"}]}
		]}`))
	}))
	defer server.Close()

	provider := NewGemini("gemini-3.7-flash", "key-test", time.Second, 0, 4096).(*base)
	provider.endpoint = server.URL

	result := provider.Complete(context.Background(), "be brief", "what is 2+2?")
	if !result.OK || result.Text != "the answer" {
		t.Fatalf("result = %+v", result)
	}
	if seen.key != "key-test" || seen.revision != geminiAPIRevision {
		t.Errorf("headers: key = %q revision = %q", seen.key, seen.revision)
	}
	// The system framing is folded into the single input the API accepts.
	if !strings.HasPrefix(seen.body.Input, "be brief") || !strings.HasSuffix(seen.body.Input, "what is 2+2?") {
		t.Errorf("input = %q", seen.body.Input)
	}
}

func TestGeminiSurfacesErrorMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"model not found"}}`))
	}))
	defer server.Close()

	provider := NewGemini("nope", "key-test", time.Second, 0, 4096).(*base)
	provider.endpoint = server.URL

	result := provider.Complete(context.Background(), "", "hi")
	if result.OK || !strings.Contains(result.Error, "model not found") {
		t.Errorf("result = %+v", result)
	}
}
