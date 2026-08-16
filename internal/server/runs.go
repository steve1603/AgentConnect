package server

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/steve1603/AgentConnect/internal/orchestration"
)

// runRetention is how long a finished run stays streamable before it is
// dropped from the registry.
const runRetention = 15 * time.Minute

// run is a single in-flight turn whose progress can be streamed.
//
// Events are buffered as well as pushed, so a browser that attaches its
// EventSource slightly after the POST still receives the whole turn from the
// beginning.
type run struct {
	id             string
	conversationID int64

	mu          sync.Mutex
	events      []orchestration.Event
	subscribers []chan orchestration.Event
	done        bool
	finishedAt  time.Time
	finished    chan struct{}
}

func newRun(conversationID int64) *run {
	return &run{
		id:             randomID(),
		conversationID: conversationID,
		finished:       make(chan struct{}),
	}
}

// emit records an event and pushes it to every live subscriber.
func (r *run) emit(event orchestration.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return
	}
	r.events = append(r.events, event)
	for _, subscriber := range r.subscribers {
		// Subscriber channels are buffered generously; a consumer that has
		// stopped reading is skipped rather than blocking the turn.
		select {
		case subscriber <- event:
		default:
		}
	}
}

// finish closes the run to further events and releases every subscriber.
func (r *run) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return
	}
	r.done = true
	r.finishedAt = time.Now()
	for _, subscriber := range r.subscribers {
		close(subscriber)
	}
	r.subscribers = nil
	close(r.finished)
}

// subscribe returns the events so far plus a channel carrying the rest.
// Taking the backlog and registering the channel under one lock means every
// event lands in exactly one of the two.
func (r *run) subscribe() ([]orchestration.Event, <-chan orchestration.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	backlog := make([]orchestration.Event, len(r.events))
	copy(backlog, r.events)

	channel := make(chan orchestration.Event, 64)
	if r.done {
		close(channel)
		return backlog, channel
	}
	r.subscribers = append(r.subscribers, channel)
	return backlog, channel
}

// unsubscribe drops a subscriber that has disconnected.
func (r *run) unsubscribe(channel <-chan orchestration.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, subscriber := range r.subscribers {
		if (<-chan orchestration.Event)(subscriber) == channel {
			r.subscribers = append(r.subscribers[:index], r.subscribers[index+1:]...)
			close(subscriber)
			return
		}
	}
}

// runRegistry tracks active runs and expires finished ones.
type runRegistry struct {
	mu        sync.Mutex
	runs      map[string]*run
	retention time.Duration
}

func newRunRegistry() *runRegistry {
	return &runRegistry{runs: make(map[string]*run), retention: runRetention}
}

func (reg *runRegistry) create(conversationID int64) *run {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	now := time.Now()
	for id, existing := range reg.runs {
		existing.mu.Lock()
		expired := existing.done && now.Sub(existing.finishedAt) > reg.retention
		existing.mu.Unlock()
		if expired {
			delete(reg.runs, id)
		}
	}

	created := newRun(conversationID)
	reg.runs[created.id] = created
	return created
}

func (reg *runRegistry) get(id string) (*run, bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	found, ok := reg.runs[id]
	return found, ok
}

func randomID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		// crypto/rand failing is fatal for IDs; fall back to a timestamp so
		// the app keeps working rather than panicking mid-turn.
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(buffer)
}
