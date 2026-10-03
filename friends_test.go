package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// userID looks a user up directly; the API deliberately never exposes ids for
// accounts you are not connected to.
func userID(t *testing.T, st *store, username string) int64 {
	t.Helper()
	u, err := st.userByName(context.Background(), username)
	if err != nil || u == nil {
		t.Fatalf("look up %s: %v", username, err)
	}
	return u.ID
}

// backdateDecline rewinds a declined request's responded_at so the cooling-off
// period can be tested without waiting a week.
func backdateDecline(t *testing.T, st *store, from, to string, days int) {
	t.Helper()
	when := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	res, err := st.db.Exec(
		`UPDATE friend_requests SET responded_at = ?
		  WHERE from_user = ? AND to_user = ? AND status = 'declined'`,
		when, userID(t, st, from), userID(t, st, to))
	if err != nil {
		t.Fatalf("backdate decline: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("backdate decline: %d rows affected, want 1", n)
	}
}

// friendsPayload is the whole friends screen: roster, both queues, leaderboard.
func (c *client) friendsPayload() map[string]any {
	c.t.Helper()
	status, body := c.do("GET", "/api/friends", nil)
	if status != http.StatusOK {
		c.t.Fatalf("GET /api/friends: status %d body %v", status, body)
	}
	return body
}

func (c *client) requestFriend(username string) (int, map[string]any) {
	c.t.Helper()
	return c.do("POST", "/api/friends/requests", map[string]string{"username": username})
}

// incomingID returns the id of the pending request from username.
func (c *client) incomingID(username string) int64 {
	c.t.Helper()
	for _, raw := range c.friendsPayload()["incoming"].([]any) {
		m := raw.(map[string]any)
		if m["username"] == username {
			return int64(m["id"].(float64))
		}
	}
	c.t.Fatalf("no pending request from %q", username)
	return 0
}

func names(t *testing.T, rows []any, key string) []string {
	t.Helper()
	out := []string{}
	for _, raw := range rows {
		out = append(out, raw.(map[string]any)[key].(string))
	}
	return out
}

// twoUsers registers two accounts against the same server, each with its own
// cookie jar, which is what makes a two-sided protocol testable.
func twoUsers(t *testing.T) (*httptest.Server, *client, *client, *store) {
	t.Helper()
	srv, st := newTestServer(t)
	a := newClient(t, srv)
	a.register("alice", "hunter2hunter2")
	b := newClient(t, srv)
	b.register("bob", "hunter2hunter2")
	return srv, a, b, st
}

func TestFriendRequestAcceptedBothWays(t *testing.T) {
	_, alice, bob, _ := twoUsers(t)

	if status, body := alice.requestFriend("bob"); status != http.StatusCreated {
		t.Fatalf("send request: status %d body %v", status, body)
	}

	// Alice sees it as outgoing and is not yet anyone's friend.
	sent := alice.friendsPayload()
	if got := names(t, sent["outgoing"].([]any), "username"); len(got) != 1 || got[0] != "bob" {
		t.Errorf("outgoing: want [bob], got %v", got)
	}
	if got := sent["friends"].([]any); len(got) != 0 {
		t.Errorf("friends before acceptance: want none, got %v", got)
	}

	// Bob sees it as incoming; that is what the banner renders from.
	if got := names(t, bob.friendsPayload()["incoming"].([]any), "username"); len(got) != 1 || got[0] != "alice" {
		t.Fatalf("incoming: want [alice], got %v", got)
	}

	id := bob.incomingID("alice")
	if status, body := bob.do("POST", "/api/friends/requests/"+itoa(id)+"/accept", nil); status != http.StatusOK {
		t.Fatalf("accept: status %d body %v", status, body)
	}

	for _, tc := range []struct {
		c    *client
		want string
	}{{alice, "bob"}, {bob, "alice"}} {
		p := tc.c.friendsPayload()
		if got := names(t, p["friends"].([]any), "username"); len(got) != 1 || got[0] != tc.want {
			t.Errorf("friends: want [%s], got %v", tc.want, got)
		}
		if got := p["incoming"].([]any); len(got) != 0 {
			t.Errorf("incoming after acceptance: want empty, got %v", got)
		}
		if got := p["outgoing"].([]any); len(got) != 0 {
			t.Errorf("outgoing after acceptance: want empty, got %v", got)
		}
	}
}

func TestDeclineBlocksReRequestForOneWeek(t *testing.T) {
	_, alice, bob, st := twoUsers(t)

	alice.requestFriend("bob")
	id := bob.incomingID("alice")
	if status, _ := bob.do("POST", "/api/friends/requests/"+itoa(id)+"/decline", nil); status != http.StatusOK {
		t.Fatalf("decline: status %d", status)
	}

	// Bob's banner is clear and they are not friends.
	if got := bob.friendsPayload()["incoming"].([]any); len(got) != 0 {
		t.Errorf("incoming after decline: want empty, got %v", got)
	}
	if got := bob.friendsPayload()["friends"].([]any); len(got) != 0 {
		t.Errorf("friends after decline: want none, got %v", got)
	}

	status, body := alice.requestFriend("bob")
	if status != http.StatusForbidden {
		t.Fatalf("re-request right after a decline: want 403, got %d body %v", status, body)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "7 days") {
		t.Errorf("error should name the wait, got %q", msg)
	}

	// Alice is told when she may try again.
	out := alice.friendsPayload()["outgoing"].([]any)
	if len(out) != 1 {
		t.Fatalf("outgoing: want the declined row kept, got %v", out)
	}
	row := out[0].(map[string]any)
	if row["status"] != "declined" || row["retry_after"] == nil {
		t.Errorf("declined row should carry status and retry_after, got %v", row)
	}

	// The block is directional: the person who declined can still ask back.
	if status, body := bob.requestFriend("alice"); status != http.StatusCreated {
		t.Fatalf("the declining side should still be able to ask: status %d body %v", status, body)
	}
	_ = st
}

func TestDeclineBlockExpiresAfterAWeek(t *testing.T) {
	_, alice, bob, st := twoUsers(t)

	alice.requestFriend("bob")
	bob.do("POST", "/api/friends/requests/"+itoa(bob.incomingID("alice"))+"/decline", nil)

	// Backdate the decline past the cooling-off period.
	backdateDecline(t, st, "alice", "bob", declineBlockDays+1)

	if status, body := alice.requestFriend("bob"); status != http.StatusCreated {
		t.Fatalf("re-request after the block expires: want 201, got %d body %v", status, body)
	}
	if got := names(t, bob.friendsPayload()["incoming"].([]any), "username"); len(got) != 1 || got[0] != "alice" {
		t.Errorf("the renewed request should be pending again, got %v", got)
	}
	// The row is reused rather than duplicated.
	if got := alice.friendsPayload()["outgoing"].([]any); len(got) != 1 {
		t.Errorf("outgoing: want exactly one row, got %v", got)
	}
}

func TestRemovingAFriendAllowsAnImmediateNewRequest(t *testing.T) {
	_, alice, bob, st := twoUsers(t)

	// Go the long way round: a decline, then an accepted request. That leaves a
	// declined row behind unless acceptance clears it, which is what would
	// otherwise block the re-request after removal.
	alice.requestFriend("bob")
	bob.do("POST", "/api/friends/requests/"+itoa(bob.incomingID("alice"))+"/decline", nil)
	backdateDecline(t, st, "alice", "bob", declineBlockDays+1)
	alice.requestFriend("bob")
	bob.do("POST", "/api/friends/requests/"+itoa(bob.incomingID("alice"))+"/accept", nil)

	if status, body := alice.do("DELETE", "/api/friends/"+itoa(userID(t, st, "bob")), nil); status != http.StatusOK {
		t.Fatalf("remove friend: status %d body %v", status, body)
	}
	// Both sides lose the friendship, not just the remover.
	for _, c := range []*client{alice, bob} {
		if got := c.friendsPayload()["friends"].([]any); len(got) != 0 {
			t.Errorf("friends after removal: want none, got %v", got)
		}
	}

	if status, body := alice.requestFriend("bob"); status != http.StatusCreated {
		t.Fatalf("re-request immediately after removal: want 201, got %d body %v", status, body)
	}
	// And the other direction is free too.
	bob.do("POST", "/api/friends/requests/"+itoa(bob.incomingID("alice"))+"/decline", nil)
	backdateDecline(t, st, "alice", "bob", declineBlockDays+1)
	alice.requestFriend("bob")
	bob.do("POST", "/api/friends/requests/"+itoa(bob.incomingID("alice"))+"/accept", nil)
	if status, _ := bob.do("DELETE", "/api/friends/"+itoa(userID(t, st, "alice")), nil); status != http.StatusOK {
		t.Fatalf("the other side should be able to remove too, got %d", status)
	}
	if status, body := bob.requestFriend("alice"); status != http.StatusCreated {
		t.Fatalf("request after being the remover: want 201, got %d body %v", status, body)
	}
}

func TestOnlyTheRecipientAnswersARequest(t *testing.T) {
	_, alice, bob, _ := twoUsers(t)
	alice.requestFriend("bob")
	id := bob.incomingID("alice")

	for _, action := range []string{"accept", "decline"} {
		if status, _ := alice.do("POST", "/api/friends/requests/"+itoa(id)+"/"+action, nil); status != http.StatusNotFound {
			t.Errorf("sender %s: want 404, got %d", action, status)
		}
	}
	// Still pending for the person who actually has to answer.
	if got := bob.friendsPayload()["incoming"].([]any); len(got) != 1 {
		t.Fatalf("the request should be untouched, got %v", got)
	}
	if status, _ := bob.do("POST", "/api/friends/requests/"+itoa(id)+"/accept", nil); status != http.StatusOK {
		t.Fatal("the recipient should be able to accept")
	}
	// Answering twice is a 404, not a second friendship.
	if status, _ := bob.do("POST", "/api/friends/requests/"+itoa(id)+"/accept", nil); status != http.StatusNotFound {
		t.Error("a request can only be answered once")
	}
}

func TestFriendRequestRejectsBadTargets(t *testing.T) {
	_, alice, bob, st := twoUsers(t)

	if status, _ := alice.requestFriend("alice"); status != http.StatusBadRequest {
		t.Errorf("self request: want 400, got %d", status)
	}
	if status, _ := alice.requestFriend("nobody"); status != http.StatusNotFound {
		t.Errorf("unknown user: want 404, got %d", status)
	}
	if status, _ := alice.requestFriend("   "); status != http.StatusBadRequest {
		t.Errorf("blank username: want 400, got %d", status)
	}

	if status, _ := alice.requestFriend("bob"); status != http.StatusCreated {
		t.Fatal("first request should succeed")
	}
	if status, _ := alice.requestFriend("bob"); status != http.StatusConflict {
		t.Errorf("duplicate pending request: want 409, got %d", status)
	}
	// Usernames are matched case-insensitively, like sign-in.
	if status, _ := alice.requestFriend("BOB"); status != http.StatusConflict {
		t.Errorf("case-insensitive duplicate: want 409, got %d", status)
	}

	bob.do("POST", "/api/friends/requests/"+itoa(bob.incomingID("alice"))+"/accept", nil)
	if status, _ := alice.requestFriend("bob"); status != http.StatusConflict {
		t.Errorf("already friends: want 409, got %d", status)
	}
	_ = st
}

func TestCrossedRequestsBecomeAFriendship(t *testing.T) {
	_, alice, bob, _ := twoUsers(t)

	alice.requestFriend("bob")
	// Bob never opens the banner and asks Alice himself: both have said yes.
	status, body := bob.requestFriend("alice")
	if status != http.StatusOK || body["status"] != "friends" {
		t.Fatalf("crossed requests: want 200/friends, got %d %v", status, body)
	}
	for _, tc := range []struct {
		c    *client
		want string
	}{{alice, "bob"}, {bob, "alice"}} {
		if got := names(t, tc.c.friendsPayload()["friends"].([]any), "username"); len(got) != 1 || got[0] != tc.want {
			t.Errorf("friends: want [%s], got %v", tc.want, got)
		}
	}
}

// A decline stops mattering once the person who declined asks back.
func TestBeingInvitedBackClearsTheBlock(t *testing.T) {
	_, alice, bob, _ := twoUsers(t)

	alice.requestFriend("bob")
	bob.do("POST", "/api/friends/requests/"+itoa(bob.incomingID("alice"))+"/decline", nil)
	bob.requestFriend("alice")

	status, body := alice.requestFriend("bob")
	if status != http.StatusOK || body["status"] != "friends" {
		t.Fatalf("want the invitation to override the block, got %d %v", status, body)
	}
}

func TestLeaderboardCoversOnlyTheFriendCircle(t *testing.T) {
	srv, st := newTestServer(t)
	alice := newClient(t, srv)
	alice.register("alice", "hunter2hunter2")
	bob := newClient(t, srv)
	bob.register("bob", "hunter2hunter2")
	carol := newClient(t, srv)
	carol.register("carol", "hunter2hunter2")

	alice.requestFriend("bob")
	bob.do("POST", "/api/friends/requests/"+itoa(bob.incomingID("alice"))+"/accept", nil)

	today := time.Now()
	lastMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.Local).AddDate(0, 0, -1)

	alice.do("POST", "/api/reps", map[string]any{"type_id": alice.typeID("Standard"), "count": 30})
	bob.do("POST", "/api/reps", map[string]any{"type_id": bob.typeID("Standard"), "count": 50})
	// A set from a previous month counts all-time but not this month.
	bob.do("POST", "/api/reps", map[string]any{
		"type_id": bob.typeID("Standard"), "count": 7, "day": lastMonth.Format(dateLayout),
	})
	carol.do("POST", "/api/reps", map[string]any{"type_id": carol.typeID("Standard"), "count": 999})

	board := alice.friendsPayload()["leaderboard"].([]any)
	if len(board) != 2 {
		t.Fatalf("leaderboard should hold alice and bob only, got %v", board)
	}
	byName := map[string]map[string]any{}
	for _, raw := range board {
		row := raw.(map[string]any)
		byName[row["username"].(string)] = row
	}
	if _, ok := byName["carol"]; ok {
		t.Error("a stranger must never appear on the leaderboard")
	}
	for _, tc := range []struct {
		name                    string
		daily, monthly, allTime float64
		me                      bool
	}{
		{"alice", 30, 30, 30, true},
		{"bob", 50, 50, 57, false},
	} {
		row := byName[tc.name]
		if row == nil {
			t.Fatalf("%s missing from the leaderboard", tc.name)
		}
		if row["daily"] != tc.daily || row["monthly"] != tc.monthly || row["all_time"] != tc.allTime {
			t.Errorf("%s: want %v/%v/%v, got %v/%v/%v", tc.name,
				tc.daily, tc.monthly, tc.allTime, row["daily"], row["monthly"], row["all_time"])
		}
		if row["me"] != tc.me {
			t.Errorf("%s: me = %v, want %v", tc.name, row["me"], tc.me)
		}
		if _, ok := row["weekly"].(float64); !ok {
			t.Errorf("%s: missing weekly total: %v", tc.name, row)
		}
	}

	// Someone with no friends still sees themselves, so the board is never empty.
	solo := carol.friendsPayload()["leaderboard"].([]any)
	if len(solo) != 1 || solo[0].(map[string]any)["username"] != "carol" {
		t.Errorf("a friendless user should see just themselves, got %v", solo)
	}
	_ = st
}

func TestLeaderboardCalendarWeekBoundaries(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	id, err := st.createUser(ctx, "lifter", "unused-test-hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.seedDefaultTypes(ctx, id); err != nil {
		t.Fatal(err)
	}
	var typeID int64
	if err := st.db.QueryRow(`SELECT id FROM pushup_types WHERE user_id = ? LIMIT 1`, id).Scan(&typeID); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Europe/Brussels")
	if err != nil {
		t.Fatal(err)
	}
	for _, date := range []string{
		"2026-09-06", // Sunday, week spans August/September.
		"2026-09-07", // Monday resets, yesterday must not count.
		"2027-01-01", // Week spans December/January.
		"2026-03-29", // Spring DST change.
		"2026-10-25", // Autumn DST change.
	} {
		t.Run(date, func(t *testing.T) {
			if _, err := st.db.Exec(`DELETE FROM rep_entries`); err != nil {
				t.Fatal(err)
			}
			now, err := time.ParseInLocation(dateLayout, date, loc)
			if err != nil {
				t.Fatal(err)
			}
			// Log one rep per day, from a fortnight ago through tomorrow.
			// Tomorrow is inserted directly to verify the query's upper bound.
			for offset := -14; offset <= 1; offset++ {
				if _, err := st.addReps(ctx, id, typeID, now.AddDate(0, 0, offset).Format(dateLayout), 1); err != nil {
					t.Fatal(err)
				}
			}
			rows, err := st.leaderboard(ctx, id, now)
			if err != nil {
				t.Fatal(err)
			}
			want := int64((int(now.Weekday())+6)%7 + 1)
			if len(rows) != 1 || rows[0].Weekly != want || rows[0].Daily != 1 {
				t.Fatalf("want weekly %d and daily 1, got %+v", want, rows)
			}
		})
	}
}

func TestFriendEndpointsRequireAuthAndCSRF(t *testing.T) {
	srv, alice, bob, _ := twoUsers(t)

	anon := newClient(t, srv)
	if status, _ := anon.do("GET", "/api/friends", nil); status != http.StatusUnauthorized {
		t.Errorf("GET /api/friends unauthenticated: want 401, got %d", status)
	}

	alice.requestFriend("bob")
	id := bob.incomingID("alice")

	good := bob.csrf
	bob.csrf = "not-the-token"
	for _, path := range []string{
		"/api/friends/requests/" + itoa(id) + "/accept",
		"/api/friends/requests/" + itoa(id) + "/decline",
	} {
		if status, _ := bob.do("POST", path, nil); status != http.StatusForbidden {
			t.Errorf("%s without a CSRF token: want 403, got %d", path, status)
		}
	}
	if status, _ := bob.do("DELETE", "/api/friends/1", nil); status != http.StatusForbidden {
		t.Errorf("DELETE without a CSRF token: want 403, got %d", status)
	}
	bob.csrf = good
	if status, _ := bob.do("POST", "/api/friends/requests/"+itoa(id)+"/accept", nil); status != http.StatusOK {
		t.Errorf("accept with the right token: want 200, got %d", status)
	}
}

func TestRemovingSomeoneWhoIsNotAFriendIs404(t *testing.T) {
	_, alice, _, st := twoUsers(t)
	if status, _ := alice.do("DELETE", "/api/friends/"+itoa(userID(t, st, "bob")), nil); status != http.StatusNotFound {
		t.Errorf("want 404, got %d", status)
	}
}
