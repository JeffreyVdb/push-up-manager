package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Only these fixed SQL expressions may be interpolated into the query.
var leaderboardMetrics = map[string]string{
	"best_day":       `SELECT user_id, day AS period, total AS score FROM daily`,
	"best_month":     `SELECT user_id, substr(day,1,7) AS period, SUM(total) AS score FROM daily GROUP BY user_id, period`,
	"best_year":      `SELECT user_id, substr(day,1,4) AS period, SUM(total) AS score FROM daily GROUP BY user_id, period`,
	"this_month":     `SELECT user_id, substr(day,1,7) AS period, SUM(total) AS score FROM daily WHERE substr(day,1,7) = substr(?,1,7) GROUP BY user_id, period`,
	"this_year":      `SELECT user_id, substr(day,1,4) AS period, SUM(total) AS score FROM daily WHERE substr(day,1,4) = substr(?,1,4) GROUP BY user_id, period`,
	"variations_day": `SELECT user_id, day AS period, variations AS score FROM daily`,
	"average_day":    `SELECT user_id, MIN(day) AS period, SUM(total) * 1.0 / (julianday(?) - julianday(MIN(day)) + 1) AS score FROM daily GROUP BY user_id`,
}

type globalLeaderRow struct {
	Rank     int     `json:"rank"`
	UserID   int64   `json:"user_id"`
	Username string  `json:"username"`
	Score    float64 `json:"score"`
	Period   string  `json:"period"`
	Me       bool    `json:"me"`
}

func (s *store) globalLeaderboard(ctx context.Context, userID int64, metric string, now time.Time) ([]globalLeaderRow, error) {
	expression, ok := leaderboardMetrics[metric]
	if !ok {
		return nil, fmt.Errorf("unknown leaderboard metric")
	}
	today := now.Format("2006-01-02")
	args := []any{today}
	if metric == "this_month" || metric == "this_year" || metric == "average_day" {
		args = append(args, today)
	}
	// Choose one record per person, preferring the earliest period on a tie.
	// Rank by unrounded score; alphabetical ordering makes the ten-row cutoff stable.
	rows, err := s.db.QueryContext(ctx, `WITH daily AS (
  SELECT user_id, day, SUM(count) AS total, COUNT(DISTINCT type_id) AS variations
  FROM rep_entries WHERE day <= ? GROUP BY user_id, day
 ), scores AS (`+expression+`), records AS (
  SELECT *, ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY score DESC, period) AS record_number FROM scores
 ), ranked AS (
  SELECT u.id, u.username, r.score, r.period, RANK() OVER (ORDER BY r.score DESC) AS place
  FROM records r JOIN users u ON u.id = r.user_id WHERE r.record_number = 1 AND r.score > 0
 ) SELECT id, username, score, period, place FROM ranked
 ORDER BY score DESC, username COLLATE NOCASE, id LIMIT 10`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	board := []globalLeaderRow{}
	for rows.Next() {
		var row globalLeaderRow
		if err := rows.Scan(&row.UserID, &row.Username, &row.Score, &row.Period, &row.Rank); err != nil {
			return nil, err
		}
		row.Me = row.UserID == userID
		board = append(board, row)
	}
	return board, rows.Err()
}

func (s *server) handleLeaderboard(w http.ResponseWriter, r *http.Request, sess *session) {
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		metric = "best_day"
	}
	if _, ok := leaderboardMetrics[metric]; !ok {
		writeError(w, http.StatusBadRequest, "unknown leaderboard metric")
		return
	}
	now := time.Now()
	rows, err := s.store.globalLeaderboard(r.Context(), sess.UserID, metric, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load leaderboard")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metric": metric, "today": now.Format("2006-01-02"), "rows": rows})
}
