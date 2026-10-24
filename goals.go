package main

import (
	"context"
	"database/sql"
	"net/http"
)

const goalSchema = `CREATE TABLE IF NOT EXISTS daily_goals (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 effective_day TEXT NOT NULL,
 target INTEGER NOT NULL CHECK (target BETWEEN 1 AND 100000),
 PRIMARY KEY (user_id, effective_day)
);`

type dailyGoal struct {
	EffectiveDay string `json:"effective_day"`
	Target       int64  `json:"target"`
}

// Accept a transaction too, so an offline snapshot is internally consistent.
func readGoals(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, userID int64) ([]dailyGoal, error) {
	rows, err := db.QueryContext(ctx, `SELECT effective_day, target FROM daily_goals WHERE user_id = ? ORDER BY effective_day`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	goals := []dailyGoal{}
	for rows.Next() {
		var goal dailyGoal
		if err := rows.Scan(&goal.EffectiveDay, &goal.Target); err != nil {
			return nil, err
		}
		goals = append(goals, goal)
	}
	return goals, rows.Err()
}

func goalForDay(goals []dailyGoal, day string) *dailyGoal {
	for i := len(goals) - 1; i >= 0; i-- {
		if goals[i].EffectiveDay <= day {
			return &goals[i]
		}
	}
	return nil
}

func (s *server) handleGoals(w http.ResponseWriter, r *http.Request, sess *session) {
	goals, err := readGoals(r.Context(), s.store.db, sess.UserID)
	if err != nil {
		writeError(w, 500, "Could not load daily goals.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"goals": goals, "today": today()})
}

func (s *server) handleSaveGoal(w http.ResponseWriter, r *http.Request, sess *session) {
	var in dailyGoal
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Target < 1 || in.Target > 100000 {
		writeError(w, 400, "Choose a whole-number goal between 1 and 100,000 push-ups.")
		return
	}
	// The preview's date must match the server day, even across midnight.
	if in.EffectiveDay != today() {
		writeError(w, 409, "The date has changed. Reopen Settings to review the new start date before saving.")
		return
	}
	_, err := s.store.db.ExecContext(r.Context(), `INSERT INTO daily_goals (user_id, effective_day, target) VALUES (?, ?, ?)
 ON CONFLICT(user_id, effective_day) DO UPDATE SET target = excluded.target`, sess.UserID, in.EffectiveDay, in.Target)
	if err != nil {
		writeError(w, 500, "Could not save your daily goal. Try again.")
		return
	}
	s.notify(sess.UserID)
	writeJSON(w, http.StatusOK, in)
}
