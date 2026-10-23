package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type capturedAccountMail struct{ to, subject, body string }
type capturingAccountMailer struct{ messages chan capturedAccountMail }

func (m *capturingAccountMailer) Send(_ context.Context, to, subject, body string) error {
	m.messages <- capturedAccountMail{to, subject, body}
	return nil
}
func (m *capturingAccountMailer) next(t *testing.T, route string) (string, capturedAccountMail) {
	t.Helper()
	select {
	case message := <-m.messages:
		for _, word := range strings.Fields(message.body) {
			prefix := "https://pushups.example" + route + "#token="
			if strings.HasPrefix(word, prefix) {
				return strings.TrimPrefix(word, prefix), message
			}
		}
		t.Fatal("mail missing expected fragment link")
	case <-time.After(3 * time.Second):
		t.Fatal("mail was not delivered")
	}
	return "", capturedAccountMail{}
}
func accountTestServer(t *testing.T) (*httptest.Server, *store, *capturingAccountMailer) {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "account.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := newServer(st)
	mail := &capturingAccountMailer{messages: make(chan capturedAccountMail, 32)}
	s.accounts.mailer = mail
	s.accounts.publicURL = "https://pushups.example"
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	return srv, st, mail
}
func accountStatus(t *testing.T, c *client, method, path string, payload any, want int) map[string]any {
	t.Helper()
	status, body := c.do(method, path, payload)
	if status != want {
		t.Fatalf("%s %s = %d, want %d: %v", method, path, status, want, body)
	}
	return body
}
func accountClearCooldown(t *testing.T, st *store) {
	t.Helper()
	if _, err := st.db.Exec(`DELETE FROM account_rate_limits WHERE key LIKE 'confirm-mail-minute:%'`); err != nil {
		t.Fatal(err)
	}
}

func TestAccountRecoveryFlow(t *testing.T) {
	srv, st, mail := accountTestServer(t)
	alice := newClient(t, srv)
	alice.register("alice-recovery", "old-password")
	body := accountStatus(t, alice, "GET", "/api/account", nil, 200)
	if body["email"] != "" || body["email_verified"] != false || body["email_prompt_dismissed"] != false {
		t.Fatalf("initial account: %v", body)
	}
	accountStatus(t, alice, "POST", "/api/account/email-prompt/dismiss", map[string]any{}, 200)
	accountStatus(t, alice, "POST", "/api/account/email", map[string]string{"email": "OLD@EXAMPLE.COM", "password": "wrong"}, 401)
	body = accountStatus(t, alice, "GET", "/api/account", nil, 200)
	if body["pending_email"] != "" {
		t.Fatal("wrong password changed email")
	}
	accountStatus(t, alice, "POST", "/api/account/email", map[string]string{"email": " OLD@EXAMPLE.COM ", "password": "old-password"}, 200)
	confirm, firstMail := mail.next(t, "/confirm-email")
	if firstMail.to != "old@example.com" {
		t.Fatalf("normalization: %q", firstMail.to)
	}
	body = accountStatus(t, alice, "GET", "/api/account", nil, 200)
	if body["email"] != "" || body["pending_email"] != "old@example.com" || body["email_verified"] != false {
		t.Fatalf("pending account: %v", body)
	}
	var digest string
	if err := st.db.QueryRow(`SELECT token_hash FROM recovery_tokens WHERE kind='confirm'`).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	if digest == confirm || digest != hashToken(confirm) {
		t.Fatal("confirmation stored in plaintext")
	}
	guest := newClient(t, srv)
	accountStatus(t, guest, "POST", "/api/account/email/confirm", map[string]string{"token": confirm}, 200)
	accountStatus(t, guest, "POST", "/api/account/email/confirm", map[string]string{"token": confirm}, 400)
	accountStatus(t, guest, "POST", "/api/forgot-password", map[string]string{"email": "old@example.com"}, 200)
	oldReset, _ := mail.next(t, "/reset-password")
	accountClearCooldown(t, st)
	accountStatus(t, alice, "POST", "/api/account/email", map[string]string{"email": "new@example.com", "password": "old-password"}, 200)
	newConfirm, _ := mail.next(t, "/confirm-email")
	body = accountStatus(t, alice, "GET", "/api/account", nil, 200)
	if body["email"] != "old@example.com" || body["pending_email"] != "new@example.com" || body["email_verified"] != true {
		t.Fatalf("replacement discarded verified address: %v", body)
	}
	accountStatus(t, guest, "POST", "/api/account/email/confirm", map[string]string{"token": newConfirm}, 200)
	accountStatus(t, guest, "POST", "/api/reset-password", map[string]string{"token": oldReset, "password": "new-password"}, 400)
	accountStatus(t, guest, "POST", "/api/forgot-password", map[string]string{"email": "new@example.com"}, 200)
	reset, _ := mail.next(t, "/reset-password")
	if err := st.db.QueryRow(`SELECT token_hash FROM recovery_tokens WHERE kind='reset'`).Scan(&digest); err != nil {
		t.Fatal(err)
	}
	if digest == reset || digest != hashToken(reset) {
		t.Fatal("reset stored in plaintext")
	}
	another := newClient(t, srv)
	login := accountStatus(t, another, "POST", "/api/login", map[string]string{"username": "alice-recovery", "password": "old-password"}, 200)
	another.csrf = login["csrf_token"].(string)
	accountStatus(t, guest, "POST", "/api/reset-password", map[string]string{"token": reset, "password": "short"}, 400)
	accountStatus(t, guest, "POST", "/api/reset-password", map[string]string{"token": reset, "password": "new-password"}, 200)
	for _, c := range []*client{alice, another} {
		accountStatus(t, c, "GET", "/api/account", nil, 401)
	}
	accountStatus(t, guest, "POST", "/api/reset-password", map[string]string{"token": reset, "password": "other-password"}, 400)
	accountStatus(t, guest, "POST", "/api/login", map[string]string{"username": "alice-recovery", "password": "old-password"}, 401)
	login = accountStatus(t, guest, "POST", "/api/login", map[string]string{"username": "alice-recovery", "password": "new-password"}, 200)
	guest.csrf = login["csrf_token"].(string)
	body = accountStatus(t, guest, "GET", "/api/account", nil, 200)
	if body["email_prompt_dismissed"] != true {
		t.Fatalf("prompt dismissal lost across sessions: %v", body)
	}
	accountStatus(t, guest, "DELETE", "/api/account/email", map[string]string{"password": "new-password"}, 200)
	body = accountStatus(t, guest, "GET", "/api/account", nil, 200)
	if body["email"] != "" || body["pending_email"] != "" || body["email_prompt_dismissed"] != true {
		t.Fatalf("removed account: %v", body)
	}
	var sessions, tokens int
	st.db.QueryRow(`SELECT COUNT(*) FROM recovery_tokens`).Scan(&tokens)
	st.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&sessions)
	if tokens != 0 || sessions != 1 {
		t.Fatalf("after reset/removal: %d tokens, %d sessions", tokens, sessions)
	}
}

func TestAccountConfirmationOwnershipExpiryAndResend(t *testing.T) {
	srv, st, mail := accountTestServer(t)
	a := newClient(t, srv)
	a.register("email-owner", "password-one")
	b := newClient(t, srv)
	b.register("email-other", "password-two")
	guest := newClient(t, srv)
	accountStatus(t, a, "POST", "/api/account/email", map[string]string{"email": "shared@example.com", "password": "password-one"}, 200)
	first, _ := mail.next(t, "/confirm-email")
	accountStatus(t, a, "POST", "/api/account/email/resend", map[string]string{"password": "password-one"}, 429)
	accountClearCooldown(t, st)
	accountStatus(t, a, "POST", "/api/account/email/resend", map[string]string{"password": "password-one"}, 200)
	second, _ := mail.next(t, "/confirm-email")
	accountStatus(t, guest, "POST", "/api/account/email/confirm", map[string]string{"token": first}, 400)
	accountStatus(t, guest, "POST", "/api/account/email/confirm", map[string]string{"token": second}, 200)
	accountStatus(t, b, "POST", "/api/account/email", map[string]string{"email": "shared@example.com", "password": "password-two"}, 200)
	collision, _ := mail.next(t, "/confirm-email")
	accountStatus(t, guest, "POST", "/api/account/email/confirm", map[string]string{"token": collision}, 400)
	state := accountStatus(t, b, "GET", "/api/account", nil, 200)
	if state["email"] != "" {
		t.Fatal("duplicate address verified for another account")
	}
	accountClearCooldown(t, st)
	accountStatus(t, b, "POST", "/api/account/email", map[string]string{"email": "expired@example.com", "password": "password-two"}, 200)
	expired, _ := mail.next(t, "/confirm-email")
	if _, err := st.db.Exec(`UPDATE recovery_tokens SET expires_at=? WHERE token_hash=?`, time.Now().Add(-time.Second).Unix(), hashToken(expired)); err != nil {
		t.Fatal(err)
	}
	accountStatus(t, guest, "POST", "/api/account/email/confirm", map[string]string{"token": expired}, 400)
	// An authenticated account cannot use another account's password to remove email.
	accountStatus(t, b, "DELETE", "/api/account/email", map[string]string{"password": "password-one"}, 401)
	if state, err := st.accountState(context.Background(), 1); err != nil || state.Email != "shared@example.com" {
		t.Fatalf("owner changed: %+v %v", state, err)
	}
}

func TestRecoveryStoreInvalidatesBoundTokens(t *testing.T) {
	_, st, _ := accountTestServer(t)
	ctx := context.Background()
	uid, err := st.createUser(ctx, "store-recovery", "old-hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.setPendingEmail(ctx, uid, "pending@example.com", "old-hash", "confirm", time.Now().Add(time.Hour), false); err != nil {
		t.Fatal(err)
	}
	if found, err := st.issuePasswordReset(ctx, "pending@example.com", "reset", time.Now().Add(time.Hour)); err != nil || found {
		t.Fatalf("pending eligible: %v %v", found, err)
	}
	if found, err := st.issuePasswordReset(ctx, "absent@example.com", "reset", time.Now().Add(time.Hour)); err != nil || found {
		t.Fatalf("unknown eligible: %v %v", found, err)
	}
	if _, err := st.confirmAccountEmail(ctx, "confirm"); err != nil {
		t.Fatal(err)
	}
	if found, err := st.issuePasswordReset(ctx, "pending@example.com", "expired-reset", time.Now().Add(-time.Second)); err != nil || !found {
		t.Fatal(err)
	}
	if _, err := st.resetAccountPassword(ctx, "expired-reset", "hijack"); !errors.Is(err, errRecoveryToken) {
		t.Fatalf("expired reset: %v", err)
	}
	if found, err := st.issuePasswordReset(ctx, "pending@example.com", "reset-1", time.Now().Add(time.Hour)); err != nil || !found {
		t.Fatal(err)
	}
	if found, err := st.issuePasswordReset(ctx, "pending@example.com", "reset-2", time.Now().Add(time.Hour)); err != nil || !found {
		t.Fatal(err)
	}
	if _, err := st.resetAccountPassword(ctx, "reset-1", "hijack"); !errors.Is(err, errRecoveryToken) {
		t.Fatalf("old reset replay: %v", err)
	}
	if err := st.setPendingEmail(ctx, uid, "different@example.com", "old-hash", "confirm-new", time.Now().Add(time.Hour), false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.resetAccountPassword(ctx, "reset-2", "new-hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.confirmAccountEmail(ctx, "confirm-new"); !errors.Is(err, errRecoveryToken) {
		t.Fatalf("pending confirmation survived reset: %v", err)
	}
	state, err := st.accountState(ctx, uid)
	if err != nil || state.Email != "pending@example.com" || state.PendingEmail != "" {
		t.Fatalf("state after reset: %+v %v", state, err)
	}
	// Reauthorization cannot race a password reset and retain old authority.
	if err := st.removeAccountEmail(ctx, uid, "old-hash"); !errors.Is(err, errBadCredentials) {
		t.Fatalf("old reauth: %v", err)
	}
}

func TestAccountRecoveryGenericResponsesAndCSRF(t *testing.T) {
	srv, st, _ := accountTestServer(t)
	c := newClient(t, srv)
	c.register("generic-recovery", "password-one")
	var expected string
	for _, email := range []string{"absent@example.com", "pending@example.com", "invalid", ""} {
		body := accountStatus(t, c, "POST", "/api/forgot-password", map[string]string{"email": email}, 200)
		message := body["message"].(string)
		if expected != "" && expected != message {
			t.Fatal("forgot password enumerates accounts")
		}
		expected = message
	}
	c.csrf = ""
	accountStatus(t, c, "POST", "/api/account/email-prompt/dismiss", map[string]any{}, 403)
	accountStatus(t, c, "POST", "/api/account/email", map[string]string{"email": "x@example.com", "password": "password-one"}, 403)
	// Rate state survives new service instances because it lives in SQLite.
	for n := 0; n < 2; n++ {
		allowed, err := st.accountRateAllowed(context.Background(), "persistent", 2, time.Hour)
		if err != nil || !allowed {
			t.Fatal(err)
		}
	}
	allowed, err := (&store{db: st.db}).accountRateAllowed(context.Background(), "persistent", 2, time.Hour)
	if err != nil || allowed {
		t.Fatalf("limiter reset: %v %v", allowed, err)
	}
	if _, err := st.db.Exec(`UPDATE account_rate_limits SET expires_at=0 WHERE key='persistent'`); err != nil {
		t.Fatal(err)
	}
	if allowed, err := st.accountRateAllowed(context.Background(), "persistent", 2, time.Hour); err != nil || !allowed {
		t.Fatalf("limiter never expires: %v %v", allowed, err)
	}
}

func TestAccountSchemaUpgradesExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO users(id,username,password_hash,created_at) VALUES(42,'existing','hash','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err = st.dismissEmailPrompt(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	state, err := st.accountState(context.Background(), 42)
	if err != nil || !state.EmailPromptDismissed {
		t.Fatalf("dismissal lost on reopen: %+v %v", state, err)
	}
	user, err := st.userByName(context.Background(), "existing")
	if err != nil || user == nil || user.ID != 42 || user.Hash != "hash" {
		t.Fatalf("existing user damaged: %+v %v", user, err)
	}
}
