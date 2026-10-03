package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxRepsPerEntry = 10000

type sessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	Username      string `json:"username,omitempty"`
	CSRFToken     string `json:"csrf_token,omitempty"`
	Today         string `json:"today"`
	DayRolloverMS int64  `json:"day_rollover_ms,omitempty"`
	Build         string `json:"build"`
}

func (s *server) handleSession(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	nextDay := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	resp := sessionResponse{
		Today: now.Format(dateLayout), Build: s.buildID,
		DayRolloverMS: nextDay.Sub(now).Milliseconds(),
	}
	if sess := s.currentSession(r); sess != nil {
		resp.Authenticated = true
		resp.Username = sess.Username
		resp.CSRFToken = sess.CSRFToken
	}
	writeJSON(w, http.StatusOK, resp)
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decodeJSON(w, r, &in) {
		return
	}
	username, err := validateUsername(in.Username)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validatePassword(in.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	hash, err := hashPassword(in.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}
	userID, err := s.store.createUser(r.Context(), username, hash)
	if errors.Is(err, errConflict) {
		writeError(w, http.StatusConflict, "That username is taken. Pick another.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}
	if err := s.store.seedDefaultTypes(r.Context(), userID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not seed push-up types")
		return
	}

	csrf, err := s.startSession(r.Context(), w, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start session")
		return
	}
	writeJSON(w, http.StatusCreated, sessionResponse{
		Authenticated: true, Username: username, CSRFToken: csrf, Today: today(), Build: s.buildID,
	})
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decodeJSON(w, r, &in) {
		return
	}
	u, err := s.store.userByName(r.Context(), strings.TrimSpace(in.Username))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not look up account")
		return
	}
	// Always run a verification so a missing user and a wrong password cost
	// roughly the same.
	stored := "argon2id$v=19$m=65536,t=3,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if u != nil {
		stored = u.Hash
	}
	if !verifyPassword(stored, in.Password) || u == nil {
		writeError(w, http.StatusUnauthorized, errBadCredentials.Error())
		return
	}

	_ = s.store.purgeExpiredSessions(r.Context())
	csrf, err := s.startSession(r.Context(), w, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start session")
		return
	}
	writeJSON(w, http.StatusOK, sessionResponse{
		Authenticated: true, Username: u.Username, CSRFToken: csrf, Today: today(), Build: s.buildID,
	})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_ = s.store.deleteSession(r.Context(), hashToken(c.Value))
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *server) handleListTypes(w http.ResponseWriter, r *http.Request, sess *session) {
	types, err := s.store.listTypes(r.Context(), sess.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load push-up types")
		return
	}
	lastUsed, err := s.store.lastUsedTypeID(r.Context(), sess.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not resolve last used type")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"types": types, "last_used_type_id": lastUsed})
}

type typePayload struct {
	Name string `json:"name"`
}

func validateTypeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 40 {
		return "", errors.New("Name must be 1-40 characters.")
	}
	return name, nil
}

func (s *server) handleCreateType(w http.ResponseWriter, r *http.Request, sess *session) {
	var in typePayload
	if !decodeJSON(w, r, &in) {
		return
	}
	name, err := validateTypeName(in.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := s.store.createType(r.Context(), sess.UserID, name)
	if errors.Is(err, errConflict) {
		writeError(w, http.StatusConflict, "You already have that variation. Use it or pick a different name.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create push-up type")
		return
	}
	// A variation is private, so only this account's other pages care.
	s.notify(sess.UserID)
	writeJSON(w, http.StatusCreated, t)
}

func (s *server) handleRenameType(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in typePayload
	if !decodeJSON(w, r, &in) {
		return
	}
	name, err := validateTypeName(in.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch err := s.store.renameType(r.Context(), sess.UserID, id, name); {
	case errors.Is(err, errConflict):
		writeError(w, http.StatusConflict, "You already have that variation. Use it or pick a different name.")
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "push-up type not found")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not rename push-up type")
	default:
		s.notify(sess.UserID)
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "name": name})
	}
}

func (s *server) handleDeleteType(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	switch err := s.store.deleteType(r.Context(), sess.UserID, id); {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "push-up type not found")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not delete push-up type")
	default:
		// Deleting a variation takes its entries with it, so totals move too.
		s.notifyWithCrew(r.Context(), sess.UserID)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

type repsPayload struct {
	TypeID int64  `json:"type_id"`
	Count  int64  `json:"count"`
	Day    string `json:"day"`
}

func (s *server) handleAddReps(w http.ResponseWriter, r *http.Request, sess *session) {
	var in repsPayload
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Count <= 0 || in.Count > maxRepsPerEntry {
		writeError(w, http.StatusBadRequest, "Reps must be between 1 and 10000. Enter a real set.")
		return
	}
	// The day always defaults to today when the client leaves it empty.
	day := strings.TrimSpace(in.Day)
	if day == "" {
		day = today()
	}
	if !validDay(day) {
		writeError(w, http.StatusBadRequest, "day must be YYYY-MM-DD")
		return
	}
	if day > today() {
		writeError(w, http.StatusBadRequest, "That day has not happened yet. Pick today or earlier.")
		return
	}

	entry, err := s.store.addReps(r.Context(), sess.UserID, in.TypeID, day, in.Count)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "That variation no longer exists. Pick another.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save reps")
		return
	}
	s.notifyWithCrew(r.Context(), sess.UserID)
	writeJSON(w, http.StatusCreated, entry)
}

func (s *server) handleDeleteRep(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	switch err := s.store.deleteEntry(r.Context(), sess.UserID, id); {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "entry not found")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not delete entry")
	default:
		s.notifyWithCrew(r.Context(), sess.UserID)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func (s *server) handleDay(w http.ResponseWriter, r *http.Request, sess *session) {
	day := r.PathValue("date")
	if !validDay(day) {
		writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	entries, err := s.store.entriesForDay(r.Context(), sess.UserID, day)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load entries")
		return
	}
	var total int64
	for _, e := range entries {
		total += e.Count
	}
	writeJSON(w, http.StatusOK, map[string]any{"day": day, "total": total, "entries": entries})
}

// handleCalendar returns per-day totals for one month, plus the padding needed
// to draw a Monday-first grid.
func (s *server) handleCalendar(w http.ResponseWriter, r *http.Request, sess *session) {
	now := time.Now()
	year, err := intParam(r, "year", now.Year())
	if err != nil || year < 1970 || year > 3000 {
		writeError(w, http.StatusBadRequest, "invalid year")
		return
	}
	month, err := intParam(r, "month", int(now.Month()))
	if err != nil || month < 1 || month > 12 {
		writeError(w, http.StatusBadRequest, "invalid month")
		return
	}

	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 1, -1)
	totals, err := s.store.totalsBetween(r.Context(), sess.UserID,
		start.Format(dateLayout), end.Format(dateLayout))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load calendar")
		return
	}

	// Monday-first offset: Go's Sunday==0 becomes 6.
	lead := (int(start.Weekday()) + 6) % 7
	writeJSON(w, http.StatusOK, map[string]any{
		"year":       year,
		"month":      month,
		"days":       end.Day(),
		"lead_blank": lead,
		"today":      today(),
		"totals":     totals,
	})
}

func (s *server) handleOverview(w http.ResponseWriter, r *http.Request, sess *session) {
	st, err := s.store.stats(r.Context(), sess.UserID, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load stats")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

/* ------------------------------------------------------------ friends */

// declineBlockDays is the cooling-off period a declined sender waits out
// before they may ask the same person again.
const declineBlockDays = 7

// declineBlockUntil reports when a decline stops blocking new requests. A
// timestamp it cannot parse fails open: a corrupt row must not block someone
// forever.
func declineBlockUntil(respondedAt string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, respondedAt)
	if err != nil {
		return time.Time{}, false
	}
	return t.Add(declineBlockDays * 24 * time.Hour), true
}

// daysUntil rounds up, so "1 day" never means "any moment now".
func daysUntil(until time.Time) int {
	d := time.Until(until)
	if d <= 0 {
		return 0
	}
	return int((d + 24*time.Hour - time.Nanosecond) / (24 * time.Hour))
}

type friendsResponse struct {
	Friends     []friend        `json:"friends"`
	Incoming    []friendRequest `json:"incoming"`
	Outgoing    []friendRequest `json:"outgoing"`
	Leaderboard []leaderRow     `json:"leaderboard"`
}

// handleFriends answers the whole friends screen in one request: the roster,
// both request queues, and the leaderboard.
func (s *server) handleFriends(w http.ResponseWriter, r *http.Request, sess *session) {
	friends, err := s.store.listFriends(r.Context(), sess.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load friends")
		return
	}
	incoming, err := s.store.incomingRequests(r.Context(), sess.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load friend requests")
		return
	}
	outgoing, err := s.store.outgoingRequests(r.Context(), sess.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load sent requests")
		return
	}
	board, err := s.store.leaderboard(r.Context(), sess.UserID, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load leaderboard")
		return
	}
	writeJSON(w, http.StatusOK, friendsResponse{
		Friends: friends, Incoming: incoming, Outgoing: outgoing, Leaderboard: board,
	})
}

type friendRequestPayload struct {
	Username string `json:"username"`
}

// handleSendFriendRequest applies the whole request policy in one place. The
// checks are ordered deliberately; see the comments on each branch.
func (s *server) handleSendFriendRequest(w http.ResponseWriter, r *http.Request, sess *session) {
	var in friendRequestPayload
	if !decodeJSON(w, r, &in) {
		return
	}
	name := strings.TrimSpace(in.Username)
	if name == "" {
		writeError(w, http.StatusBadRequest, "Type a username to send a request.")
		return
	}

	target, err := s.store.userByName(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not look up that user")
		return
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "No account with that username. Check the spelling.")
		return
	}
	if target.ID == sess.UserID {
		writeError(w, http.StatusBadRequest, "You cannot add yourself. Find someone else.")
		return
	}

	friends, err := s.store.areFriends(r.Context(), sess.UserID, target.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not check that friendship")
		return
	}
	if friends {
		writeError(w, http.StatusConflict, target.Username+" is already on your list.")
		return
	}

	// Mutual intent settles it before any block does: if they already asked
	// you, typing their name is the same answer as tapping accept, and a
	// decline you handed out earlier is moot once they invite you back.
	if theirs, err := s.store.requestBetween(r.Context(), target.ID, sess.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not check pending requests")
		return
	} else if theirs != nil && theirs.Status == "pending" {
		if err := s.store.befriend(r.Context(), sess.UserID, target.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "could not add that friend")
			return
		}
		s.notify(sess.UserID, target.ID)
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "friends", "username": target.Username,
		})
		return
	}

	mine, err := s.store.requestBetween(r.Context(), sess.UserID, target.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not check sent requests")
		return
	}
	switch {
	case mine != nil && mine.Status == "pending":
		writeError(w, http.StatusConflict, "You already asked "+target.Username+". Wait for an answer.")
		return
	case mine != nil && mine.Status == "declined":
		if until, ok := declineBlockUntil(mine.RespondedAt); ok && time.Now().Before(until) {
			left := daysUntil(until)
			unit := "days"
			if left == 1 {
				unit = "day"
			}
			writeError(w, http.StatusForbidden, fmt.Sprintf(
				"%s declined. You can ask again in %d %s.", target.Username, left, unit))
			return
		}
		// The cooling-off period is over: reuse the row rather than piling up
		// a second one for the same direction.
		if err := s.store.renewRequest(r.Context(), mine.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "could not send that request")
			return
		}
	default:
		if _, err := s.store.createRequest(r.Context(), sess.UserID, target.ID); err != nil {
			if errors.Is(err, errConflict) {
				writeError(w, http.StatusConflict, "You already asked "+target.Username+".")
				return
			}
			writeError(w, http.StatusInternalServerError, "could not send that request")
			return
		}
	}
	// The recipient's banner has no poll behind it any more: this is what
	// makes an incoming request appear on a page that is already open. The
	// sender's own other pages gained an ASKED row, so they are told too.
	s.notify(sess.UserID, target.ID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"status": "pending", "username": target.Username,
	})
}

func (s *server) handleAcceptFriendRequest(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	from, name, err := s.store.acceptRequest(r.Context(), id, sess.UserID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "That request is gone. Reload and look again.")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not accept that request")
	default:
		// Both sides gain a board row and lose a queued request.
		s.notify(sess.UserID, from)
		writeJSON(w, http.StatusOK, map[string]any{"status": "friends", "username": name})
	}
}

func (s *server) handleDeclineFriendRequest(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	from, name, err := s.store.declineRequest(r.Context(), id, sess.UserID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "That request is gone. Reload and look again.")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not decline that request")
	default:
		s.notify(sess.UserID, from)
		writeJSON(w, http.StatusOK, map[string]any{"status": "declined", "username": name})
	}
}

// handleRemoveFriend takes the friend's user id, not a friendship id: the pair
// is the identity.
func (s *server) handleRemoveFriend(w http.ResponseWriter, r *http.Request, sess *session) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	switch err := s.store.removeFriend(r.Context(), sess.UserID, id); {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "They are not on your list.")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not remove that friend")
	default:
		// Notify after the cut: they are no longer in each other's friendIDs.
		s.notify(sess.UserID, id)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func intParam(r *http.Request, name string, fallback int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
}

// today is the server's local calendar day; the UI always defaults to it.
func today() string { return time.Now().Format(dateLayout) }

func validDay(day string) bool {
	t, err := time.ParseInLocation(dateLayout, day, time.Local)
	return err == nil && t.Format(dateLayout) == day
}
