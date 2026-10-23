package main

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type accountService struct {
	store     *store
	mailer    mailSender
	publicURL string
	mailOnce  sync.Once
	mailSlots chan struct{}
}

func (a *accountService) available() bool { return a != nil && a.mailer != nil && a.publicURL != "" }

func (s *server) registerAccountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/account", s.auth(s.handleAccount))
	mux.HandleFunc("POST /api/account/email", s.auth(s.csrf(s.handleSetEmail)))
	mux.HandleFunc("DELETE /api/account/email", s.auth(s.csrf(s.handleRemoveEmail)))
	mux.HandleFunc("POST /api/account/email/resend", s.auth(s.csrf(s.handleResendEmail)))
	mux.HandleFunc("POST /api/account/email-prompt/dismiss", s.auth(s.csrf(s.handleDismissEmailPrompt)))
	mux.HandleFunc("POST /api/account/email/confirm", s.handleConfirmEmail)
	mux.HandleFunc("POST /api/forgot-password", s.handleForgotPassword)
	mux.HandleFunc("POST /api/reset-password", s.handleResetPassword)
}

func (s *server) handleAccount(w http.ResponseWriter, r *http.Request, sess *session) {
	state, err := s.store.accountState(r.Context(), sess.UserID)
	if err != nil {
		writeError(w, 500, "Could not load account settings.")
		return
	}
	state.RecoveryAvailable = s.accounts.available()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, state)
}

func (s *server) handleDismissEmailPrompt(w http.ResponseWriter, r *http.Request, sess *session) {
	if err := s.store.dismissEmailPrompt(r.Context(), sess.UserID); err != nil {
		writeError(w, 500, "Could not save your preference.")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

type accountEmailPayload struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func accountRemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *server) accountLimit(w http.ResponseWriter, r *http.Request, key string, limit int, window time.Duration) bool {
	allowed, err := s.store.accountRateAllowed(r.Context(), key, limit, window)
	if err != nil {
		writeError(w, 503, "Please try again shortly.")
		return false
	}
	if !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(window/time.Second)))
		writeError(w, 429, "Too many attempts. Please try again later.")
		return false
	}
	return true
}

func (s *server) accountReauthenticate(w http.ResponseWriter, r *http.Request, sess *session, password string) (string, bool) {
	if !s.accountLimit(w, r, "reauth-ip:"+hashToken(accountRemoteIP(r)), 30, 15*time.Minute) || !s.accountLimit(w, r, "reauth-user:"+strconv.FormatInt(sess.UserID, 10), 12, 15*time.Minute) {
		return "", false
	}
	if len(password) > maxPasswordLen {
		writeError(w, 401, "Your current password is incorrect.")
		return "", false
	}
	hash, err := s.store.accountPasswordHash(r.Context(), sess.UserID)
	if err != nil {
		writeError(w, 500, "Could not check your password.")
		return "", false
	}
	if !verifyPassword(hash, password) {
		writeError(w, 401, "Your current password is incorrect.")
		return "", false
	}
	return hash, true
}

func (s *server) handleSetEmail(w http.ResponseWriter, r *http.Request, sess *session) {
	s.sendEmailConfirmation(w, r, sess, false)
}
func (s *server) handleResendEmail(w http.ResponseWriter, r *http.Request, sess *session) {
	s.sendEmailConfirmation(w, r, sess, true)
}

func (s *server) sendEmailConfirmation(w http.ResponseWriter, r *http.Request, sess *session, resend bool) {
	if !s.accounts.available() {
		writeError(w, 503, "Email recovery is not configured yet.")
		return
	}
	var in accountEmailPayload
	if !decodeJSON(w, r, &in) {
		return
	}
	hash, ok := s.accountReauthenticate(w, r, sess, in.Password)
	if !ok {
		return
	}
	var email string
	if resend {
		state, err := s.store.accountState(r.Context(), sess.UserID)
		if err != nil {
			writeError(w, 500, "Could not load your pending email.")
			return
		}
		email = state.PendingEmail
		if email == "" {
			writeError(w, 400, "There is no email awaiting confirmation.")
			return
		}
	} else {
		var err error
		email, err = normalizeAccountEmail(in.Email)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}
	key := strconv.FormatInt(sess.UserID, 10)
	if !s.accountLimit(w, r, "confirm-mail-hour:"+key, 5, time.Hour) || !s.accountLimit(w, r, "confirm-mail-minute:"+key, 1, time.Minute) {
		return
	}
	token, err := randomToken()
	if err != nil {
		writeError(w, 500, "Could not create an email confirmation.")
		return
	}
	err = s.store.setPendingEmail(r.Context(), sess.UserID, email, hash, hashToken(token), time.Now().Add(24*time.Hour), resend)
	if errors.Is(err, errConflict) {
		writeError(w, 409, "That email is already verified for your account.")
		return
	}
	if err != nil {
		writeError(w, 500, "Could not save your email.")
		return
	}
	link := s.accounts.publicURL + "/confirm-email#token=" + token
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err = s.accounts.mailer.Send(ctx, email, "Confirm your Push-ups recovery email", "Confirm this address to use it for password recovery:\n\n"+link+"\n\nThis link expires in 24 hours. If you did not request it, ignore this email."); err != nil {
		writeError(w, 503, "Could not send the confirmation email. Try resending it shortly.")
		return
	}
	s.notify(sess.UserID)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) handleRemoveEmail(w http.ResponseWriter, r *http.Request, sess *session) {
	var in struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	hash, ok := s.accountReauthenticate(w, r, sess, in.Password)
	if !ok {
		return
	}
	if err := s.store.removeAccountEmail(r.Context(), sess.UserID, hash); err != nil {
		writeError(w, 500, "Could not remove your recovery email.")
		return
	}
	s.notify(sess.UserID)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func validAccountToken(token string) bool {
	if len(token) != 43 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) == 32
}

func (s *server) handleConfirmEmail(w http.ResponseWriter, r *http.Request) {
	if !s.accountLimit(w, r, "confirm-ip:"+hashToken(accountRemoteIP(r)), 30, 15*time.Minute) {
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !validAccountToken(in.Token) {
		writeError(w, 400, "This confirmation link is invalid or expired. Request a new one in Account.")
		return
	}
	userID, err := s.store.confirmAccountEmail(r.Context(), hashToken(in.Token))
	if errors.Is(err, errRecoveryToken) || errors.Is(err, errConflict) {
		writeError(w, 400, "This address could not be confirmed. Request a new link or use another address in Account.")
		return
	}
	if err != nil {
		writeError(w, 500, "Could not confirm the email. Please try again.")
		return
	}
	s.notify(userID)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

const forgotPasswordMessage = "If that address is verified for an account, a password reset link will arrive shortly."

func (s *server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	// Limits and all address states return the same response, including disabled
	// delivery. SMTP runs outside the request so network timing cannot reveal an account.
	respond := func() { writeJSON(w, 200, map[string]any{"ok": true, "message": forgotPasswordMessage}) }
	email, err := normalizeAccountEmail(in.Email)
	if err != nil || !s.accounts.available() {
		respond()
		return
	}
	ipAllowed, err := s.store.accountRateAllowed(r.Context(), "forgot-ip:"+hashToken(accountRemoteIP(r)), 20, time.Hour)
	if err != nil || !ipAllowed {
		respond()
		return
	}
	emailAllowed, err := s.store.accountRateAllowed(r.Context(), "forgot-email:"+hashToken(email), 3, time.Hour)
	if err != nil || !emailAllowed {
		respond()
		return
	}
	a := s.accounts
	a.mailOnce.Do(func() { a.mailSlots = make(chan struct{}, 4) })
	select {
	case a.mailSlots <- struct{}{}:
		go func() {
			defer func() { <-a.mailSlots }()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			token, err := randomToken()
			if err != nil {
				return
			}
			found, err := a.store.issuePasswordReset(ctx, email, hashToken(token), time.Now().Add(30*time.Minute))
			if err != nil || !found {
				return
			}
			link := a.publicURL + "/reset-password#token=" + token
			_ = a.mailer.Send(ctx, email, "Reset your Push-ups password", "Choose a new password using this link:\n\n"+link+"\n\nThis link expires in 30 minutes and can be used once. If you did not request it, ignore this email.")
		}()
	default:
	}
	respond()
}

func (s *server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if !s.accountLimit(w, r, "reset-ip:"+hashToken(accountRemoteIP(r)), 20, 15*time.Minute) {
		return
	}
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !validAccountToken(in.Token) {
		writeError(w, 400, "This reset link is invalid or expired. Request a new one.")
		return
	}
	if err := validatePassword(in.Password); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	valid, err := s.store.validPasswordReset(r.Context(), hashToken(in.Token))
	if err != nil {
		writeError(w, 500, "Could not reset your password. Please try again.")
		return
	}
	if !valid {
		writeError(w, 400, "This reset link is invalid or expired. Request a new one.")
		return
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		writeError(w, 500, "Could not save your password.")
		return
	}
	userID, err := s.store.resetAccountPassword(r.Context(), hashToken(in.Token), hash)
	if errors.Is(err, errRecoveryToken) {
		writeError(w, 400, "This reset link is invalid or expired. Request a new one.")
		return
	}
	if err != nil {
		writeError(w, 500, "Could not reset your password. Please try again.")
		return
	}
	s.clearSessionCookie(w)
	s.notify(userID)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
