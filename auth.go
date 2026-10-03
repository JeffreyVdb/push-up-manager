package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/argon2"
)

const (
	sessionCookie = "pushup_session"
	sessionTTL    = 30 * 24 * time.Hour

	// RFC 9106 lower-memory parameters.
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

var errBadCredentials = errors.New("Wrong username or password. Check both and try again.")

// hashPassword returns a self-describing Argon2id record so the parameters can
// be raised later without invalidating existing hashes.
func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false
	}
	var memory uint32
	var timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken stores only a digest, so a database leak does not hand out live
// sessions.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// startSession creates a session row and sets the cookie.
func (s *server) startSession(ctx context.Context, w http.ResponseWriter, userID int64) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := s.store.createSession(ctx, userID, hashToken(token), csrf, sessionTTL); err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure is deliberately off: the service is reachable over plain
		// HTTP on a LAN port. Put it behind HTTPS/Tailscale and set
		// PUSHUP_SECURE_COOKIES=1 to harden it.
		Secure:  s.secureCookies,
		Expires: time.Now().Add(sessionTTL),
		MaxAge:  int(sessionTTL / time.Second),
	})
	return csrf, nil
}

func (s *server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureCookies,
		MaxAge:   -1,
	})
}

// currentSession resolves the session cookie, or nil when unauthenticated.
func (s *server) currentSession(r *http.Request) *session {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	sess, err := s.store.sessionByHash(r.Context(), hashToken(c.Value))
	if err != nil {
		return nil
	}
	return sess
}

const (
	minUsernameLen = 3
	maxUsernameLen = 32
	minPasswordLen = 8
	maxPasswordLen = 256
)

func validateUsername(name string) (string, error) {
	name = strings.TrimSpace(name)
	if len(name) < minUsernameLen || len(name) > maxUsernameLen {
		return "", fmt.Errorf("Username must be %d-%d characters.", minUsernameLen, maxUsernameLen)
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '.' {
			return "", errors.New("Username may only contain letters, digits, and _ - . Remove anything else.")
		}
	}
	return name, nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLen || len(password) > maxPasswordLen {
		return fmt.Errorf("Password must be %d-%d characters. Make it longer.", minPasswordLen, maxPasswordLen)
	}
	return nil
}
