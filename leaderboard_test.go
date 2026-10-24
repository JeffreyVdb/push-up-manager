package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"
)

func TestGlobalLeaderboardRecords(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	now := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	addUser := func(name string) (int64, []int64) {
		t.Helper()
		id, err := st.createUser(ctx, name, "unused")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.seedDefaultTypes(ctx, id); err != nil {
			t.Fatal(err)
		}
		types, err := st.listTypes(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		ids := []int64{}
		for _, typ := range types {
			ids = append(ids, typ.ID)
		}
		return id, ids
	}
	add := func(id, typ int64, day string, count int) {
		t.Helper()
		_, err := st.db.Exec(`INSERT INTO rep_entries (user_id,type_id,day,count,created_at) VALUES (?,?,?,?,?)`, id, typ, day, count, nowUTC())
		if err != nil {
			t.Fatal(err)
		}
	}
	alice, a := addUser("Alice")
	bob, b := addUser("Bob")
	addUser("Empty")
	add(alice, a[0], "2024-12-31", 100)
	add(alice, a[0], "2025-01-01", 80)
	add(alice, a[1], "2025-01-01", 40)
	add(alice, a[1], "2025-01-01", 30)
	add(alice, a[0], "2026-03-01", 20)
	add(alice, a[0], "2026-03-02", 20)
	add(bob, b[0], "2026-03-01", 200)
	add(bob, b[0], "2026-03-02", 100)
	add(bob, b[0], "2026-03-03", 9999) // Future data never contributes.
	checks := []struct {
		metric string
		id     int64
		score  float64
		period string
	}{
		{"best_day", bob, 200, "2026-03-01"}, {"best_month", bob, 300, "2026-03"},
		{"best_year", bob, 300, "2026"}, {"this_month", bob, 300, "2026-03"},
		{"this_year", bob, 300, "2026"}, {"variations_day", alice, 2, "2025-01-01"},
		{"average_day", bob, 150, "2026-03-01"},
	}
	for _, c := range checks {
		t.Run(c.metric, func(t *testing.T) {
			rows, err := st.globalLeaderboard(ctx, alice, c.metric, now)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 2 || rows[0].UserID != c.id || rows[0].Score != c.score || rows[0].Period != c.period {
				t.Fatalf("rows: %+v", rows)
			}
			for _, row := range rows {
				if row.Me != (row.UserID == alice) {
					t.Fatal("wrong self marker")
				}
			}
			if c.metric == "average_day" {
				days := now.Sub(time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)).Hours()/24 + 1
				if math.Abs(rows[1].Score-290/days) > 1e-9 {
					t.Fatalf("rest days omitted: %+v", rows[1])
				}
			}
		})
	}
	// Current windows reset, while historical records remain.
	future := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, metric := range []string{"this_month", "this_year"} {
		rows, err := st.globalLeaderboard(ctx, alice, metric, future)
		if err != nil || len(rows) != 0 {
			t.Fatalf("%s: %+v %v", metric, rows, err)
		}
	}
	// Deletion changes the record and distinct-variation count.
	if _, err := st.db.Exec(`DELETE FROM rep_entries WHERE user_id=? AND type_id=?`, alice, a[1]); err != nil {
		t.Fatal(err)
	}
	rows, err := st.globalLeaderboard(ctx, alice, "best_day", now)
	if err != nil || rows[1].Score != 100 || rows[1].Period != "2024-12-31" {
		t.Fatalf("deleted record: %+v %v", rows, err)
	}
}

func TestGlobalLeaderboardTopTenAndTies(t *testing.T) {
	_, st := newTestServer(t)
	ctx := context.Background()
	now := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	for i := 11; i >= 0; i-- {
		id, err := st.createUser(ctx, fmt.Sprintf("person%02d", i), "unused")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.seedDefaultTypes(ctx, id); err != nil {
			t.Fatal(err)
		}
		types, err := st.listTypes(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		for _, day := range []string{"2024-02-28", "2024-03-01"} {
			if _, err := st.db.Exec(`INSERT INTO rep_entries(user_id,type_id,day,count,created_at) VALUES(?,?,?,?,?)`, id, types[0].ID, day, 30, nowUTC()); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, metric := range []string{"best_day", "average_day"} {
		rows, err := st.globalLeaderboard(ctx, 0, metric, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 10 {
			t.Fatalf("not ten: %d", len(rows))
		}
		for i, row := range rows {
			if row.Rank != 1 || row.Username != fmt.Sprintf("person%02d", i) || row.Period != "2024-02-28" {
				t.Fatalf("tie ordering: %+v", row)
			}
			if metric == "average_day" && row.Score != 20 {
				t.Fatalf("leap day missing: %+v", row)
			}
		}
	}
	// Competition ranks skip places after ties.
	if _, err := st.db.Exec(`UPDATE rep_entries SET count=60 WHERE user_id IN (SELECT id FROM users WHERE username IN ('person00','person01'))`); err != nil {
		t.Fatal(err)
	}
	rows, err := st.globalLeaderboard(ctx, 0, "best_day", now)
	if err != nil || len(rows) != 10 || rows[0].Rank != 1 || rows[1].Rank != 1 || rows[2].Rank != 3 {
		t.Fatalf("competition ranks: %+v %v", rows, err)
	}

}

func TestGlobalLeaderboardAPI(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	if status, _ := c.do("GET", "/api/leaderboard", nil); status != http.StatusUnauthorized {
		t.Fatal(status)
	}
	c.register("viewer", "password-123")
	if status, body := c.do("GET", "/api/leaderboard?metric=bogus", nil); status != 400 {
		t.Fatalf("%d %v", status, body)
	}
	if status, body := c.do("GET", "/api/leaderboard", nil); status != 200 || body["metric"] != "best_day" || len(body["rows"].([]any)) != 0 {
		t.Fatalf("%d %v", status, body)
	}
	other := newClient(t, srv)
	other.register("stranger", "password-123")
	status, body := other.do("POST", "/api/reps", map[string]any{"type_id": other.typeID("Standard"), "day": time.Now().Format("2006-01-02"), "count": 42})
	if status != 201 {
		t.Fatalf("%d %v", status, body)
	}
	status, body = c.do("GET", "/api/leaderboard", nil)
	if status != 200 {
		t.Fatal(status)
	}
	rows := body["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["username"] != "stranger" {
		t.Fatalf("nonfriend missing: %v", body)
	}
}
