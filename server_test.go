package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

// newTestServer builds a server backed by a throwaway database file.
func newTestServer(t *testing.T) (*httptest.Server, *store) {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	srv := httptest.NewServer(newServer(st).handler())
	t.Cleanup(srv.Close)
	return srv, st
}

// client is a cookie-carrying test client that tracks its CSRF token.
type client struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
}

func newClient(t *testing.T, srv *httptest.Server) *client {
	t.Helper()
	jar := &cookieJar{}
	return &client{t: t, base: srv.URL, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body any) (int, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatalf("encode body: %v", err)
		}
	}
	req, err := http.NewRequest(method, c.base+path, &buf)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	// Mimic a same-origin browser request so CrossOriginProtection allows it.
	req.Header.Set("Sec-Fetch-Site", "same-origin")

	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()

	out := map[string]any{}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func (c *client) register(username, password string) {
	c.t.Helper()
	status, body := c.do("POST", "/api/register", map[string]string{
		"username": username, "password": password,
	})
	if status != http.StatusCreated {
		c.t.Fatalf("register %s: status %d body %v", username, status, body)
	}
	c.csrf, _ = body["csrf_token"].(string)
}

func (c *client) typeID(name string) int64 {
	c.t.Helper()
	status, body := c.do("GET", "/api/types", nil)
	if status != http.StatusOK {
		c.t.Fatalf("list types: status %d", status)
	}
	for _, raw := range body["types"].([]any) {
		m := raw.(map[string]any)
		if m["name"] == name {
			return int64(m["id"].(float64))
		}
	}
	c.t.Fatalf("type %q not found in %v", name, body["types"])
	return 0
}

// cookieJar is a minimal single-host jar; net/http/cookiejar would pull in
// publicsuffix for no benefit here.
type cookieJar struct{ cookies []*http.Cookie }

func (j *cookieJar) SetCookies(_ *url.URL, cookies []*http.Cookie) {
	for _, c := range cookies {
		replaced := false
		for i, existing := range j.cookies {
			if existing.Name == c.Name {
				j.cookies[i] = c
				replaced = true
			}
		}
		if !replaced {
			j.cookies = append(j.cookies, c)
		}
	}
}

func (j *cookieJar) Cookies(_ *url.URL) []*http.Cookie {
	live := []*http.Cookie{}
	for _, c := range j.cookies {
		if c.MaxAge < 0 {
			continue
		}
		live = append(live, c)
	}
	return live
}

func TestRegisterSeedsTypesAndSignsIn(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	c.register("lifter", "hunter2hunter2")

	status, body := c.do("GET", "/api/types", nil)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	types := body["types"].([]any)
	if len(types) != 4 {
		t.Fatalf("want 4 seeded types, got %d", len(types))
	}
	if got := body["last_used_type_id"].(float64); got != 0 {
		t.Errorf("a fresh account has no last used type, got %v", got)
	}
}

func TestRegisterRejectsDuplicateUsername(t *testing.T) {
	srv, _ := newTestServer(t)
	newClient(t, srv).register("lifter", "hunter2hunter2")

	status, _ := newClient(t, srv).do("POST", "/api/register", map[string]string{
		"username": "LIFTER", "password": "hunter2hunter2",
	})
	if status != http.StatusConflict {
		t.Fatalf("want 409 for a case-insensitive duplicate, got %d", status)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	srv, _ := newTestServer(t)
	newClient(t, srv).register("lifter", "hunter2hunter2")

	c := newClient(t, srv)
	if status, _ := c.do("POST", "/api/login", map[string]string{
		"username": "lifter", "password": "wrongwrongwrong",
	}); status != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", status)
	}
	if status, _ := c.do("POST", "/api/login", map[string]string{
		"username": "lifter", "password": "hunter2hunter2",
	}); status != http.StatusOK {
		t.Fatalf("want 200 for the right password, got %d", status)
	}
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	for _, path := range []string{"/api/types", "/api/overview", "/api/calendar", "/api/day/2026-01-01"} {
		if status, _ := c.do("GET", path, nil); status != http.StatusUnauthorized {
			t.Errorf("%s: want 401, got %d", path, status)
		}
	}
}

func TestCSRFTokenRequiredForMutations(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	c.register("lifter", "hunter2hunter2")

	good := c.csrf
	c.csrf = "not-the-token"
	if status, _ := c.do("POST", "/api/types", map[string]string{"name": "Archer"}); status != http.StatusForbidden {
		t.Fatalf("want 403 with a bad CSRF token, got %d", status)
	}
	c.csrf = good
	if status, _ := c.do("POST", "/api/types", map[string]string{"name": "Archer"}); status != http.StatusCreated {
		t.Fatalf("want 201 with the right CSRF token, got %d", status)
	}
}

func TestRepsDefaultToToday(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	c.register("lifter", "hunter2hunter2")
	id := c.typeID("Standard")

	// No day field at all: the server must fill in today.
	status, body := c.do("POST", "/api/reps", map[string]any{"type_id": id, "count": 25})
	if status != http.StatusCreated {
		t.Fatalf("status %d body %v", status, body)
	}
	if got := body["day"].(string); got != today() {
		t.Errorf("want day %q, got %q", today(), got)
	}

	// An empty string must behave the same way.
	_, body = c.do("POST", "/api/reps", map[string]any{"type_id": id, "count": 5, "day": ""})
	if got := body["day"].(string); got != today() {
		t.Errorf("empty day: want %q, got %q", today(), got)
	}

	_, overview := c.do("GET", "/api/overview", nil)
	if got := overview["today"].(float64); got != 30 {
		t.Errorf("want 30 reps today, got %v", got)
	}
}

func TestRepsValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	c.register("lifter", "hunter2hunter2")
	id := c.typeID("Standard")

	tomorrow := time.Now().AddDate(0, 0, 1).Format(dateLayout)
	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"zero count", map[string]any{"type_id": id, "count": 0}, http.StatusBadRequest},
		{"negative count", map[string]any{"type_id": id, "count": -5}, http.StatusBadRequest},
		{"absurd count", map[string]any{"type_id": id, "count": 10001}, http.StatusBadRequest},
		{"future day", map[string]any{"type_id": id, "count": 10, "day": tomorrow}, http.StatusBadRequest},
		{"malformed day", map[string]any{"type_id": id, "count": 10, "day": "24-08-2026"}, http.StatusBadRequest},
		{"impossible day", map[string]any{"type_id": id, "count": 10, "day": "2026-02-31"}, http.StatusBadRequest},
		{"unknown type", map[string]any{"type_id": 99999, "count": 10}, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if status, body := c.do("POST", "/api/reps", tc.body); status != tc.want {
				t.Errorf("want %d, got %d (%v)", tc.want, status, body)
			}
		})
	}
}

func TestLastUsedTypeFollowsMostRecentEntry(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	c.register("lifter", "hunter2hunter2")

	wide := c.typeID("Wide")
	diamond := c.typeID("Diamond")

	c.do("POST", "/api/reps", map[string]any{"type_id": wide, "count": 10})
	_, body := c.do("GET", "/api/types", nil)
	if got := int64(body["last_used_type_id"].(float64)); got != wide {
		t.Fatalf("want last used %d, got %d", wide, got)
	}

	c.do("POST", "/api/reps", map[string]any{"type_id": diamond, "count": 10})
	_, body = c.do("GET", "/api/types", nil)
	if got := int64(body["last_used_type_id"].(float64)); got != diamond {
		t.Fatalf("want last used %d, got %d", diamond, got)
	}
}

func TestPastDayLoggingDoesNotAffectToday(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	c.register("lifter", "hunter2hunter2")
	id := c.typeID("Standard")

	past := time.Now().AddDate(0, 0, -3).Format(dateLayout)
	c.do("POST", "/api/reps", map[string]any{"type_id": id, "count": 40, "day": past})
	c.do("POST", "/api/reps", map[string]any{"type_id": id, "count": 10})

	_, overview := c.do("GET", "/api/overview", nil)
	if got := overview["today"].(float64); got != 10 {
		t.Errorf("today: want 10, got %v", got)
	}
	if got := overview["all_time"].(float64); got != 50 {
		t.Errorf("all time: want 50, got %v", got)
	}
	if got := overview["best_day"].(float64); got != 40 {
		t.Errorf("best day: want 40, got %v", got)
	}

	_, day := c.do("GET", "/api/day/"+past, nil)
	if got := day["total"].(float64); got != 40 {
		t.Errorf("past day total: want 40, got %v", got)
	}
}

func TestStreakCountsConsecutiveDays(t *testing.T) {
	srv, st := newTestServer(t)
	c := newClient(t, srv)
	c.register("lifter", "hunter2hunter2")
	id := c.typeID("Standard")

	now := time.Now()
	for _, back := range []int{0, 1, 2, 4} { // a gap at 3 days ago breaks it
		day := now.AddDate(0, 0, -back).Format(dateLayout)
		c.do("POST", "/api/reps", map[string]any{"type_id": id, "count": 10, "day": day})
	}
	_, overview := c.do("GET", "/api/overview", nil)
	if got := overview["streak"].(float64); got != 3 {
		t.Errorf("want a streak of 3, got %v", got)
	}

	// A streak survives a day that has not been logged yet.
	srv2, _ := newTestServer(t)
	c2 := newClient(t, srv2)
	c2.register("other", "hunter2hunter2")
	id2 := c2.typeID("Standard")
	for _, back := range []int{1, 2} {
		day := now.AddDate(0, 0, -back).Format(dateLayout)
		c2.do("POST", "/api/reps", map[string]any{"type_id": id2, "count": 10, "day": day})
	}
	_, overview2 := c2.do("GET", "/api/overview", nil)
	if got := overview2["streak"].(float64); got != 2 {
		t.Errorf("want a streak of 2 with today unlogged, got %v", got)
	}
	_ = st
}

func TestCalendarShapeAndTotals(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	c.register("lifter", "hunter2hunter2")
	id := c.typeID("Standard")
	c.do("POST", "/api/reps", map[string]any{"type_id": id, "count": 12})

	now := time.Now()
	status, body := c.do("GET", fmt.Sprintf("/api/calendar?year=%d&month=%d", now.Year(), int(now.Month())), nil)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	wantLead := (int(firstOfMonth.Weekday()) + 6) % 7
	if got := int(body["lead_blank"].(float64)); got != wantLead {
		t.Errorf("lead_blank: want %d, got %d", wantLead, got)
	}
	wantDays := firstOfMonth.AddDate(0, 1, -1).Day()
	if got := int(body["days"].(float64)); got != wantDays {
		t.Errorf("days: want %d, got %d", wantDays, got)
	}
	totals := body["totals"].([]any)
	if len(totals) != 1 || totals[0].(map[string]any)["total"].(float64) != 12 {
		t.Errorf("totals: want one entry of 12, got %v", totals)
	}
}

func TestCalendarRejectsBadParameters(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	c.register("lifter", "hunter2hunter2")

	for _, q := range []string{"?month=0", "?month=13", "?year=abc", "?year=1000"} {
		if status, _ := c.do("GET", "/api/calendar"+q, nil); status != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", q, status)
		}
	}
}

func TestUsersAreIsolated(t *testing.T) {
	srv, _ := newTestServer(t)

	a := newClient(t, srv)
	a.register("alpha", "hunter2hunter2")
	alphaType := a.typeID("Standard")
	a.do("POST", "/api/reps", map[string]any{"type_id": alphaType, "count": 100})

	b := newClient(t, srv)
	b.register("bravo", "hunter2hunter2")

	_, overview := b.do("GET", "/api/overview", nil)
	if got := overview["all_time"].(float64); got != 0 {
		t.Errorf("bravo should see no reps, got %v", got)
	}
	if status, _ := b.do("POST", "/api/reps", map[string]any{"type_id": alphaType, "count": 5}); status != http.StatusNotFound {
		t.Errorf("bravo logging against alpha's type: want 404, got %d", status)
	}
	if status, _ := b.do("DELETE", fmt.Sprintf("/api/types/%d", alphaType), nil); status != http.StatusNotFound {
		t.Errorf("bravo deleting alpha's type: want 404, got %d", status)
	}

	_, alphaOverview := a.do("GET", "/api/overview", nil)
	if got := alphaOverview["all_time"].(float64); got != 100 {
		t.Errorf("alpha's data must survive, got %v", got)
	}
}

func TestDeletingTypeRemovesItsEntries(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	c.register("lifter", "hunter2hunter2")
	id := c.typeID("Standard")
	c.do("POST", "/api/reps", map[string]any{"type_id": id, "count": 60})

	if status, _ := c.do("DELETE", fmt.Sprintf("/api/types/%d", id), nil); status != http.StatusOK {
		t.Fatalf("delete type failed")
	}
	_, overview := c.do("GET", "/api/overview", nil)
	if got := overview["all_time"].(float64); got != 0 {
		t.Errorf("entries should cascade away with the type, got %v", got)
	}
}

func TestDeleteEntry(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	c.register("lifter", "hunter2hunter2")
	id := c.typeID("Standard")

	_, entry := c.do("POST", "/api/reps", map[string]any{"type_id": id, "count": 33})
	entryID := int64(entry["id"].(float64))

	if status, _ := c.do("DELETE", fmt.Sprintf("/api/reps/%d", entryID), nil); status != http.StatusOK {
		t.Fatalf("delete entry failed")
	}
	if status, _ := c.do("DELETE", fmt.Sprintf("/api/reps/%d", entryID), nil); status != http.StatusNotFound {
		t.Errorf("deleting twice: want 404, got %d", status)
	}
	_, overview := c.do("GET", "/api/overview", nil)
	if got := overview["today"].(float64); got != 0 {
		t.Errorf("want 0 after delete, got %v", got)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	c.register("lifter", "hunter2hunter2")

	if status, _ := c.do("POST", "/api/logout", nil); status != http.StatusOK {
		t.Fatalf("logout failed")
	}
	if status, _ := c.do("GET", "/api/types", nil); status != http.StatusUnauthorized {
		t.Errorf("want 401 after logout, got %d", status)
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !verifyPassword(hash, "correct horse battery staple") {
		t.Error("the right password must verify")
	}
	if verifyPassword(hash, "wrong horse battery staple") {
		t.Error("the wrong password must not verify")
	}
	for _, bad := range []string{"", "notahash", "argon2id$v=19$bad$salt$hash", "bcrypt$x$y$z$w"} {
		if verifyPassword(bad, "anything") {
			t.Errorf("malformed hash %q must not verify", bad)
		}
	}

	other, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if other == hash {
		t.Error("each hash must use a fresh salt")
	}
}

func TestStaticAssetsAreEmbedded(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, tc := range []struct{ path, contentType string }{
		{"/", "text/html; charset=utf-8"},
		{"/app.css", "text/css; charset=utf-8"},
		{"/app.js", "text/javascript; charset=utf-8"},
		{"/sw.js", "text/javascript; charset=utf-8"},
		{"/manifest.webmanifest", "application/manifest+json; charset=utf-8"},
		{"/icons/icon-192.png", "image/png"},
		{"/fonts/archivo.woff2", "font/woff2"},
	} {
		res, err := http.Get(srv.URL + tc.path)
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", tc.path, res.StatusCode)
		}
		if got := res.Header.Get("Content-Type"); got != tc.contentType {
			t.Errorf("%s: content type %q, want %q", tc.path, got, tc.contentType)
		}
	}
}

func TestMissingAssetIs404ButDeepLinkServesShell(t *testing.T) {
	srv, _ := newTestServer(t)

	res, err := http.Get(srv.URL + "/missing.png")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("missing asset: want 404, got %d", res.StatusCode)
	}

	req, _ := http.NewRequest("GET", srv.URL+"/some/deep/link", nil)
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("deep link: want 200, got %d", res.StatusCode)
	}
}

func TestBuildIDSubstitutedIntoAssets(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, path := range []string{"/", "/sw.js"} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		buf := new(bytes.Buffer)
		buf.ReadFrom(res.Body)
		res.Body.Close()
		if bytes.Contains(buf.Bytes(), []byte("__BUILD__")) {
			t.Errorf("%s still contains the __BUILD__ placeholder", path)
		}
	}
}

func TestCalendarMonthTotalBoundaries(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	c.register("lifter", "hunter2hunter2")
	id := c.typeID("Standard")

	log := func(day string, count int) {
		t.Helper()
		if status, _ := c.do("POST", "/api/reps", map[string]any{"type_id": id, "count": count, "day": day}); status != http.StatusOK && status != http.StatusCreated {
			t.Fatalf("log %s: status %d", day, status)
		}
	}
	monthTotal := func(year, month int) float64 {
		t.Helper()
		status, body := c.do("GET", fmt.Sprintf("/api/calendar?year=%d&month=%d", year, month), nil)
		if status != http.StatusOK {
			t.Fatalf("calendar %d-%d: status %d", year, month, status)
		}
		return body["month_total"].(float64)
	}

	// Empty month reads zero.
	if got := monthTotal(2024, 2); got != 0 {
		t.Errorf("empty month: want 0, got %v", got)
	}

	// February 2024 is a leap month: 1st and 29th count, the edge days of the
	// neighbouring months must not.
	log("2024-01-31", 1000)
	log("2024-02-01", 10)
	log("2024-02-01", 5)
	log("2024-02-15", 20)
	log("2024-02-29", 7)
	log("2024-03-01", 2000)

	if got := monthTotal(2024, 2); got != 42 {
		t.Errorf("february: want 42, got %v", got)
	}
	if got := monthTotal(2024, 1); got != 1000 {
		t.Errorf("january: want 1000, got %v", got)
	}
	if got := monthTotal(2024, 3); got != 2000 {
		t.Errorf("march: want 2000, got %v", got)
	}
	if got := monthTotal(2024, 4); got != 0 {
		t.Errorf("april: want 0, got %v", got)
	}
}
