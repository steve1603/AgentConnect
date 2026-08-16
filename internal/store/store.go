// Package store persists conversations, messages, and deliberation traces in
// SQLite. The driver is pure Go, so no C toolchain is needed to build or run.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/steve1603/AgentConnect/internal/orchestration"
)

// TitleMaxChars bounds the title derived from a conversation's first message.
const TitleMaxChars = 60

// ErrNotFound is returned when a conversation does not exist.
var ErrNotFound = errors.New("conversation not found")

// Conversation is one chat thread.
type Conversation struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Message is one turn. Trace carries the full deliberation record for
// assistant messages and is nil for user messages.
type Message struct {
	ID        int64           `json:"id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Mode      string          `json:"mode,omitempty"`
	Chair     string          `json:"chair,omitempty"`
	Trace     json.RawMessage `json:"trace,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// Store owns the database handle.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS conversations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id INTEGER NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role            TEXT NOT NULL,
    content         TEXT NOT NULL,
    mode            TEXT,
    chair           TEXT,
    trace           TEXT,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_conversation
    ON messages(conversation_id, id);
`

// Open connects to the database at path, creating the file, its parent
// directory, and the schema if they do not exist.
func Open(path string) (*Store, error) {
	if directory := filepath.Dir(path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, fmt.Errorf("create data directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// SQLite handles one writer at a time; keeping a single connection avoids
	// "database is locked" under concurrent turns.
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DeriveTitle turns a first message into a short conversation title.
func DeriveTitle(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	if flat == "" {
		return "New conversation"
	}
	if len([]rune(flat)) <= TitleMaxChars {
		return flat
	}
	return strings.TrimSpace(string([]rune(flat)[:TitleMaxChars-1])) + "…"
}

// CreateConversation starts a thread titled from its first message.
func (s *Store) CreateConversation(ctx context.Context, firstMessage string) (Conversation, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO conversations (title, created_at, updated_at) VALUES (?, ?, ?)`,
		DeriveTitle(firstMessage), format(now), format(now))
	if err != nil {
		return Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	return Conversation{ID: id, Title: DeriveTitle(firstMessage), CreatedAt: now, UpdatedAt: now}, nil
}

// ListConversations returns every thread, most recently active first.
func (s *Store) ListConversations(ctx context.Context) ([]Conversation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, created_at, updated_at FROM conversations ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	conversations := []Conversation{}
	for rows.Next() {
		conversation, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, conversation)
	}
	return conversations, rows.Err()
}

// GetConversation returns one thread with all of its messages.
func (s *Store) GetConversation(ctx context.Context, id int64) (Conversation, []Message, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, created_at, updated_at FROM conversations WHERE id = ?`, id)

	conversation, err := scanConversation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Conversation{}, nil, ErrNotFound
	}
	if err != nil {
		return Conversation{}, nil, err
	}

	messages, err := s.Messages(ctx, id)
	if err != nil {
		return Conversation{}, nil, err
	}
	return conversation, messages, nil
}

// Messages returns every message in a conversation, oldest first.
func (s *Store) Messages(ctx context.Context, conversationID int64) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, role, content, mode, chair, trace, created_at
		 FROM messages WHERE conversation_id = ? ORDER BY id`, conversationID)
	if err != nil {
		return nil, fmt.Errorf("read messages: %w", err)
	}
	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		var (
			message            Message
			mode, chair, trace sql.NullString
			createdAt          string
		)
		if err := rows.Scan(&message.ID, &message.Role, &message.Content,
			&mode, &chair, &trace, &createdAt); err != nil {
			return nil, fmt.Errorf("read messages: %w", err)
		}
		message.Mode = mode.String
		message.Chair = chair.String
		if trace.Valid && trace.String != "" {
			message.Trace = json.RawMessage(trace.String)
		}
		message.CreatedAt = parse(createdAt)
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

// History returns prior turns in the shape the prompt builder expects.
func (s *Store) History(ctx context.Context, conversationID int64) ([]orchestration.HistoryMessage, error) {
	messages, err := s.Messages(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	history := make([]orchestration.HistoryMessage, 0, len(messages))
	for _, message := range messages {
		history = append(history, orchestration.HistoryMessage{Role: message.Role, Content: message.Content})
	}
	return history, nil
}

// AddMessage appends a turn and marks its conversation as recently active.
func (s *Store) AddMessage(ctx context.Context, conversationID int64, message Message) (Message, error) {
	now := time.Now().UTC()
	message.CreatedAt = now

	var trace any
	if len(message.Trace) > 0 {
		trace = string(message.Trace)
	}

	result, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (conversation_id, role, content, mode, chair, trace, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		conversationID, message.Role, message.Content,
		nullable(message.Mode), nullable(message.Chair), trace, format(now))
	if err != nil {
		return Message{}, fmt.Errorf("add message: %w", err)
	}
	if message.ID, err = result.LastInsertId(); err != nil {
		return Message{}, fmt.Errorf("add message: %w", err)
	}

	if _, err := s.db.ExecContext(ctx,
		`UPDATE conversations SET updated_at = ? WHERE id = ?`, format(now), conversationID); err != nil {
		return Message{}, fmt.Errorf("touch conversation: %w", err)
	}
	return message, nil
}

// DeleteConversation removes a thread and every message it owns.
func (s *Store) DeleteConversation(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE conversation_id = ?`, id); err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanConversation(row scanner) (Conversation, error) {
	var (
		conversation         Conversation
		createdAt, updatedAt string
	)
	if err := row.Scan(&conversation.ID, &conversation.Title, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Conversation{}, err
		}
		return Conversation{}, fmt.Errorf("read conversation: %w", err)
	}
	conversation.CreatedAt = parse(createdAt)
	conversation.UpdatedAt = parse(updatedAt)
	return conversation, nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func format(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parse(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
