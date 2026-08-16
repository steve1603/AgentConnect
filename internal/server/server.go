// Package server exposes the HTTP API, the SSE progress stream, and the
// embedded React UI.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/steve1603/AgentConnect/internal/config"
	"github.com/steve1603/AgentConnect/internal/orchestration"
	"github.com/steve1603/AgentConnect/internal/providers"
	"github.com/steve1603/AgentConnect/internal/store"
)

// contentSecurityPolicy locks the page to same-origin assets. The UI ships no
// third-party code, and Vite emits external CSS and JS, so neither
// 'unsafe-inline' nor any remote origin is needed.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self' data:; " +
	"connect-src 'self'; " +
	"font-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// maxRequestBytes bounds a chat request body.
const maxRequestBytes = 1 << 20

// buildProviders is swappable so tests can run turns without network access.
type buildProviders func(cfg *config.Config, overrides map[string]string) map[string]providers.Provider

// Server wires configuration, storage, and the provider layer to HTTP.
type Server struct {
	cfg    *config.Config
	store  *store.Store
	runs   *runRegistry
	assets fs.FS
	build  buildProviders
	mux    *http.ServeMux
}

// New builds a server. assets is the built web UI, served at the root.
func New(cfg *config.Config, st *store.Store, assets fs.FS) *Server {
	s := &Server{
		cfg:    cfg,
		store:  st,
		runs:   newRunRegistry(),
		assets: assets,
		build:  providers.Build,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /api/config", s.handleConfig)
	s.mux.HandleFunc("GET /api/conversations", s.handleListConversations)
	s.mux.HandleFunc("GET /api/conversations/{id}", s.handleGetConversation)
	s.mux.HandleFunc("DELETE /api/conversations/{id}", s.handleDeleteConversation)
	s.mux.HandleFunc("POST /api/chat", s.handleChat)
	s.mux.HandleFunc("GET /api/runs/{id}/events", s.handleRunEvents)

	if s.assets != nil {
		s.mux.Handle("GET /", s.spaHandler())
	}
}

// ServeHTTP applies the security headers to every response.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Content-Security-Policy", contentSecurityPolicy)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
	header.Set("Cross-Origin-Opener-Policy", "same-origin")
	s.mux.ServeHTTP(w, r)
}

// spaHandler serves the built UI, falling back to index.html so client-side
// routes resolve. Hashed asset filenames are safe to cache; index.html is not.
func (s *Server) spaHandler() http.Handler {
	files := http.FileServer(http.FS(s.assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(s.assets, path); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			w.Header().Set("Cache-Control", "no-store")
		} else if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

// -- handlers ---------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type providerConfig struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Model      string `json:"model"`
	Configured bool   `json:"configured"`
}

type appConfig struct {
	Providers    []providerConfig `json:"providers"`
	Modes        []string         `json:"modes"`
	DefaultChair string           `json:"default_chair"`
	Ready        bool             `json:"ready"`
}

// handleConfig returns the public configuration. API keys are deliberately
// absent: only whether each provider has one.
func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	list := make([]providerConfig, 0, len(config.Keys))
	for _, key := range config.Keys {
		list = append(list, providerConfig{
			Key:        key,
			Label:      config.Labels[key],
			Model:      s.cfg.Models[key],
			Configured: s.cfg.APIKeys[key] != "",
		})
	}
	writeJSON(w, http.StatusOK, appConfig{
		Providers:    list,
		Modes:        orchestration.Modes,
		DefaultChair: s.cfg.ResolvedChair(),
		Ready:        s.cfg.Ready(),
	})
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	conversations, err := s.store.ListConversations(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, conversations)
}

type conversationDetail struct {
	store.Conversation
	Messages []store.Message `json:"messages"`
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid conversation id")
		return
	}

	conversation, messages, err := s.store.GetConversation(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, conversationDetail{Conversation: conversation, Messages: messages})
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid conversation id")
		return
	}

	switch err := s.store.DeleteConversation(r.Context(), id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "conversation not found")
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

type chatRequest struct {
	Message        string            `json:"message"`
	ConversationID *int64            `json:"conversation_id"`
	Mode           string            `json:"mode"`
	Chair          string            `json:"chair"`
	Models         map[string]string `json:"models"`
}

type chatAccepted struct {
	RunID          string `json:"run_id"`
	ConversationID int64  `json:"conversation_id"`
}

// handleChat starts a turn and returns the id of its progress stream. The
// turn itself runs in the background so the POST returns immediately.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Ready() {
		writeError(w, http.StatusServiceUnavailable,
			"No provider API keys are configured. Add them to .env and restart.")
		return
	}

	var request chatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	request.Message = strings.TrimSpace(request.Message)
	if request.Message == "" {
		writeError(w, http.StatusUnprocessableEntity, "message must not be blank")
		return
	}

	ctx := r.Context()
	var conversationID int64
	if request.ConversationID == nil {
		conversation, err := s.store.CreateConversation(ctx, request.Message)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		conversationID = conversation.ID
	} else {
		conversationID = *request.ConversationID
		if _, _, err := s.store.GetConversation(ctx, conversationID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "conversation not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	// History is read before the new message is stored, so the prompt sees
	// prior turns without the current request duplicated into it.
	history, err := s.store.History(ctx, conversationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.store.AddMessage(ctx, conversationID, store.Message{
		Role: "user", Content: request.Message,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	created := s.runs.create(conversationID)
	go s.executeTurn(created, conversationID, request, history)

	writeJSON(w, http.StatusAccepted, chatAccepted{RunID: created.id, ConversationID: conversationID})
}

// executeTurn runs one turn to completion, emitting progress to its run and
// storing the answer plus the full trace.
func (s *Server) executeTurn(
	active *run, conversationID int64, request chatRequest, history []orchestration.HistoryMessage,
) {
	defer active.finish()

	// The turn outlives the HTTP request that started it, so it gets its own
	// context bounded by the total time all phases could take.
	ctx, cancel := context.WithTimeout(context.Background(), s.turnBudget())
	defer cancel()

	engine := orchestration.New(s.build(s.cfg, request.Models), s.cfg)
	outcome, err := engine.Run(ctx, orchestration.Request{
		Text:    request.Message,
		History: history,
		Mode:    request.Mode,
		Chair:   request.Chair,
	}, active.emit)

	if err != nil {
		slog.Warn("turn failed", "error", err)
		active.emit(orchestration.Event{Type: "error", Message: err.Error()})
		return
	}

	trace, err := json.Marshal(outcome)
	if err != nil {
		slog.Error("could not encode trace", "error", err)
		trace = nil
	}

	stored, err := s.store.AddMessage(ctx, conversationID, store.Message{
		Role:    "assistant",
		Content: outcome.FinalText,
		Mode:    outcome.Mode,
		Chair:   outcome.Chair,
		Trace:   trace,
	})
	if err != nil {
		slog.Error("could not store answer", "error", err)
		active.emit(orchestration.Event{Type: "error", Message: "the answer could not be saved: " + err.Error()})
		return
	}

	active.emit(orchestration.Event{Type: "saved", MessageID: stored.ID})
}

// turnBudget bounds a whole turn: three sequential phases, each of which may
// use its full per-provider retry budget.
func (s *Server) turnBudget() time.Duration {
	perPhase := s.cfg.RequestTimeout * time.Duration(s.cfg.ProviderRetries+1)
	return 3*perPhase + time.Minute
}

// handleRunEvents streams a turn's progress as server-sent events.
func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	active, ok := s.runs.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "run not found or expired")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	backlog, live := active.subscribe()
	defer active.unsubscribe(live)

	for _, event := range backlog {
		if !writeEvent(w, flusher, event) {
			return
		}
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-live:
			if !open {
				return
			}
			if !writeEvent(w, flusher, event) {
				return
			}
		}
	}
}

func writeEvent(w http.ResponseWriter, flusher http.Flusher, event orchestration.Event) bool {
	payload, err := json.Marshal(event)
	if err != nil {
		slog.Error("could not encode event", "error", err)
		return true
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// -- helpers ----------------------------------------------------------------

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("could not write response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]string{"detail": detail})
}
