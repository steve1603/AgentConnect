package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/steve1603/AgentConnect/internal/config"
	"github.com/steve1603/AgentConnect/internal/orchestration"
	"github.com/steve1603/AgentConnect/internal/providers"
	"github.com/steve1603/AgentConnect/internal/store"
)

// fakeProvider answers instantly and without network access, so the HTTP
// tests spend no API credits.
type fakeProvider struct {
	key, label string
	fail       bool
}

func (f fakeProvider) Key() string   { return f.key }
func (f fakeProvider) Label() string { return f.label }
func (f fakeProvider) Model() string { return f.key + "-model" }

func (f fakeProvider) Complete(_ context.Context, system, _ string) providers.Result {
	result := providers.Result{Provider: f.key, Label: f.label, Model: f.Model(), Attempts: 1}
	if f.fail {
		result.Error = f.label + " is unavailable"
		return result
	}
	result.OK = true
	switch system {
	case orchestration.ReviewSystem:
		result.Text = f.label + " review"
	case orchestration.ChairSystem:
		result.Text = "FINAL from " + f.label
	default:
		result.Text = f.label + " says hello."
	}
	return result
}

func newTestServer(t *testing.T, failing ...string) *httptest.Server {
	t.Helper()

	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &config.Config{
		APIKeys: map[string]string{"openai": "test", "anthropic": "test", "gemini": "test"},
		Models: map[string]string{
			"openai": "gpt-5.6", "anthropic": "claude-sonnet-5", "gemini": "gemini-3.7-flash",
		},
		RequestTimeout:      time.Second,
		MaxPromptChars:      30000,
		MaxOutputTokens:     4096,
		HistoryMessageLimit: 24,
		HistoryCharLimit:    60000,
		DefaultChair:        "openai",
	}

	shouldFail := map[string]bool{}
	for _, key := range failing {
		shouldFail[key] = true
	}

	srv := New(cfg, db, nil)
	srv.build = func(*config.Config, map[string]string) map[string]providers.Provider {
		return map[string]providers.Provider{
			"openai":    fakeProvider{"openai", "ChatGPT", shouldFail["openai"]},
			"anthropic": fakeProvider{"anthropic", "Claude", shouldFail["anthropic"]},
			"gemini":    fakeProvider{"gemini", "Gemini", shouldFail["gemini"]},
		}
	}

	httpServer := httptest.NewServer(srv)
	t.Cleanup(httpServer.Close)
	return httpServer
}

func post(t *testing.T, base, path, body string) (*http.Response, []byte) {
	t.Helper()
	response, err := http.Post(base+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	return response, payload
}

func get(t *testing.T, base, path string) (*http.Response, []byte) {
	t.Helper()
	response, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(response.Body)
	return response, payload
}

// drain reads a run's SSE stream to completion.
func drain(t *testing.T, base, runID string) []orchestration.Event {
	t.Helper()

	response, err := http.Get(base + "/api/runs/" + runID + "/events")
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", response.StatusCode)
	}

	var events []orchestration.Event
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line, found := strings.CutPrefix(scanner.Text(), "data: ")
		if !found {
			continue
		}
		var event orchestration.Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		events = append(events, event)
	}
	return events
}

func startTurn(t *testing.T, base, body string) (string, int64) {
	t.Helper()
	response, payload := post(t, base, "/api/chat", body)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("chat status = %d: %s", response.StatusCode, payload)
	}
	var accepted chatAccepted
	if err := json.Unmarshal(payload, &accepted); err != nil {
		t.Fatalf("decode accepted: %v", err)
	}
	return accepted.RunID, accepted.ConversationID
}

func TestConfigNeverExposesAPIKeys(t *testing.T) {
	server := newTestServer(t)

	response, payload := get(t, server.URL, "/api/config")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if strings.Contains(string(payload), "test") {
		t.Errorf("the config response leaks an API key: %s", payload)
	}

	var parsed appConfig
	if err := json.Unmarshal(payload, &parsed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !parsed.Ready || len(parsed.Providers) != 3 {
		t.Errorf("config = %+v", parsed)
	}
	for _, provider := range parsed.Providers {
		if !provider.Configured {
			t.Errorf("%s should be configured", provider.Key)
		}
	}
}

func TestSecurityHeadersArePresent(t *testing.T) {
	server := newTestServer(t)
	response, _ := get(t, server.URL, "/healthz")

	policy := response.Header.Get("Content-Security-Policy")
	if !strings.Contains(policy, "default-src 'self'") || strings.Contains(policy, "unsafe-inline") {
		t.Errorf("content-security-policy = %q", policy)
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := response.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestFullTurnStreamsProgressAndPersistsTheTrace(t *testing.T) {
	server := newTestServer(t)
	runID, conversationID := startTurn(t, server.URL, `{"message":"What is 2+2?","mode":"full"}`)

	events := drain(t, server.URL, runID)
	if len(events) == 0 {
		t.Fatal("no events were streamed")
	}
	if last := events[len(events)-1]; last.Type != "saved" {
		t.Errorf("last event = %q, want saved", last.Type)
	}

	var complete *orchestration.Outcome
	phases := map[string]bool{}
	for _, event := range events {
		if event.Type == "provider" {
			phases[event.Phase] = true
		}
		if event.Type == "complete" {
			complete = event.Result
		}
	}
	if !phases["independent"] || !phases["review"] || !phases["chair"] {
		t.Errorf("phases seen = %v", phases)
	}
	if complete == nil || complete.FinalText != "FINAL from ChatGPT" {
		t.Fatalf("complete = %+v", complete)
	}

	_, payload := get(t, server.URL, "/api/conversations/"+strconv.FormatInt(conversationID, 10))
	var detail conversationDetail
	if err := json.Unmarshal(payload, &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if len(detail.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(detail.Messages))
	}

	assistant := detail.Messages[1]
	if assistant.Content != "FINAL from ChatGPT" || assistant.Chair != "openai" {
		t.Errorf("assistant = %+v", assistant)
	}

	var trace orchestration.Outcome
	if err := json.Unmarshal(assistant.Trace, &trace); err != nil {
		t.Fatalf("decode trace: %v", err)
	}
	if len(trace.Answers) != 3 || len(trace.Reviews) != 3 {
		t.Errorf("trace answers = %d reviews = %d, want 3 and 3", len(trace.Answers), len(trace.Reviews))
	}
}

func TestFailedProviderIsReportedButTheTurnSucceeds(t *testing.T) {
	server := newTestServer(t, "gemini")
	runID, _ := startTurn(t, server.URL, `{"message":"hello","mode":"full"}`)

	var complete *orchestration.Outcome
	for _, event := range drain(t, server.URL, runID) {
		if event.Type == "complete" {
			complete = event.Result
		}
	}
	if complete == nil {
		t.Fatal("the turn did not complete")
	}
	if len(complete.Failures) != 1 || complete.Failures[0].Provider != "gemini" {
		t.Errorf("failures = %+v", complete.Failures)
	}
	if complete.FinalText != "FINAL from ChatGPT" {
		t.Errorf("final text = %q", complete.FinalText)
	}
}

func TestSecondTurnReusesTheConversation(t *testing.T) {
	server := newTestServer(t)

	runID, conversationID := startTurn(t, server.URL, `{"message":"My name is Ada."}`)
	drain(t, server.URL, runID)

	body := `{"message":"What is my name?","conversation_id":` + strconv.FormatInt(conversationID, 10) + `}`
	secondRun, secondConversation := startTurn(t, server.URL, body)
	if secondConversation != conversationID {
		t.Fatalf("conversation = %d, want %d", secondConversation, conversationID)
	}
	drain(t, server.URL, secondRun)

	_, payload := get(t, server.URL, "/api/conversations/"+strconv.FormatInt(conversationID, 10))
	var detail conversationDetail
	if err := json.Unmarshal(payload, &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(detail.Messages) != 4 {
		t.Errorf("messages = %d, want 4", len(detail.Messages))
	}
}

func TestConversationCanBeDeleted(t *testing.T) {
	server := newTestServer(t)
	runID, conversationID := startTurn(t, server.URL, `{"message":"hello"}`)
	drain(t, server.URL, runID)

	request, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/conversations/"+strconv.FormatInt(conversationID, 10), nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", response.StatusCode)
	}

	if response, _ := get(t, server.URL, "/api/conversations/"+strconv.FormatInt(conversationID, 10)); response.StatusCode != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", response.StatusCode)
	}
	if _, payload := get(t, server.URL, "/api/conversations"); strings.TrimSpace(string(payload)) != "[]" {
		t.Errorf("conversations after delete = %s", payload)
	}
}

func TestBlankMessageIsRejected(t *testing.T) {
	server := newTestServer(t)
	if response, _ := post(t, server.URL, "/api/chat", `{"message":"   "}`); response.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", response.StatusCode)
	}
}

func TestUnknownConversationIsRejected(t *testing.T) {
	server := newTestServer(t)
	response, _ := post(t, server.URL, "/api/chat", `{"message":"hello","conversation_id":9999}`)
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", response.StatusCode)
	}
}

func TestUnknownRunIsRejected(t *testing.T) {
	server := newTestServer(t)
	if response, _ := get(t, server.URL, "/api/runs/does-not-exist/events"); response.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", response.StatusCode)
	}
}

func TestLateSubscriberStillReceivesTheWholeRun(t *testing.T) {
	server := newTestServer(t)
	runID, _ := startTurn(t, server.URL, `{"message":"hello","mode":"direct"}`)

	// Wait for the turn to finish before attaching, so the whole stream has
	// to come from the run's buffer.
	deadline := time.After(10 * time.Second)
	for {
		if response, _ := get(t, server.URL, "/api/conversations"); response.StatusCode == http.StatusOK {
			events := drainIfFinished(t, server.URL, runID)
			if events != nil {
				if last := events[len(events)-1]; last.Type != "saved" {
					t.Errorf("last event = %q, want saved", last.Type)
				}
				return
			}
		}
		select {
		case <-deadline:
			t.Fatal("the turn did not finish in time")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// drainIfFinished returns the run's events once the run has ended, else nil.
func drainIfFinished(t *testing.T, base, runID string) []orchestration.Event {
	t.Helper()
	events := drain(t, base, runID)
	for _, event := range events {
		if event.Type == "saved" || event.Type == "error" {
			return events
		}
	}
	return nil
}
