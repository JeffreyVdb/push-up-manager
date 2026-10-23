package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
)

func TestOfflineReplayKeepsOriginalEntry(t *testing.T) {
	srv, st := newTestServer(t)
	c := newClient(t, srv)
	c.register("offline-alice", "long-enough-password")
	status, session := c.do("GET", "/api/session", nil)
	if status != 200 {
		t.Fatal(session)
	}
	payload := map[string]any{"user_id": session["user_id"], "client_id": "550e8400-e29b-41d4-a716-446655440000", "type_id": c.typeID("Standard"), "day": "2025-01-12", "count": 27}
	status, first := c.do("POST", "/api/reps", payload)
	if status != http.StatusCreated {
		t.Fatalf("first: %d %v", status, first)
	}
	status, retry := c.do("POST", "/api/reps", payload)
	if status != http.StatusCreated || first["id"] != retry["id"] {
		t.Fatalf("retry: %d %v", status, retry)
	}
	var count, total int
	if err := st.db.QueryRow(`SELECT COUNT(*), SUM(count) FROM rep_entries`).Scan(&count, &total); err != nil {
		t.Fatal(err)
	}
	if count != 1 || total != 27 {
		t.Fatalf("got %d entries and %d reps", count, total)
	}
	// A delayed retry after deletion is an acknowledgement, never resurrection.
	c.do("DELETE", "/api/types/"+fmt.Sprint(payload["type_id"]), nil)
	status, retry = c.do("POST", "/api/reps", payload)
	if status != http.StatusCreated || first["id"] != retry["id"] {
		t.Fatalf("retry after delete: %d %v", status, retry)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM rep_entries`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("retry resurrected deleted reps")
	}
	payload["count"] = 28
	if status, _ := c.do("POST", "/api/reps", payload); status != http.StatusConflict {
		t.Fatalf("changed payload: %d", status)
	}
}

func TestOfflineIdentityAndSnapshot(t *testing.T) {
	srv, _ := newTestServer(t)
	alice := newClient(t, srv)
	alice.register("offline-user-a", "long-enough-password")
	_, session := alice.do("GET", "/api/session", nil)
	payload := map[string]any{"user_id": session["user_id"], "client_id": "same-operation-key-for-both", "type_id": alice.typeID("Standard"), "day": "2025-01-12", "count": 8}
	if status, response := alice.do("POST", "/api/reps", payload); status != http.StatusCreated {
		t.Fatalf("add: %d %v", status, response)
	}
	bob := newClient(t, srv)
	bob.register("offline-user-b", "long-enough-password")
	if status, _ := bob.do("POST", "/api/reps", payload); status != http.StatusConflict {
		t.Fatalf("cross-account: %d", status)
	}
	_, bobSession := bob.do("GET", "/api/session", nil)
	payload["user_id"] = bobSession["user_id"]
	payload["type_id"] = bob.typeID("Standard")
	if status, response := bob.do("POST", "/api/reps", payload); status != http.StatusCreated {
		t.Fatalf("per-user operation key: %d %v", status, response)
	}
	status, snapshot := alice.do("GET", "/api/offline", nil)
	if status != http.StatusOK || snapshot["user_id"] != session["user_id"] {
		t.Fatalf("snapshot: %d %v", status, snapshot)
	}
	entries := snapshot["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["client_id"] != payload["client_id"] {
		t.Fatalf("snapshot entries: %v", entries)
	}
}

func TestConcurrentOfflineReplayIsAtomic(t *testing.T) {
	srv, st := newTestServer(t)
	c := newClient(t, srv)
	c.register("offline-concurrent", "long-enough-password")
	_, session := c.do("GET", "/api/session", nil)
	userID := int64(session["user_id"].(float64))
	typeID := c.typeID("Standard")
	var wg sync.WaitGroup
	results := make(chan *repEntry, 8)
	failures := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entry, err := st.addRepsOnce(context.Background(), userID, typeID, "2025-01-13", 12, "concurrent-replay-id")
			if err != nil {
				failures <- err
				return
			}
			results <- entry
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var id int64
	for entry := range results {
		if id != 0 && id != entry.ID {
			t.Fatal("concurrent replay created multiple entries")
		}
		id = entry.ID
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM rep_entries`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("got %d entries", count)
	}
}
