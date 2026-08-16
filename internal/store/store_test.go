package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steve1603/AgentConnect/internal/store"
)

func open(t *testing.T) *store.Store {
	t.Helper()
	// A nested path also exercises directory creation.
	db, err := store.Open(filepath.Join(t.TempDir(), "data", "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestConversationRoundTrip(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	conversation, err := db.CreateConversation(ctx, "What is 2+2?")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if conversation.Title != "What is 2+2?" {
		t.Errorf("title = %q", conversation.Title)
	}

	if _, err := db.AddMessage(ctx, conversation.ID, store.Message{
		Role: "user", Content: "What is 2+2?",
	}); err != nil {
		t.Fatalf("add user message: %v", err)
	}

	trace := json.RawMessage(`{"mode":"full","chair":"openai"}`)
	if _, err := db.AddMessage(ctx, conversation.ID, store.Message{
		Role: "assistant", Content: "4", Mode: "full", Chair: "openai", Trace: trace,
	}); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	_, messages, err := db.GetConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	if messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Errorf("roles = %q, %q", messages[0].Role, messages[1].Role)
	}
	if messages[0].Trace != nil {
		t.Error("a user message should carry no trace")
	}
	if string(messages[1].Trace) != string(trace) {
		t.Errorf("trace = %s, want %s", messages[1].Trace, trace)
	}
	if messages[1].Mode != "full" || messages[1].Chair != "openai" {
		t.Errorf("mode = %q chair = %q", messages[1].Mode, messages[1].Chair)
	}
}

func TestHistoryReturnsRoleAndContentOnly(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	conversation, _ := db.CreateConversation(ctx, "hello")
	for _, message := range []store.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	} {
		if _, err := db.AddMessage(ctx, conversation.ID, message); err != nil {
			t.Fatalf("add message: %v", err)
		}
	}

	history, err := db.History(ctx, conversation.ID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 || history[0].Content != "hello" || history[1].Role != "assistant" {
		t.Errorf("history = %+v", history)
	}
}

func TestListOrdersByMostRecentActivity(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	first, _ := db.CreateConversation(ctx, "first")
	second, _ := db.CreateConversation(ctx, "second")

	// Activity on the older conversation should move it to the top.
	if _, err := db.AddMessage(ctx, first.ID, store.Message{Role: "user", Content: "ping"}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	conversations, err := db.ListConversations(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(conversations) != 2 {
		t.Fatalf("conversations = %d, want 2", len(conversations))
	}
	if conversations[0].ID != first.ID || conversations[1].ID != second.ID {
		t.Errorf("order = %d, %d; want %d first", conversations[0].ID, conversations[1].ID, first.ID)
	}
}

func TestDeleteRemovesConversationAndMessages(t *testing.T) {
	db := open(t)
	ctx := context.Background()

	conversation, _ := db.CreateConversation(ctx, "temporary")
	if _, err := db.AddMessage(ctx, conversation.ID, store.Message{Role: "user", Content: "hi"}); err != nil {
		t.Fatalf("add message: %v", err)
	}

	if err := db.DeleteConversation(ctx, conversation.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, err := db.GetConversation(ctx, conversation.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get after delete = %v, want ErrNotFound", err)
	}
	if messages, _ := db.Messages(ctx, conversation.ID); len(messages) != 0 {
		t.Errorf("messages after delete = %d, want 0", len(messages))
	}
}

func TestDeleteMissingConversationIsNotFound(t *testing.T) {
	db := open(t)
	if err := db.DeleteConversation(context.Background(), 9999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("delete = %v, want ErrNotFound", err)
	}
}

func TestDeriveTitle(t *testing.T) {
	cases := map[string]string{
		"":                      "New conversation",
		"   ":                   "New conversation",
		"Short question":        "Short question",
		"line one\n\nline  two": "line one line two",
	}
	for input, want := range cases {
		if got := store.DeriveTitle(input); got != want {
			t.Errorf("DeriveTitle(%q) = %q, want %q", input, got, want)
		}
	}

	long := store.DeriveTitle(strings.Repeat("word ", 40))
	if len([]rune(long)) != store.TitleMaxChars {
		t.Errorf("long title = %d runes, want %d", len([]rune(long)), store.TitleMaxChars)
	}
	if !strings.HasSuffix(long, "…") {
		t.Errorf("long title is not marked as truncated: %q", long)
	}
}
