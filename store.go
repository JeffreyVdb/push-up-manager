package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// store owns the single serialized SQLite connection used by every handler.
type store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	username      TEXT NOT NULL COLLATE NOCASE UNIQUE,
	password_hash TEXT NOT NULL,
	created_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	csrf_token TEXT NOT NULL,
	created_at TEXT NOT NULL,
	expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS pushup_types (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name       TEXT NOT NULL COLLATE NOCASE,
	created_at TEXT NOT NULL,
	UNIQUE(user_id, name)
);

CREATE TABLE IF NOT EXISTS rep_entries (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	type_id    INTEGER NOT NULL REFERENCES pushup_types(id) ON DELETE CASCADE,
	day        TEXT NOT NULL,
	count      INTEGER NOT NULL CHECK (count > 0),
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS rep_entries_user_day ON rep_entries(user_id, day);
CREATE INDEX IF NOT EXISTS rep_entries_recent ON rep_entries(user_id, id DESC);

-- One row per ordered pair. A declined row is kept, not deleted: its
-- responded_at is what enforces the cooling-off period before the sender may
-- ask again.
CREATE TABLE IF NOT EXISTS friend_requests (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	from_user    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	to_user      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	status       TEXT NOT NULL CHECK (status IN ('pending', 'declined')),
	created_at   TEXT NOT NULL,
	responded_at TEXT,
	UNIQUE(from_user, to_user),
	CHECK (from_user <> to_user)
);
CREATE INDEX IF NOT EXISTS friend_requests_inbox ON friend_requests(to_user, status);

-- A friendship is mutual, so it is stored once with the lower id first. The
-- CHECK is what makes that canonical form impossible to violate.
CREATE TABLE IF NOT EXISTS friendships (
	user_a     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	user_b     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL,
	PRIMARY KEY (user_a, user_b),
	CHECK (user_a < user_b)
);
CREATE INDEX IF NOT EXISTS friendships_b ON friendships(user_b);
`

// openStore opens the database with the pragmas that keep a single-writer
// SQLite app well behaved, then applies the schema.
func openStore(path string) (*store, error) {
	dsn := "file:" + url.PathEscape(path) + "?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection serializes all SQLite work; the pragmas above are
	// per-connection, so a single connection also keeps them consistent.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &store{db: db}, nil
}

func (s *store) Close() error { return s.db.Close() }

var errConflict = errors.New("already exists")

type user struct {
	ID       int64
	Username string
	Hash     string
}

func (s *store) createUser(ctx context.Context, username, hash string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)`,
		username, hash, nowUTC())
	if err != nil {
		if isUniqueViolation(err) {
			return 0, errConflict
		}
		return 0, err
	}
	return res.LastInsertId()
}

func (s *store) userByName(ctx context.Context, username string) (*user, error) {
	var u user
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, password_hash FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.Hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &u, err
}

// seedDefaultTypes gives a brand new account something to log against.
func (s *store) seedDefaultTypes(ctx context.Context, userID int64) error {
	for _, name := range []string{"Standard", "Wide", "Diamond", "Incline"} {
		if _, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO pushup_types (user_id, name, created_at) VALUES (?, ?, ?)`,
			userID, name, nowUTC()); err != nil {
			return err
		}
	}
	return nil
}

type session struct {
	UserID    int64
	Username  string
	CSRFToken string
}

func (s *store) createSession(ctx context.Context, userID int64, tokenHash, csrf string, ttl time.Duration) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		tokenHash, userID, csrf, now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339))
	return err
}

func (s *store) sessionByHash(ctx context.Context, tokenHash string) (*session, error) {
	var sess session
	var expires string
	err := s.db.QueryRowContext(ctx,
		`SELECT s.user_id, u.username, s.csrf_token, s.expires_at
		   FROM sessions s JOIN users u ON u.id = s.user_id
		  WHERE s.token_hash = ?`, tokenHash).
		Scan(&sess.UserID, &sess.Username, &sess.CSRFToken, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil || time.Now().After(exp) {
		_ = s.deleteSession(ctx, tokenHash)
		return nil, nil
	}
	return &sess, nil
}

func (s *store) deleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *store) purgeExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, nowUTC())
	return err
}

type pushupType struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Total int64  `json:"total"`
}

func (s *store) listTypes(ctx context.Context, userID int64) ([]pushupType, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.name, COALESCE(SUM(r.count), 0)
		   FROM pushup_types t
		   LEFT JOIN rep_entries r ON r.type_id = t.id
		  WHERE t.user_id = ?
		  GROUP BY t.id, t.name
		  ORDER BY t.name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	types := []pushupType{}
	for rows.Next() {
		var t pushupType
		if err := rows.Scan(&t.ID, &t.Name, &t.Total); err != nil {
			return nil, err
		}
		types = append(types, t)
	}
	return types, rows.Err()
}

func (s *store) createType(ctx context.Context, userID int64, name string) (*pushupType, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO pushup_types (user_id, name, created_at) VALUES (?, ?, ?)`,
		userID, name, nowUTC())
	if err != nil {
		if isUniqueViolation(err) {
			return nil, errConflict
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &pushupType{ID: id, Name: name}, nil
}

func (s *store) renameType(ctx context.Context, userID, typeID int64, name string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE pushup_types SET name = ? WHERE id = ? AND user_id = ?`, name, typeID, userID)
	if err != nil {
		if isUniqueViolation(err) {
			return errConflict
		}
		return err
	}
	return requireAffected(res)
}

func (s *store) deleteType(ctx context.Context, userID, typeID int64) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM pushup_types WHERE id = ? AND user_id = ?`, typeID, userID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// lastUsedTypeID reports the type of the most recent entry so the UI can
// default to it. Returns 0 when the user has never logged anything.
func (s *store) lastUsedTypeID(ctx context.Context, userID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`SELECT type_id FROM rep_entries WHERE user_id = ? ORDER BY id DESC LIMIT 1`, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

type repEntry struct {
	ID        int64  `json:"id"`
	TypeID    int64  `json:"type_id"`
	TypeName  string `json:"type_name"`
	Day       string `json:"day"`
	Count     int64  `json:"count"`
	CreatedAt string `json:"created_at"`
}

func (s *store) addReps(ctx context.Context, userID, typeID int64, day string, count int64) (*repEntry, error) {
	var name string
	err := s.db.QueryRowContext(ctx,
		`SELECT name FROM pushup_types WHERE id = ? AND user_id = ?`, typeID, userID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}

	created := nowUTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO rep_entries (user_id, type_id, day, count, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, typeID, day, count, created)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &repEntry{ID: id, TypeID: typeID, TypeName: name, Day: day, Count: count, CreatedAt: created}, nil
}

func (s *store) deleteEntry(ctx context.Context, userID, entryID int64) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM rep_entries WHERE id = ? AND user_id = ?`, entryID, userID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (s *store) entriesForDay(ctx context.Context, userID int64, day string) ([]repEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.type_id, t.name, r.day, r.count, r.created_at
		   FROM rep_entries r JOIN pushup_types t ON t.id = r.type_id
		  WHERE r.user_id = ? AND r.day = ?
		  ORDER BY r.id DESC`, userID, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []repEntry{}
	for rows.Next() {
		var e repEntry
		if err := rows.Scan(&e.ID, &e.TypeID, &e.TypeName, &e.Day, &e.Count, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

type dayTotal struct {
	Day   string `json:"day"`
	Total int64  `json:"total"`
}

func (s *store) totalsBetween(ctx context.Context, userID int64, from, to string) ([]dayTotal, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT day, SUM(count) FROM rep_entries
		  WHERE user_id = ? AND day >= ? AND day <= ?
		  GROUP BY day ORDER BY day`, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	totals := []dayTotal{}
	for rows.Next() {
		var d dayTotal
		if err := rows.Scan(&d.Day, &d.Total); err != nil {
			return nil, err
		}
		totals = append(totals, d)
	}
	return totals, rows.Err()
}

type stats struct {
	Today     int64 `json:"today"`
	Week      int64 `json:"week"`
	Month     int64 `json:"month"`
	AllTime   int64 `json:"all_time"`
	BestDay   int64 `json:"best_day"`
	StreakNow int64 `json:"streak"`
}

func (s *store) stats(ctx context.Context, userID int64, today time.Time) (*stats, error) {
	var out stats
	sum := func(from, to string, dst *int64) error {
		return s.db.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(count), 0) FROM rep_entries WHERE user_id = ? AND day >= ? AND day <= ?`,
			userID, from, to).Scan(dst)
	}
	todayStr := today.Format(dateLayout)
	if err := sum(todayStr, todayStr, &out.Today); err != nil {
		return nil, err
	}
	if err := sum(today.AddDate(0, 0, -6).Format(dateLayout), todayStr, &out.Week); err != nil {
		return nil, err
	}
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())
	if err := sum(monthStart.Format(dateLayout), todayStr, &out.Month); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(count), 0) FROM rep_entries WHERE user_id = ?`, userID).Scan(&out.AllTime); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(t), 0) FROM (SELECT SUM(count) AS t FROM rep_entries WHERE user_id = ? GROUP BY day)`,
		userID).Scan(&out.BestDay); err != nil {
		return nil, err
	}

	streak, err := s.currentStreak(ctx, userID, today)
	if err != nil {
		return nil, err
	}
	out.StreakNow = streak
	return &out, nil
}

// currentStreak counts consecutive days with at least one rep, ending today or
// yesterday (so a streak is not lost before the day is over).
func (s *store) currentStreak(ctx context.Context, userID int64, today time.Time) (int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT day FROM rep_entries WHERE user_id = ? AND day <= ? ORDER BY day DESC LIMIT 400`,
		userID, today.Format(dateLayout))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	days := map[string]bool{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return 0, err
		}
		days[d] = true
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	cursor := today
	if !days[cursor.Format(dateLayout)] {
		cursor = cursor.AddDate(0, 0, -1)
	}
	var streak int64
	for days[cursor.Format(dateLayout)] {
		streak++
		cursor = cursor.AddDate(0, 0, -1)
	}
	return streak, nil
}

/* ------------------------------------------------------------ friends */

type friend struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Since    string `json:"since"`
}

// storedRequest is the raw request row, before any policy is applied to it.
type storedRequest struct {
	ID          int64
	FromUser    int64
	ToUser      int64
	Status      string
	RespondedAt string
}

// pair puts a friendship's two user ids in the canonical order the friendships
// table requires.
func pair(a, b int64) (int64, int64) {
	if a < b {
		return a, b
	}
	return b, a
}

func (s *store) listFriends(ctx context.Context, userID int64) ([]friend, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT u.id, u.username, f.created_at
		   FROM friendships f
		   JOIN users u ON u.id = CASE WHEN f.user_a = ? THEN f.user_b ELSE f.user_a END
		  WHERE f.user_a = ? OR f.user_b = ?
		  ORDER BY u.username COLLATE NOCASE`, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	friends := []friend{}
	for rows.Next() {
		var f friend
		if err := rows.Scan(&f.ID, &f.Username, &f.Since); err != nil {
			return nil, err
		}
		friends = append(friends, f)
	}
	return friends, rows.Err()
}

// friendIDs is the id-only form of listFriends, for scoping the leaderboard.
func (s *store) friendIDs(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT CASE WHEN user_a = ? THEN user_b ELSE user_a END
		   FROM friendships WHERE user_a = ? OR user_b = ?`, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *store) areFriends(ctx context.Context, a, b int64) (bool, error) {
	lo, hi := pair(a, b)
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM friendships WHERE user_a = ? AND user_b = ?`, lo, hi).Scan(&n)
	return n > 0, err
}

// requestBetween returns the single row for one direction, or nil.
func (s *store) requestBetween(ctx context.Context, from, to int64) (*storedRequest, error) {
	var req storedRequest
	var responded sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, from_user, to_user, status, responded_at
		   FROM friend_requests WHERE from_user = ? AND to_user = ?`, from, to).
		Scan(&req.ID, &req.FromUser, &req.ToUser, &req.Status, &responded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	req.RespondedAt = responded.String
	return &req, nil
}

func (s *store) createRequest(ctx context.Context, from, to int64) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO friend_requests (from_user, to_user, status, created_at) VALUES (?, ?, 'pending', ?)`,
		from, to, nowUTC())
	if err != nil {
		if isUniqueViolation(err) {
			return 0, errConflict
		}
		return 0, err
	}
	return res.LastInsertId()
}

// renewRequest reopens a previously declined row once its cooling-off period
// has run out, which keeps one row per direction forever.
func (s *store) renewRequest(ctx context.Context, requestID int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE friend_requests SET status = 'pending', created_at = ?, responded_at = NULL WHERE id = ?`,
		nowUTC(), requestID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

type friendRequest struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	CreatedAt string `json:"created_at"`
	// Status and RetryAfter are only meaningful on outgoing requests.
	Status     string `json:"status,omitempty"`
	RetryAfter string `json:"retry_after,omitempty"`
}

func (s *store) incomingRequests(ctx context.Context, userID int64) ([]friendRequest, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, u.username, r.created_at
		   FROM friend_requests r JOIN users u ON u.id = r.from_user
		  WHERE r.to_user = ? AND r.status = 'pending'
		  ORDER BY r.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []friendRequest{}
	for rows.Next() {
		var fr friendRequest
		if err := rows.Scan(&fr.ID, &fr.Username, &fr.CreatedAt); err != nil {
			return nil, err
		}
		fr.Status = "pending"
		out = append(out, fr)
	}
	return out, rows.Err()
}

// outgoingRequests includes declined rows: the sender is told when they may
// ask again rather than being left to guess why a retry fails.
func (s *store) outgoingRequests(ctx context.Context, userID int64) ([]friendRequest, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, u.username, r.created_at, r.status, COALESCE(r.responded_at, '')
		   FROM friend_requests r JOIN users u ON u.id = r.to_user
		  WHERE r.from_user = ?
		  ORDER BY u.username COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []friendRequest{}
	for rows.Next() {
		var fr friendRequest
		var responded string
		if err := rows.Scan(&fr.ID, &fr.Username, &fr.CreatedAt, &fr.Status, &responded); err != nil {
			return nil, err
		}
		if fr.Status == "declined" {
			if until, ok := declineBlockUntil(responded); ok {
				fr.RetryAfter = until.UTC().Format(time.RFC3339)
			}
		}
		out = append(out, fr)
	}
	return out, rows.Err()
}

// acceptRequest is recipient-only: the row is matched on to_user, so the
// sender cannot accept their own request. It returns the sender's id and name.
func (s *store) acceptRequest(ctx context.Context, requestID, userID int64) (int64, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()

	var from int64
	var name string
	err = tx.QueryRowContext(ctx,
		`SELECT r.from_user, u.username
		   FROM friend_requests r JOIN users u ON u.id = r.from_user
		  WHERE r.id = ? AND r.to_user = ? AND r.status = 'pending'`, requestID, userID).
		Scan(&from, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", sql.ErrNoRows
	}
	if err != nil {
		return 0, "", err
	}
	if err := befriendTx(ctx, tx, from, userID); err != nil {
		return 0, "", err
	}
	return from, name, tx.Commit()
}

// declineRequest marks the row declined and stamps responded_at, which starts
// the sender's cooling-off period. It returns the sender's id and name.
func (s *store) declineRequest(ctx context.Context, requestID, userID int64) (int64, string, error) {
	var from int64
	var name string
	err := s.db.QueryRowContext(ctx,
		`SELECT r.from_user, u.username FROM friend_requests r JOIN users u ON u.id = r.from_user
		  WHERE r.id = ? AND r.to_user = ? AND r.status = 'pending'`, requestID, userID).Scan(&from, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", sql.ErrNoRows
	}
	if err != nil {
		return 0, "", err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE friend_requests SET status = 'declined', responded_at = ?
		  WHERE id = ? AND to_user = ? AND status = 'pending'`, nowUTC(), requestID, userID)
	if err != nil {
		return 0, "", err
	}
	return from, name, requireAffected(res)
}

// befriend links two users directly, used when each has an open request to the
// other: they have both already said yes.
func (s *store) befriend(ctx context.Context, a, b int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := befriendTx(ctx, tx, a, b); err != nil {
		return err
	}
	return tx.Commit()
}

func befriendTx(ctx context.Context, tx *sql.Tx, a, b int64) error {
	lo, hi := pair(a, b)
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO friendships (user_a, user_b, created_at) VALUES (?, ?, ?)`,
		lo, hi, nowUTC()); err != nil {
		return err
	}
	// A friendship supersedes every request between the pair in both
	// directions, so removing the friend later cannot uncover a stale
	// declined row that would block a fresh request.
	_, err := tx.ExecContext(ctx,
		`DELETE FROM friend_requests
		  WHERE (from_user = ? AND to_user = ?) OR (from_user = ? AND to_user = ?)`, a, b, b, a)
	return err
}

// removeFriend drops the friendship and every request row between the pair, so
// either side may ask again immediately.
func (s *store) removeFriend(ctx context.Context, userID, friendID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	lo, hi := pair(userID, friendID)
	res, err := tx.ExecContext(ctx,
		`DELETE FROM friendships WHERE user_a = ? AND user_b = ?`, lo, hi)
	if err != nil {
		return err
	}
	if err := requireAffected(res); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM friend_requests
		  WHERE (from_user = ? AND to_user = ?) OR (from_user = ? AND to_user = ?)`,
		userID, friendID, friendID, userID); err != nil {
		return err
	}
	return tx.Commit()
}

/* ------------------------------------------------------------ leaderboard */

type leaderRow struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Daily    int64  `json:"daily"`
	Weekly   int64  `json:"weekly"`
	Monthly  int64  `json:"monthly"`
	AllTime  int64  `json:"all_time"`
	Me       bool   `json:"me"`
}

// leaderboard totals the signed-in user and their accepted friends over four
// windows in one pass. Nobody outside that circle is ever counted, which is
// the whole privacy boundary of the feature.
func (s *store) leaderboard(ctx context.Context, userID int64, today time.Time) ([]leaderRow, error) {
	ids, err := s.friendIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	ids = append(ids, userID)

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	todayStr := today.Format(dateLayout)
	// Calendar weeks start on Monday, including weeks spanning months/years.
	weekStart := today.AddDate(0, 0, -(int(today.Weekday())+6)%7).Format(dateLayout)
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location()).Format(dateLayout)

	args := []any{todayStr, weekStart, todayStr, monthStart, todayStr}
	for _, id := range ids {
		args = append(args, id)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT u.id, u.username,
		        COALESCE(SUM(CASE WHEN r.day = ? THEN r.count END), 0),
		        COALESCE(SUM(CASE WHEN r.day >= ? AND r.day <= ? THEN r.count END), 0),
		        COALESCE(SUM(CASE WHEN r.day >= ? AND r.day <= ? THEN r.count END), 0),
		        COALESCE(SUM(r.count), 0)
		   FROM users u
		   LEFT JOIN rep_entries r ON r.user_id = u.id
		  WHERE u.id IN (`+placeholders+`)
		  GROUP BY u.id, u.username
		  ORDER BY u.username COLLATE NOCASE`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	board := []leaderRow{}
	for rows.Next() {
		var row leaderRow
		if err := rows.Scan(&row.UserID, &row.Username, &row.Daily, &row.Weekly, &row.Monthly, &row.AllTime); err != nil {
			return nil, err
		}
		row.Me = row.UserID == userID
		board = append(board, row)
	}
	return board, rows.Err()
}

func requireAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }
