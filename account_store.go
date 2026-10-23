package main

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Separate tables keep upgrades additive for existing installations.
const accountSchema = `
CREATE TABLE IF NOT EXISTS account_settings (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 email TEXT COLLATE NOCASE UNIQUE,
 pending_email TEXT,
 email_prompt_dismissed INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS recovery_tokens (
 token_hash TEXT PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('confirm', 'reset')),
 email TEXT NOT NULL,
 expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS recovery_tokens_user ON recovery_tokens(user_id, kind);
CREATE TABLE IF NOT EXISTS account_rate_limits (
 key TEXT PRIMARY KEY,
 count INTEGER NOT NULL,
 expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS account_rate_limits_expiry ON account_rate_limits(expires_at);
`

var errRecoveryToken = errors.New("invalid or expired recovery token")

type accountState struct {
	Email                string `json:"email"`
	PendingEmail         string `json:"pending_email"`
	EmailVerified        bool   `json:"email_verified"`
	EmailPromptDismissed bool   `json:"email_prompt_dismissed"`
	RecoveryAvailable    bool   `json:"recovery_available"`
}

func (s *store) accountState(ctx context.Context, userID int64) (accountState, error) {
	var state accountState
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(email,''), COALESCE(pending_email,''), email_prompt_dismissed FROM account_settings WHERE user_id=?`, userID).Scan(&state.Email, &state.PendingEmail, &state.EmailPromptDismissed)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	state.EmailVerified = state.Email != ""
	return state, err
}

func (s *store) accountPasswordHash(ctx context.Context, userID int64) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id=?`, userID).Scan(&hash)
	return hash, err
}

func (s *store) dismissEmailPrompt(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO account_settings(user_id,email_prompt_dismissed) VALUES (?,1) ON CONFLICT(user_id) DO UPDATE SET email_prompt_dismissed=1`, userID)
	return err
}

// accountRateAllowed persists fixed windows across restarts. Expired keys are
// pruned and a hard cap prevents attacker-controlled addresses growing the DB.
func (s *store) accountRateAllowed(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, err = tx.ExecContext(ctx, `DELETE FROM account_rate_limits WHERE expires_at<=?`, now); err != nil {
		return false, err
	}
	var count int
	err = tx.QueryRowContext(ctx, `SELECT count FROM account_rate_limits WHERE key=?`, key).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		var total int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_rate_limits`).Scan(&total); err != nil {
			return false, err
		}
		if total >= 10000 {
			return false, nil
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO account_rate_limits(key,count,expires_at) VALUES (?,1,?)`, key, now+int64(window/time.Second))
	} else if err == nil {
		if count >= limit {
			return false, nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE account_rate_limits SET count=count+1 WHERE key=?`, key)
	}
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func accountCheckPassword(ctx context.Context, tx *sql.Tx, userID int64, expected string) error {
	var hash string
	if err := tx.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id=?`, userID).Scan(&hash); err != nil {
		return err
	}
	if hash != expected {
		return errBadCredentials
	}
	return nil
}

func (s *store) setPendingEmail(ctx context.Context, userID int64, email, passwordHash, tokenHash string, expires time.Time, resend bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = accountCheckPassword(ctx, tx, userID, passwordHash); err != nil {
		return err
	}
	if resend {
		var pending string
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(pending_email,'') FROM account_settings WHERE user_id=?`, userID).Scan(&pending); err != nil {
			return err
		}
		if pending == "" || pending != email {
			return errRecoveryToken
		}
	} else {
		// A verified address does not need another confirmation.
		var current string
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(email,'') FROM account_settings WHERE user_id=?`, userID).Scan(&current)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if current == email {
			return errConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO account_settings(user_id,pending_email) VALUES (?,?) ON CONFLICT(user_id) DO UPDATE SET pending_email=excluded.pending_email`, userID, email)
		if err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM recovery_tokens WHERE user_id=? AND kind='confirm'`, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO recovery_tokens(token_hash,user_id,kind,email,expires_at) VALUES (?,?,'confirm',?,?)`, tokenHash, userID, email, expires.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *store) removeAccountEmail(ctx context.Context, userID int64, passwordHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = accountCheckPassword(ctx, tx, userID, passwordHash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_settings(user_id,email_prompt_dismissed) VALUES (?,1) ON CONFLICT(user_id) DO UPDATE SET email=NULL,pending_email=NULL,email_prompt_dismissed=1`, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM recovery_tokens WHERE user_id=?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *store) confirmAccountEmail(ctx context.Context, tokenHash string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var userID int64
	var email string
	err = tx.QueryRowContext(ctx, `SELECT t.user_id,t.email FROM recovery_tokens t JOIN account_settings a ON a.user_id=t.user_id WHERE t.token_hash=? AND t.kind='confirm' AND t.expires_at>? AND a.pending_email=t.email`, tokenHash, time.Now().Unix()).Scan(&userID, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errRecoveryToken
	}
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE account_settings SET email=?,pending_email=NULL WHERE user_id=?`, email, userID); err != nil {
		if isUniqueViolation(err) {
			return 0, errConflict
		}
		return 0, err
	}
	// The previous verified address loses recovery rights immediately.
	if _, err = tx.ExecContext(ctx, `DELETE FROM recovery_tokens WHERE user_id=?`, userID); err != nil {
		return 0, err
	}
	return userID, tx.Commit()
}

func (s *store) issuePasswordReset(ctx context.Context, email, tokenHash string, expires time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var userID int64
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM account_settings WHERE email=?`, email).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM recovery_tokens WHERE user_id=? AND kind='reset'`, userID); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO recovery_tokens(token_hash,user_id,kind,email,expires_at) VALUES (?,?,'reset',?,?)`, tokenHash, userID, email, expires.Unix()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *store) resetAccountPassword(ctx context.Context, tokenHash, passwordHash string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var userID int64
	err = tx.QueryRowContext(ctx, `SELECT t.user_id FROM recovery_tokens t JOIN account_settings a ON a.user_id=t.user_id WHERE t.token_hash=? AND t.kind='reset' AND t.expires_at>? AND a.email=t.email`, tokenHash, time.Now().Unix()).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errRecoveryToken
	}
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, passwordHash, userID); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM recovery_tokens WHERE user_id=?`, userID); err != nil {
		return 0, err
	}
	// A pending email is tied to the old password's authorization.
	if _, err = tx.ExecContext(ctx, `UPDATE account_settings SET pending_email=NULL WHERE user_id=?`, userID); err != nil {
		return 0, err
	}
	return userID, tx.Commit()
}

func (s *store) validPasswordReset(ctx context.Context, tokenHash string) (bool, error) {
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM recovery_tokens t JOIN account_settings a ON a.user_id=t.user_id WHERE t.token_hash=? AND t.kind='reset' AND t.expires_at>? AND a.email=t.email`, tokenHash, time.Now().Unix()).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
