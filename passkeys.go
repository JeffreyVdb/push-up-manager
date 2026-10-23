package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const passkeySchema = `
CREATE TABLE IF NOT EXISTS passkey_users (
 user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 handle BLOB NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS passkeys (
 id TEXT PRIMARY KEY,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 credential TEXT NOT NULL,
 created_at TEXT NOT NULL,
 last_used_at TEXT
);
CREATE INDEX IF NOT EXISTS passkeys_user ON passkeys(user_id);
CREATE TABLE IF NOT EXISTS passkey_ceremonies (
 token_hash TEXT PRIMARY KEY,
 action TEXT NOT NULL,
 origin TEXT NOT NULL,
 user_id INTEGER NOT NULL,
 session_hash TEXT NOT NULL,
 name TEXT NOT NULL,
 data TEXT NOT NULL,
 expires_at INTEGER NOT NULL
);
`

const passkeyCeremonyCookie = "pushup_passkey"
const passkeyCeremonyTTL = 5 * time.Minute

type passkeyService struct {
	store     *store
	publicURL string
}

func newPasskeyService(st *store, publicURL string) *passkeyService {
	return &passkeyService{store: st, publicURL: strings.TrimRight(strings.TrimSpace(publicURL), "/")}
}

// Production origins are configuration, never forwarded headers or Host. An
// unconfigured installation only offers passkeys on localhost. WebAuthn RP IDs
// must be domain names, so numeric loopback addresses cannot be used.
func (p *passkeyService) relyingParty(r *http.Request) (*webauthn.WebAuthn, error) {
	origin := p.publicURL
	if origin == "" {
		host := r.Host
		hostname := host
		if h, _, err := net.SplitHostPort(host); err == nil {
			hostname = h
		}
		hostname = strings.Trim(hostname, "[]")
		if net.ParseIP(hostname) != nil {
			return nil, errors.New("Passkeys need a domain name. For local development, open http://localhost on the same port.")
		}
		if hostname != "localhost" {
			return nil, errors.New("Passkeys need HTTPS. Configure PUSHUP_PUBLIC_URL to enable them.")
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		origin = scheme + "://" + host
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("Passkeys are unavailable: invalid public URL.")
	}
	if net.ParseIP(u.Hostname()) != nil {
		return nil, errors.New("Passkeys need a domain name. For local development, open http://localhost on the same port.")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && u.Hostname() == "localhost") {
		return nil, errors.New("Passkeys require an HTTPS public URL.")
	}
	return webauthn.New(&webauthn.Config{
		RPID: u.Hostname(), RPDisplayName: "Push-Up Counter", RPOrigins: []string{origin},
		AttestationPreference:  protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationRequired},
		Timeouts:               webauthn.TimeoutsConfig{Login: webauthn.TimeoutConfig{Enforce: true, Timeout: passkeyCeremonyTTL}, Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: passkeyCeremonyTTL}},
	})
}

type passkeyUser struct {
	id          int64
	name        string
	handle      []byte
	credentials []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte                         { return u.handle }
func (u *passkeyUser) WebAuthnName() string                       { return u.name }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.name }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func (p *passkeyService) user(ctx context.Context, id int64, initialize bool) (*passkeyUser, error) {
	if initialize {
		handle := make([]byte, 32)
		if _, err := rand.Read(handle); err != nil {
			return nil, err
		}
		if _, err := p.store.db.ExecContext(ctx, `INSERT OR IGNORE INTO passkey_users(user_id, handle) VALUES (?, ?)`, id, handle); err != nil {
			return nil, err
		}
	}
	u := &passkeyUser{id: id}
	if err := p.store.db.QueryRowContext(ctx, `SELECT u.username, p.handle FROM users u JOIN passkey_users p ON p.user_id=u.id WHERE u.id=?`, id).Scan(&u.name, &u.handle); err != nil {
		return nil, err
	}
	rows, err := p.store.db.QueryContext(ctx, `SELECT credential FROM passkeys WHERE user_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			return nil, err
		}
		u.credentials = append(u.credentials, c)
	}
	return u, rows.Err()
}

func (s *server) registerPasskeyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/passkeys", s.auth(s.handlePasskeyList))
	mux.HandleFunc("POST /api/passkeys/register/begin", s.auth(s.csrf(s.handlePasskeyRegisterBegin)))
	mux.HandleFunc("POST /api/passkeys/register/finish", s.auth(s.csrf(s.handlePasskeyRegisterFinish)))
	mux.HandleFunc("DELETE /api/passkeys/{id}", s.auth(s.csrf(s.handlePasskeyDelete)))
	mux.HandleFunc("POST /api/passkeys/login/begin", s.handlePasskeyLoginBegin)
	mux.HandleFunc("POST /api/passkeys/login/finish", s.handlePasskeyLoginFinish)
}

func (s *server) passkeyRP(w http.ResponseWriter, r *http.Request) *webauthn.WebAuthn {
	if s.passkeys == nil {
		writeError(w, http.StatusServiceUnavailable, "Passkeys are unavailable.")
		return nil
	}
	rp, err := s.passkeys.relyingParty(r)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return nil
	}
	return rp
}

type passkeySummary struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
}

func (s *server) handlePasskeyList(w http.ResponseWriter, r *http.Request, sess *session) {
	available := s.passkeys != nil
	if available {
		_, err := s.passkeys.relyingParty(r)
		available = err == nil
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT id,name,created_at,last_used_at FROM passkeys WHERE user_id=? ORDER BY created_at,id`, sess.UserID)
	if err != nil {
		writeError(w, 500, "Could not load passkeys.")
		return
	}
	defer rows.Close()
	keys := []passkeySummary{}
	for rows.Next() {
		var key passkeySummary
		if err := rows.Scan(&key.ID, &key.Name, &key.CreatedAt, &key.LastUsedAt); err != nil {
			writeError(w, 500, "Could not load passkeys.")
			return
		}
		keys = append(keys, key)
	}
	if rows.Err() != nil {
		writeError(w, 500, "Could not load passkeys.")
		return
	}
	writeJSON(w, 200, map[string]any{"available": available, "passkeys": keys})
}

type passkeyPassword struct {
	Password string `json:"password"`
	Name     string `json:"name,omitempty"`
}

func (s *server) confirmPasskeyPassword(w http.ResponseWriter, r *http.Request, sess *session, in *passkeyPassword) bool {
	if !decodeJSON(w, r, in) {
		return false
	}
	_, ok := s.accountReauthenticate(w, r, sess, in.Password)
	return ok
}

func ceremonySessionHash(r *http.Request) string {
	if c, err := r.Cookie(sessionCookie); err == nil {
		return hashToken(c.Value)
	}
	return ""
}
func (s *server) beginPasskeyCeremony(w http.ResponseWriter, r *http.Request, rp *webauthn.WebAuthn, action string, userID int64, name string, data *webauthn.SessionData) bool {
	if !s.accountLimit(w, r, "passkey-begin:"+hashToken(accountRemoteIP(r)), 120, 5*time.Minute) {
		return false
	}
	token, err := randomToken()
	if err != nil {
		writeError(w, 500, "Could not begin passkey request.")
		return false
	}
	raw, err := json.Marshal(data)
	if err != nil {
		writeError(w, 500, "Could not begin passkey request.")
		return false
	}
	_, err = s.store.db.ExecContext(r.Context(), `DELETE FROM passkey_ceremonies WHERE expires_at <= ?`, time.Now().Unix())
	if err == nil {
		result, insertErr := s.store.db.ExecContext(r.Context(), `INSERT INTO passkey_ceremonies(token_hash,action,origin,user_id,session_hash,name,data,expires_at) SELECT ?,?,?,?,?,?,?,? WHERE (SELECT count(*) FROM passkey_ceremonies)<2000`, hashToken(token), action, rp.Config.RPOrigins[0], userID, ceremonySessionHash(r), name, string(raw), time.Now().Add(passkeyCeremonyTTL).Unix())
		err = insertErr
		if err == nil {
			count, _ := result.RowsAffected()
			if count != 1 {
				writeError(w, 429, "Too many passkey requests. Try again shortly.")
				return false
			}
		}
	}
	if err != nil {
		writeError(w, 500, "Could not begin passkey request.")
		return false
	}
	http.SetCookie(w, &http.Cookie{Name: passkeyCeremonyCookie, Value: token, Path: "/api/passkeys/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.secureCookies, MaxAge: int(passkeyCeremonyTTL / time.Second)})
	return true
}

type passkeyCeremony struct {
	data   webauthn.SessionData
	userID int64
	name   string
}

func (s *server) consumePasskeyCeremony(w http.ResponseWriter, r *http.Request, rp *webauthn.WebAuthn, action string, userID int64) *passkeyCeremony {
	cookie, err := r.Cookie(passkeyCeremonyCookie)
	if err != nil || cookie.Value == "" {
		writeError(w, 400, "Passkey request expired. Start again.")
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: passkeyCeremonyCookie, Value: "", Path: "/api/passkeys/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.secureCookies, MaxAge: -1})
	var actualAction, origin, sessionHash, raw string
	var expires int64
	c := &passkeyCeremony{}
	err = s.store.db.QueryRowContext(r.Context(), `DELETE FROM passkey_ceremonies WHERE token_hash=? RETURNING action,origin,user_id,session_hash,name,data,expires_at`, hashToken(cookie.Value)).Scan(&actualAction, &origin, &c.userID, &sessionHash, &c.name, &raw, &expires)
	if err != nil || actualAction != action || origin != rp.Config.RPOrigins[0] || c.userID != userID || sessionHash != ceremonySessionHash(r) || expires <= time.Now().Unix() || json.Unmarshal([]byte(raw), &c.data) != nil {
		writeError(w, 400, "Passkey request expired or does not match this browser. Start again.")
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	return c
}

func (s *server) handlePasskeyRegisterBegin(w http.ResponseWriter, r *http.Request, sess *session) {
	rp := s.passkeyRP(w, r)
	if rp == nil {
		return
	}
	var in passkeyPassword
	if !s.confirmPasskeyPassword(w, r, sess, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = "My passkey"
	}
	if len(in.Name) > 80 {
		writeError(w, 400, "Passkey name must be at most 80 characters.")
		return
	}
	u, err := s.passkeys.user(r.Context(), sess.UserID, true)
	if err != nil {
		writeError(w, 500, "Could not load account.")
		return
	}
	if len(u.credentials) >= 20 {
		writeError(w, 400, "You can save up to 20 passkeys. Remove one first.")
		return
	}
	exclusions := []protocol.CredentialDescriptor{}
	for _, c := range u.credentials {
		exclusions = append(exclusions, c.Descriptor())
	}
	options, data, err := rp.BeginRegistration(u, webauthn.WithExclusions(exclusions))
	if err != nil {
		writeError(w, 500, "Could not begin passkey registration.")
		return
	}
	if s.beginPasskeyCeremony(w, r, rp, "register", sess.UserID, in.Name, data) {
		writeJSON(w, 200, options)
	}
}
func (s *server) handlePasskeyRegisterFinish(w http.ResponseWriter, r *http.Request, sess *session) {
	rp := s.passkeyRP(w, r)
	if rp == nil {
		return
	}
	ceremony := s.consumePasskeyCeremony(w, r, rp, "register", sess.UserID)
	if ceremony == nil {
		return
	}
	u, err := s.passkeys.user(r.Context(), sess.UserID, false)
	if err != nil {
		writeError(w, 500, "Could not load account.")
		return
	}
	c, err := rp.FinishRegistration(u, ceremony.data, r)
	if err != nil {
		writeError(w, 400, "Passkey registration could not be verified. Start again.")
		return
	}
	raw, err := json.Marshal(c)
	if err != nil {
		writeError(w, 500, "Could not save passkey.")
		return
	}
	result, err := s.store.db.ExecContext(r.Context(), `INSERT INTO passkeys(id,user_id,name,credential,created_at) SELECT ?,?,?,?,? WHERE EXISTS(SELECT 1 FROM sessions WHERE token_hash=? AND user_id=? AND expires_at>?) AND (SELECT count(*) FROM passkeys WHERE user_id=?)<20`, base64.RawURLEncoding.EncodeToString(c.ID), sess.UserID, ceremony.name, string(raw), nowUTC(), ceremonySessionHash(r), sess.UserID, nowUTC(), sess.UserID)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, 409, "That passkey is already registered.")
		} else {
			writeError(w, 500, "Could not save passkey.")
		}
		return
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		writeError(w, 409, "Your session changed or the passkey limit was reached. Sign in and try again.")
		return
	}
	writeJSON(w, 201, map[string]bool{"ok": true})
}
func (s *server) handlePasskeyDelete(w http.ResponseWriter, r *http.Request, sess *session) {
	var in passkeyPassword
	if !s.confirmPasskeyPassword(w, r, sess, &in) {
		return
	}
	res, err := s.store.db.ExecContext(r.Context(), `DELETE FROM passkeys WHERE id=? AND user_id=? AND EXISTS(SELECT 1 FROM sessions WHERE token_hash=? AND user_id=? AND expires_at>?)`, r.PathValue("id"), sess.UserID, ceremonySessionHash(r), sess.UserID, nowUTC())
	if err != nil {
		writeError(w, 500, "Could not remove passkey.")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeError(w, 404, "Passkey not found.")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *server) handlePasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	rp := s.passkeyRP(w, r)
	if rp == nil {
		return
	}
	options, data, err := rp.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		writeError(w, 500, "Could not begin passkey sign-in.")
		return
	}
	if s.beginPasskeyCeremony(w, r, rp, "login", 0, "", data) {
		writeJSON(w, 200, options)
	}
}
func (s *server) handlePasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	rp := s.passkeyRP(w, r)
	if rp == nil {
		return
	}
	ceremony := s.consumePasskeyCeremony(w, r, rp, "login", 0)
	if ceremony == nil {
		return
	}
	resolve := func(rawID, handle []byte) (webauthn.User, error) {
		var id int64
		if err := s.store.db.QueryRowContext(r.Context(), `SELECT p.user_id FROM passkeys p JOIN passkey_users u ON u.user_id=p.user_id WHERE p.id=? AND u.handle=?`, base64.RawURLEncoding.EncodeToString(rawID), handle).Scan(&id); err != nil {
			return nil, err
		}
		return s.passkeys.user(r.Context(), id, false)
	}
	verified, c, err := rp.FinishPasskeyLogin(resolve, ceremony.data, r)
	if err != nil || c.Authenticator.CloneWarning {
		writeError(w, 401, "Passkey sign-in could not be verified. Start again.")
		return
	}
	u := verified.(*passkeyUser)
	var old []byte
	for _, existing := range u.credentials {
		if bytes.Equal(existing.ID, c.ID) {
			old, _ = json.Marshal(existing)
			break
		}
	}
	raw, err := json.Marshal(c)
	if err != nil {
		writeError(w, 500, "Could not update passkey.")
		return
	}
	res, err := s.store.db.ExecContext(r.Context(), `UPDATE passkeys SET credential=?,last_used_at=? WHERE id=? AND user_id=? AND credential=?`, string(raw), nowUTC(), base64.RawURLEncoding.EncodeToString(c.ID), u.id, string(old))
	if err != nil {
		writeError(w, 500, "Could not update passkey.")
		return
	}
	changed, _ := res.RowsAffected()
	if changed != 1 {
		writeError(w, 401, "Passkey changed during sign-in. Start again.")
		return
	}
	csrf, err := s.startSession(r.Context(), w, u.id)
	if err != nil {
		writeError(w, 500, "Could not start session.")
		return
	}
	writeJSON(w, 200, sessionResponse{Authenticated: true, UserID: u.id, Username: u.name, CSRFToken: csrf, Today: today(), Build: s.buildID})
}
