package main

import (
	"context"
	"testing"
	"time"
)

func TestDatedGoalHistoryAndIsolation(t *testing.T) {
	srv, st := newTestServer(t)
	c := newClient(t, srv)
	c.register("goalowner", "long-enough-password")
	u, _ := st.userByName(context.Background(), "goalowner")
	for _, g := range []dailyGoal{{"2026-09-05", 100}, {"2026-09-10", 150}} {
		if _, err := st.db.Exec(`INSERT INTO daily_goals VALUES (?, ?, ?)`, u.ID, g.EffectiveDay, g.Target); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		day    string
		target float64
	}{{"2026-09-04", 0}, {"2026-09-05", 100}, {"2026-09-09", 100}, {"2026-09-10", 150}, {"2026-10-01", 150}} {
		status, data := c.do("GET", "/api/day/"+tc.day, nil)
		if status != 200 {
			t.Fatal(status, data)
		}
		if tc.target == 0 {
			if data["goal"] != nil {
				t.Fatalf("retroactive goal: %v", data)
			}
			continue
		}
		if data["goal"].(map[string]any)["target"] != tc.target {
			t.Fatalf("wrong dated goal: %v", data)
		}
	}
	_, cal := c.do("GET", "/api/calendar?year=2026&month=9", nil)
	if len(cal["goals"].([]any)) != 2 {
		t.Fatal(cal)
	}
	_, snapshot := c.do("GET", "/api/offline", nil)
	if len(snapshot["goals"].([]any)) != 2 {
		t.Fatal(snapshot)
	}
	other := newClient(t, srv)
	other.register("goalother", "long-enough-password")
	_, data := other.do("GET", "/api/goals", nil)
	if len(data["goals"].([]any)) != 0 {
		t.Fatal("goals leaked across accounts", data)
	}
}

func TestSaveGoalDateValidationAndSameDayReplacement(t *testing.T) {
	srv, _ := newTestServer(t)
	c := newClient(t, srv)
	if code, _ := c.do("GET", "/api/goals", nil); code != 401 {
		t.Fatal(code)
	}
	c.register("savegoal", "long-enough-password")
	for _, target := range []int{100, 150} {
		code, data := c.do("PUT", "/api/goals", dailyGoal{today(), int64(target)})
		if code != 200 {
			t.Fatal(code, data)
		}
	}
	_, data := c.do("GET", "/api/goals", nil)
	goals := data["goals"].([]any)
	if len(goals) != 1 || goals[0].(map[string]any)["target"] != float64(150) {
		t.Fatal(data)
	}
	for _, target := range []any{0, -1, 100001, 1.5, "100"} {
		code, _ := c.do("PUT", "/api/goals", map[string]any{"target": target, "effective_day": today()})
		if code != 400 {
			t.Fatalf("target %v accepted: %d", target, code)
		}
	}
	for _, day := range []string{"", time.Now().AddDate(0, 0, -1).Format(dateLayout), time.Now().AddDate(0, 0, 1).Format(dateLayout)} {
		code, _ := c.do("PUT", "/api/goals", dailyGoal{day, 100})
		if code != 409 {
			t.Fatalf("preview date %q accepted: %d", day, code)
		}
	}
	c.csrf = ""
	if code, _ := c.do("PUT", "/api/goals", dailyGoal{today(), 100}); code != 403 {
		t.Fatal("missing CSRF accepted", code)
	}
}
