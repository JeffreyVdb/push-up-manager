package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Realtime is deliberately an invalidation hint, not a data feed: the server
// says "something you can see changed" and the page re-runs the same JSON
// loaders it already uses. SQLite stays the only source of truth, so no event
// carries state, needs an id, or has to be replayed after a reconnect.

const (
	// The HTML Standard suggests a comment about every 15 seconds so a legacy
	// proxy does not time the idle stream out.
	eventHeartbeat = 15 * time.Second
	// A per-write deadline, refreshed for every frame. A global
	// Server.WriteTimeout cannot be used: it would kill every stream on a
	// fixed interval instead of only the stalled ones.
	eventWriteWait = 5 * time.Second

	eventInvalidate = "event: invalidate\ndata: {}\n\n"
	eventKeepAlive  = ": keep-alive\n\n"
)

// subscriber is one open page. Its channel holds a single pending signal
// because an invalidation carries no unique information: two of them say
// exactly what one says.
type subscriber struct{ signal chan struct{} }

// eventHub maps a user to every page they have open. It owns nothing else:
// authorization stays in the session, and the data stays in the store.
type eventHub struct {
	mu     sync.Mutex
	subs   map[int64]map[*subscriber]struct{}
	done   chan struct{}
	closed bool
}

func newEventHub() *eventHub {
	return &eventHub{subs: map[int64]map[*subscriber]struct{}{}, done: make(chan struct{})}
}

// Subscribe registers one page and returns its signal channel plus the
// removal it must defer. Subscribing to a closed hub hands back a channel that
// never fires; the caller is already selecting on Done.
func (h *eventHub) Subscribe(userID int64) (<-chan struct{}, func()) {
	sub := &subscriber{signal: make(chan struct{}, 1)}

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return sub.signal, func() {}
	}
	if h.subs[userID] == nil {
		h.subs[userID] = map[*subscriber]struct{}{}
	}
	h.subs[userID][sub] = struct{}{}

	var once sync.Once
	return sub.signal, func() { once.Do(func() { h.remove(userID, sub) }) }
}

func (h *eventHub) remove(userID int64, sub *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs[userID], sub)
	if len(h.subs[userID]) == 0 {
		delete(h.subs, userID)
	}
}

// Notify wakes every page of every named user. The send is nonblocking, so a
// suspended phone can never delay the mutation that triggered it or another
// client's notification; a full channel already means "invalidated".
func (h *eventHub) Notify(userIDs ...int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	seen := make(map[int64]struct{}, len(userIDs))
	for _, id := range userIDs {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		for sub := range h.subs[id] {
			select {
			case sub.signal <- struct{}{}:
			default:
			}
		}
	}
}

// Done is closed when the hub shuts down, which is how every open handler is
// released before http.Server.Shutdown starts waiting on it.
func (h *eventHub) Done() <-chan struct{} { return h.done }

// Close is nonblocking and safe to call more than once. It never closes a
// subscriber channel: only the handler that owns one may stop reading it.
func (h *eventHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	close(h.done)
}

// handleEvents streams invalidations for one signed-in user.
//
// It sits behind s.auth but deliberately not behind s.csrf: EventSource sends
// a plain GET and cannot attach a header, and a GET changes no state. The
// HttpOnly session cookie plus http.NewCrossOriginProtection already stop a
// foreign origin from reading this.
func (s *server) handleEvents(w http.ResponseWriter, r *http.Request, sess *session) {
	// Subscribe before the first byte: an invalidation raised while the stream
	// is still being set up has to be waiting when the loop starts.
	signal, unsubscribe := s.events.Subscribe(sess.UserID)
	defer unsubscribe()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	// Tells nginx and friends not to buffer a response that never ends.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// The response controller reaches the real flusher through statusRecorder's
	// Unwrap; a bare w.(http.Flusher) assertion would fail on that wrapper.
	rc := http.NewResponseController(w)
	write := func(frame string) error {
		// A writer without deadlines (HTTP/2, a test recorder) is not a reason
		// to drop the stream.
		if err := rc.SetWriteDeadline(time.Now().Add(eventWriteWait)); err != nil && !errors.Is(err, errors.ErrUnsupported) {
			return err
		}
		if _, err := io.WriteString(w, frame); err != nil {
			return err
		}
		return rc.Flush()
	}

	// Flushing the headers immediately is what makes EventSource fire `open`,
	// which the page uses to refresh after every reconnect.
	start := time.Now()
	err := write(eventKeepAlive)
	slog.Info("events open", "user_id", sess.UserID)
	defer func() {
		slog.Info("events closed", "user_id", sess.UserID,
			"dur_ms", time.Since(start).Milliseconds(), "err", err)
	}()
	if err != nil {
		return
	}

	// One goroutine owns this response: http.ResponseWriter is not safe for
	// concurrent use, so events and heartbeats share this single loop.
	beat := time.NewTicker(s.heartbeat)
	defer beat.Stop()
	for {
		select {
		case <-signal:
			if err = write(eventInvalidate); err != nil {
				return // The browser reconnects on its own.
			}
		case <-beat.C:
			if err = write(eventKeepAlive); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		case <-s.events.Done():
			return
		}
	}
}

// notify wakes the acting user's other pages, and their crew's when the change
// is one a friend can see. It runs after a successful commit and is strictly
// best effort: a browser that went away is not a reason to fail a write that
// already landed.
func (s *server) notify(userIDs ...int64) { s.events.Notify(userIDs...) }

// notifyWithCrew adds the user's friends, whose leaderboards move with them.
func (s *server) notifyWithCrew(ctx context.Context, userID int64) {
	ids, err := s.store.friendIDs(ctx, userID)
	if err != nil {
		// The reps are saved either way; the crew just finds out on their next
		// reconnect or foreground.
		slog.Error("friend ids for notify", "user_id", userID, "err", err)
	}
	s.events.Notify(append(ids, userID)...)
}
