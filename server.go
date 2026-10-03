package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// The whole frontend ships inside the binary: HTML, CSS, JS, icons, the web
// app manifest and the service worker.
//
//go:embed web
var webFS embed.FS

const dateLayout = "2006-01-02"

type server struct {
	store         *store
	assets        fs.FS
	buildID       string
	secureCookies bool
	// rewritten holds assets whose contents contain the build ID placeholder.
	rewritten map[string][]byte
	// events fans invalidation hints out to every open page of a user.
	events *eventHub
	// heartbeat is a field only so tests need not wait a quarter minute.
	heartbeat time.Duration
}

func newServer(st *store) *server {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	s := &server{
		store:         st,
		assets:        sub,
		secureCookies: os.Getenv("PUSHUP_SECURE_COOKIES") == "1",
		rewritten:     map[string][]byte{},
		events:        newEventHub(),
		heartbeat:     eventHeartbeat,
	}
	s.buildID = assetsBuildID(sub)
	s.rewriteBuildPlaceholders()
	return s
}

// assetsBuildID hashes every embedded asset so the service worker cache name
// and asset URLs change exactly when the frontend changes.
func assetsBuildID(assets fs.FS) string {
	h := sha256.New()
	_ = fs.WalkDir(assets, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		f, err := assets.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		io.WriteString(h, p)
		_, err = io.Copy(h, f)
		return err
	})
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))[:12]
}

// rewriteBuildPlaceholders substitutes __BUILD__ in the text assets, which is
// how cache busting happens without a frontend build step.
func (s *server) rewriteBuildPlaceholders() {
	for _, name := range []string{"index.html", "sw.js", "app.js", "manifest.webmanifest"} {
		raw, err := fs.ReadFile(s.assets, name)
		if err != nil {
			continue
		}
		if !strings.Contains(string(raw), "__BUILD__") {
			continue
		}
		s.rewritten[name] = []byte(strings.ReplaceAll(string(raw), "__BUILD__", s.buildID))
	}
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("POST /api/register", s.handleRegister)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)

	mux.HandleFunc("GET /api/types", s.auth(s.handleListTypes))
	mux.HandleFunc("POST /api/types", s.auth(s.csrf(s.handleCreateType)))
	mux.HandleFunc("PATCH /api/types/{id}", s.auth(s.csrf(s.handleRenameType)))
	mux.HandleFunc("DELETE /api/types/{id}", s.auth(s.csrf(s.handleDeleteType)))

	mux.HandleFunc("POST /api/reps", s.auth(s.csrf(s.handleAddReps)))
	mux.HandleFunc("DELETE /api/reps/{id}", s.auth(s.csrf(s.handleDeleteRep)))

	mux.HandleFunc("GET /api/friends", s.auth(s.handleFriends))
	mux.HandleFunc("POST /api/friends/requests", s.auth(s.csrf(s.handleSendFriendRequest)))
	mux.HandleFunc("POST /api/friends/requests/{id}/accept", s.auth(s.csrf(s.handleAcceptFriendRequest)))
	mux.HandleFunc("POST /api/friends/requests/{id}/decline", s.auth(s.csrf(s.handleDeclineFriendRequest)))
	mux.HandleFunc("DELETE /api/friends/{id}", s.auth(s.csrf(s.handleRemoveFriend)))

	mux.HandleFunc("GET /api/day/{date}", s.auth(s.handleDay))
	mux.HandleFunc("GET /api/calendar", s.auth(s.handleCalendar))
	mux.HandleFunc("GET /api/overview", s.auth(s.handleOverview))

	mux.HandleFunc("GET /api/events", s.auth(s.handleEvents))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "ok\n")
	})

	mux.HandleFunc("GET /", s.serveAsset)

	// Rejects state-changing cross-origin browser requests; the per-session
	// CSRF token above covers clients that send no Sec-Fetch-Site/Origin.
	return http.NewCrossOriginProtection().Handler(logRequests(mux))
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		// /healthz is noise, and an event stream lives for as long as the page
		// does: one line at disconnect with an enormous dur_ms would say
		// nothing. handleEvents logs its own open and close instead.
		if r.URL.Path == "/healthz" || r.URL.Path == "/api/events" {
			return
		}
		slog.Info("request",
			"method", r.Method, "path", r.URL.Path,
			"status", rec.status, "dur_ms", time.Since(start).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController find the real flusher and write
// deadlines underneath this wrapper, which is what keeps SSE streaming.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// serveAsset serves the embedded frontend with cache headers that keep the
// entry points fresh and let fingerprinted assets live forever.
func (s *server) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" || name == "." {
		name = "index.html"
	}

	body, ok := s.rewritten[name]
	if !ok {
		raw, err := fs.ReadFile(s.assets, name)
		switch {
		case err == nil:
			body = raw
		case isNavigation(r):
			// A deep link boots the app shell; a missing asset must still 404
			// rather than quietly returning HTML with a 200.
			body = s.rewritten["index.html"]
			name = "index.html"
			if body == nil {
				http.NotFound(w, r)
				return
			}
		default:
			http.NotFound(w, r)
			return
		}
	}

	switch {
	case name == "sw.js":
		// Keep the worker URL stable and always revalidated so browsers can
		// discover a new version.
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Service-Worker-Allowed", "/")
	case name == "index.html" || name == "manifest.webmanifest":
		w.Header().Set("Cache-Control", "no-cache")
	case r.URL.Query().Get("v") != "":
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}

	if ct := contentType(name); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, time.Time{}, strings.NewReader(string(body)))
}

func contentType(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".webmanifest":
		return "application/manifest+json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	}
	return ""
}

// isNavigation reports whether the request is a browser navigating to a page,
// as opposed to fetching a subresource.
func isNavigation(r *http.Request) bool {
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" {
		return mode == "navigate"
	}
	// Older browsers and curl: fall back to the Accept header, and never treat
	// a path with a file extension as a page.
	if path.Ext(r.URL.Path) != "" {
		return false
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// auth requires a valid session and passes it to the handler.
func (s *server) auth(next func(http.ResponseWriter, *http.Request, *session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := s.currentSession(r)
		if sess == nil {
			writeError(w, http.StatusUnauthorized, "not signed in")
			return
		}
		next(w, r, sess)
	}
}

// csrf enforces the synchronizer token on state-changing requests.
func (s *server) csrf(next func(http.ResponseWriter, *http.Request, *session)) func(http.ResponseWriter, *http.Request, *session) {
	return func(w http.ResponseWriter, r *http.Request, sess *session) {
		got := r.Header.Get("X-CSRF-Token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRFToken)) != 1 {
			writeError(w, http.StatusForbidden, "invalid CSRF token")
			return
		}
		next(w, r, sess)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON reads a small JSON body.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}
