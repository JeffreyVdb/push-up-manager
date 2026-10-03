package main

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// waitSignal reports whether a signal arrives before the test gets bored.
func waitSignal(t *testing.T, ch <-chan struct{}) bool {
	t.Helper()
	select {
	case <-ch:
		return true
	case <-time.After(2 * time.Second):
		return false
	}
}

func TestHubCoalescesRepeatedNotifications(t *testing.T) {
	hub := newEventHub()
	defer hub.Close()

	signal, unsubscribe := hub.Subscribe(7)
	defer unsubscribe()

	// Two invalidations with nobody reading say exactly what one says.
	hub.Notify(7)
	hub.Notify(7)

	if !waitSignal(t, signal) {
		t.Fatal("no signal after notify")
	}
	select {
	case <-signal:
		t.Fatal("second signal queued; invalidations must coalesce")
	default:
	}
}

func TestHubNotifyNeverBlocks(t *testing.T) {
	hub := newEventHub()
	defer hub.Close()

	// A page that has stopped reading, exactly like a suspended phone.
	_, unsubscribe := hub.Subscribe(7)
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		for range 100 {
			hub.Notify(7)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Notify blocked on a subscriber that is not reading")
	}
}

func TestHubReachesEveryPageOfOneUser(t *testing.T) {
	hub := newEventHub()
	defer hub.Close()

	first, unsubFirst := hub.Subscribe(7)
	defer unsubFirst()
	second, unsubSecond := hub.Subscribe(7)
	defer unsubSecond()
	other, unsubOther := hub.Subscribe(8)
	defer unsubOther()

	hub.Notify(7)
	if !waitSignal(t, first) || !waitSignal(t, second) {
		t.Fatal("both pages of the notified user must be woken")
	}
	select {
	case <-other:
		t.Fatal("an unrelated user was notified")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHubNotifyToAbsentUserIsHarmless(t *testing.T) {
	hub := newEventHub()
	defer hub.Close()

	hub.Notify(1, 2, 3) // Nobody has a page open.

	signal, unsubscribe := hub.Subscribe(1)
	unsubscribe()
	unsubscribe() // Unsubscribing twice must not panic or wake anyone.

	hub.Notify(1)
	select {
	case <-signal:
		t.Fatal("an unsubscribed page was notified")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHubCloseReleasesHandlersAndIsIdempotent(t *testing.T) {
	hub := newEventHub()
	_, unsubscribe := hub.Subscribe(7)
	defer unsubscribe()

	hub.Close()
	hub.Close() // Shutdown may take either path; calling twice is safe.

	if !waitSignal(t, hub.Done()) {
		t.Fatal("Done must be closed after Close")
	}
	// Subscribing after the close hands back a released stream, not a hang.
	signal, unsub := hub.Subscribe(7)
	defer unsub()
	hub.Notify(7)
	select {
	case <-signal:
		t.Fatal("a closed hub must not deliver signals")
	case <-time.After(50 * time.Millisecond):
	}
}

/* ------------------------------------------------------------ the stream */

// newTestApp is newTestServer plus the server value, so a test can look at the
// hub, and with a heartbeat short enough to observe.
func newTestApp(t *testing.T) (*httptest.Server, *server) {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	app := newServer(st)
	app.heartbeat = 50 * time.Millisecond
	srv := httptest.NewServer(app.handler())
	t.Cleanup(srv.Close)
	return srv, app
}

// subscriberCount is how a test watches a page arrive and leave.
func (h *eventHub) subscriberCount(userID int64) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[userID])
}

// stream opens /api/events and returns its response plus a line reader. The
// caller cancels ctx to hang up, exactly as a closed tab would.
func (c *client) stream(ctx context.Context, path string) (*http.Response, *bufio.Reader) {
	c.t.Helper()
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Accept", "text/event-stream")
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("GET %s: %v", path, err)
	}
	return res, bufio.NewReader(res.Body)
}

// readFrame reads one non-empty stream line, failing rather than hanging.
func readFrame(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read stream: %v", err)
		}
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
}

// makeFriends walks the request and acceptance the real UI would.
func makeFriends(t *testing.T, from, to *client, fromName, toName string) {
	t.Helper()
	if status, body := from.requestFriend(toName); status != http.StatusCreated {
		t.Fatalf("send request: status %d body %v", status, body)
	}
	id := to.incomingID(fromName)
	if status, body := to.do("POST", "/api/friends/requests/"+itoa(id)+"/accept", nil); status != http.StatusOK {
		t.Fatalf("accept: status %d body %v", status, body)
	}
}

func TestEventsRequiresASession(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)

	status, _ := c.do("GET", "/api/events", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated stream: status %d, want 401", status)
	}
}

func TestEventsStreamsHeadersThenAFriendsMutation(t *testing.T) {
	_, alice, bob, _ := twoUsers(t)
	makeFriends(t, alice, bob, "alice", "bob")

	ctx, hangUp := context.WithCancel(context.Background())
	defer hangUp()
	res, reader := alice.stream(ctx, "/api/events")
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("stream status %d, want 200", res.StatusCode)
	}
	for _, want := range [][2]string{
		{"Content-Type", "text/event-stream"},
		{"Cache-Control", "no-cache"},
		{"X-Accel-Buffering", "no"},
	} {
		if got := res.Header.Get(want[0]); got != want[1] {
			t.Errorf("%s: got %q, want %q", want[0], got, want[1])
		}
	}

	// The opening comment proves the headers were flushed, which is what makes
	// EventSource fire `open` instead of waiting for the first real event.
	if got := readFrame(t, reader); got != ": keep-alive" {
		t.Fatalf("first frame %q, want the opening keep-alive", got)
	}

	// Bob's set moves Alice's crew board, so her open page has to hear about it.
	status, body := bob.do("POST", "/api/reps", map[string]any{
		"type_id": bob.typeID("Standard"), "count": 20,
	})
	if status != http.StatusCreated {
		t.Fatalf("log reps: status %d body %v", status, body)
	}
	if got := readFrame(t, reader); got != "event: invalidate" {
		t.Fatalf("frame %q, want event: invalidate", got)
	}
	if got := readFrame(t, reader); got != "data: {}" {
		t.Fatalf("frame %q, want data: {}", got)
	}
}

func TestEventsHeartbeatIsAComment(t *testing.T) {
	srv, _ := newTestApp(t)
	c := newClient(t, srv)
	c.register("solo", "hunter2hunter2")

	ctx, hangUp := context.WithCancel(context.Background())
	defer hangUp()
	res, reader := c.stream(ctx, "/api/events")
	defer res.Body.Close()

	// A heartbeat must never dispatch a message in the browser, so it stays a
	// comment line and nothing else.
	for range 3 {
		if got := readFrame(t, reader); got != ": keep-alive" {
			t.Fatalf("frame %q, want a keep-alive comment", got)
		}
	}
}

func TestEventsHandlerReturnsWhenTheClientHangsUp(t *testing.T) {
	srv, app := newTestApp(t)
	c := newClient(t, srv)
	c.register("solo", "hunter2hunter2")
	id := userID(t, app.store, "solo")

	ctx, hangUp := context.WithCancel(context.Background())
	res, reader := c.stream(ctx, "/api/events")
	readFrame(t, reader)
	if n := app.events.subscriberCount(id); n != 1 {
		t.Fatalf("subscribers while streaming: %d, want 1", n)
	}

	hangUp()
	res.Body.Close()

	// The handler selects on the request context and runs its deferred
	// unsubscribe; without that, srv.Close would hang at cleanup.
	deadline := time.Now().Add(2 * time.Second)
	for app.events.subscriberCount(id) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("handler did not unsubscribe after the client hung up")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestEventsHandlerReturnsWhenTheHubCloses(t *testing.T) {
	srv, app := newTestApp(t)
	c := newClient(t, srv)
	c.register("solo", "hunter2hunter2")
	id := userID(t, app.store, "solo")

	ctx, hangUp := context.WithCancel(context.Background())
	defer hangUp()
	res, reader := c.stream(ctx, "/api/events")
	defer res.Body.Close()
	readFrame(t, reader)
	if n := app.events.subscriberCount(id); n != 1 {
		t.Fatalf("subscribers while streaming: %d, want 1", n)
	}

	// Shutdown closes the hub first, and every open stream has to let go of its
	// response right then: a handler that waited for its client instead would
	// spend the whole 25-second grace period on every restart.
	app.events.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		io.Copy(io.Discard, res.Body) // Returns when the handler returns.
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler held the response open after the hub closed")
	}

	deadline := time.Now().Add(2 * time.Second)
	for app.events.subscriberCount(id) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("handler did not unsubscribe after the hub closed")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
