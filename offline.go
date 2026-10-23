package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

// Receipts outlive entries and variations. Retrying an acknowledged offline
// operation must never resurrect an entry somebody subsequently deleted.
func applyOfflineSchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS rep_receipts (
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 client_id TEXT NOT NULL,
 entry_id INTEGER NOT NULL,
 response TEXT NOT NULL,
 PRIMARY KEY (user_id, client_id)
 ); CREATE INDEX IF NOT EXISTS rep_receipts_entry ON rep_receipts(entry_id);`)
	return err
}

var errReplayMismatch = errors.New("This saved set has a different payload. Keep it on this device and contact the administrator.")

func (s *store) addRepsOnce(ctx context.Context, userID, typeID int64, day string, count int64, clientID string) (*repEntry, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var stored string
	err = tx.QueryRowContext(ctx, `SELECT response FROM rep_receipts WHERE user_id = ? AND client_id = ?`, userID, clientID).Scan(&stored)
	if err == nil {
		var entry repEntry
		if err := json.Unmarshal([]byte(stored), &entry); err != nil {
			return nil, err
		}
		if entry.TypeID != typeID || entry.Day != day || entry.Count != count {
			return nil, errReplayMismatch
		}
		return &entry, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var name string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM pushup_types WHERE id = ? AND user_id = ?`, typeID, userID).Scan(&name); err != nil {
		return nil, err
	}
	created := nowUTC()
	result, err := tx.ExecContext(ctx, `INSERT INTO rep_entries (user_id, type_id, day, count, created_at) VALUES (?, ?, ?, ?, ?)`, userID, typeID, day, count, created)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	entry := &repEntry{ID: id, TypeID: typeID, TypeName: name, Day: day, Count: count, CreatedAt: created, ClientID: clientID}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO rep_receipts (user_id, client_id, entry_id, response) VALUES (?, ?, ?, ?)`, userID, clientID, id, string(encoded)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return entry, nil
}

// An authenticated snapshot belongs to one account. The browser stores it in
// that account's IndexedDB record, never in a shared service-worker API cache.
func (s *server) handleOfflineSnapshot(w http.ResponseWriter, r *http.Request, sess *session) {
	tx, err := s.store.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		writeError(w, 500, "could not load offline data")
		return
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(r.Context(), `SELECT r.id, r.type_id, t.name, r.day, r.count, r.created_at, COALESCE(q.client_id, '')
 FROM rep_entries r JOIN pushup_types t ON t.id = r.type_id
 LEFT JOIN rep_receipts q ON q.entry_id = r.id AND q.user_id = r.user_id
 WHERE r.user_id = ? ORDER BY r.id DESC`, sess.UserID)
	if err != nil {
		writeError(w, 500, "could not load offline data")
		return
	}
	entries := []repEntry{}
	for rows.Next() {
		var e repEntry
		if err = rows.Scan(&e.ID, &e.TypeID, &e.TypeName, &e.Day, &e.Count, &e.CreatedAt, &e.ClientID); err != nil {
			break
		}
		entries = append(entries, e)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		writeError(w, 500, "could not load offline data")
		return
	}
	rows, err = tx.QueryContext(r.Context(), `SELECT id, name FROM pushup_types WHERE user_id = ? ORDER BY name COLLATE NOCASE`, sess.UserID)
	if err != nil {
		writeError(w, 500, "could not load offline data")
		return
	}
	types := []pushupType{}
	for rows.Next() {
		var t pushupType
		if err = rows.Scan(&t.ID, &t.Name); err != nil {
			break
		}
		types = append(types, t)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		writeError(w, 500, "could not load offline data")
		return
	}
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "could not load offline data")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user_id": sess.UserID, "types": types, "entries": entries})
}
