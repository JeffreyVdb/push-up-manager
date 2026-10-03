# Go stack decision for the push-up counter

Research checked 2026-08-24. The target is one `CGO_ENABLED=0` Go binary, with SQLite and embedded PWA assets, serving a small mostly-single-user LAN application on port 5554.

## Executive decisions

- Build with Go **1.27.0**, released 2026-08-19. Pin the toolchain in the project and update it deliberately. The [Go release history](https://go.dev/doc/devel/release) records 1.27.0 as the current major release and its exact release date; the [downloads page](https://go.dev/dl/) lists it as the stable version.
- Use **`modernc.org/sqlite` v1.57.0**. It is a CGo-free port of SQLite, exposes the normal `database/sql` API, and registers the driver name `sqlite`; its current supported SQLite version is 3.53.3. The [package documentation](https://pkg.go.dev/modernc.org/sqlite) gives the version, CGo-free description, driver import/open example, and SQLite support matrix.
- Open the database with the validated modernc DSN shorthands for WAL, foreign keys, and a five-second busy timeout, then set `db.SetMaxOpenConns(1)` and `db.SetMaxIdleConns(1)`. This intentionally serializes all in-process SQLite work and is the right first design for this workload.
- Prefer **Argon2id** using `golang.org/x/crypto/argon2` from x/crypto **v0.55.0**; store the salt and parameters beside the hash. Use bcrypt from the same module only if the simpler self-contained hash format outweighs Argon2id’s memory-hardness.
- Use an opaque, random, server-side session ID in a host-only `HttpOnly` cookie. For the required plain HTTP LAN mode, `Secure` must be false or browsers will not send the cookie; this means the session is exposed to LAN eavesdroppers, so HTTPS/Tailscale remains the security upgrade.
- Use Go’s enhanced ServeMux pattern `POST /api/reps/{id}` and retrieve the wildcard with `r.PathValue("id")`.
- Embed an `embed.FS`, serve `sw.js` at the origin root, set explicit MIME/cache headers, and hash ordinary asset filenames. A service worker will not run on an ordinary `http://192.168.x.x:5554` origin: service workers and installable PWAs need HTTPS, apart from localhost/127.0.0.1.

## 1. Go release and relevant standard-library changes

### Current release

As of the research date, the latest stable release is **Go 1.27.0**, released **2026-08-19**. Go 1.26.0 was released 2026-02-10. These dates are from the Go project’s [official release history](https://go.dev/doc/devel/release), not from a third-party version tracker.

### Go 1.26 and 1.27 relevance

There is no new `go:embed` feature in Go 1.26 or 1.27. `embed.FS` remains the correct standard API for a static web tree; it was introduced earlier and the [current `embed` documentation](https://pkg.go.dev/embed) says that an `embed.FS` implements `io/fs.FS` and can be passed to `net/http` through `http.FS`.

There is also no new ServeMux pattern syntax in 1.26 or 1.27. The enhanced syntax landed in Go 1.22 and remains present. Go 1.26 does have one relevant behavior change: ServeMux trailing-slash redirects now use **307 Temporary Redirect** instead of 301. That matters if a client accidentally posts to a subtree root and the mux redirects it; use exact endpoint patterns where appropriate and do not rely on a redirect for state-changing requests. See the [Go 1.26 release notes](https://go.dev/doc/go1.26) and the [ServeMux documentation](https://pkg.go.dev/net/http#ServeMux).

Go 1.27 adds two `database/sql`/driver-facing APIs:

- [`database/sql.ConvertAssign`](https://pkg.go.dev/database/sql#ConvertAssign) exposes the conversions used by `Rows.Scan` to drivers.
- Drivers may implement [`database/sql/driver.RowsColumnScanner`](https://pkg.go.dev/database/sql/driver#RowsColumnScanner) to scan directly into caller-provided destinations.

Neither changes application-level SQL usage or improves SQLite locking. The app can continue using `QueryContext`, `QueryRowContext`, `ExecContext`, and transactions normally.

## 2. Pure-Go SQLite choices

### Recommended: modernc.org/sqlite v1.57.0

The current [modernc.org/sqlite package page](https://pkg.go.dev/modernc.org/sqlite) reports **v1.57.0**, published 2026-08-19. It describes the package as a **CGo-free port of the C SQLite3 library**, and its support matrix reports SQLite **3.53.3** for the relevant Linux, macOS, Windows, and BSD targets. That satisfies the no-CGo/static-binary constraint when the rest of the program also remains pure Go.

The normal API is:

```go
import (
	"database/sql"
	_ "modernc.org/sqlite"
)

db, err := sql.Open("sqlite", dsn)
```

The driver name is exactly **`sqlite`**, not `sqlite3`. modernc also provides [`NewConnector`](https://pkg.go.dev/modernc.org/sqlite#NewConnector) for callers that need connection-scoped setup, tracing, or metrics before using `sql.OpenDB`.

Modernc’s current DSN is a filename or SQLite `file:` URI followed by query parameters. Its validated shorthand keys include `_busy_timeout`/`_timeout`, `_foreign_keys`/`_fk`, `_journal_mode`/`_journal`, `_synchronous`/`_sync`, `_auto_vacuum`/`_vacuum`, and `_query_only`. It also accepts repeated `_pragma` parameters; modernc prepends `PRAGMA ` to each value, so `_pragma=foreign_keys(1)` becomes `PRAGMA foreign_keys(1)`. These details, including the fixed application order and the fact that raw `_pragma` values are not validated, are documented in [the driver’s `Open` documentation](https://pkg.go.dev/modernc.org/sqlite#Driver.Open).

Important modernc/SQLite gotchas:

1. **Connection scope.** SQLite settings such as `foreign_keys` and `busy_timeout` are connection settings. Put them in the DSN so every connection opened by a pool receives them; do not configure one connection with a startup `Exec` and assume the pool will reuse it.
2. **One connection is not concurrent.** modernc documents that a returned SQLite connection is used by only one goroutine at a time. `*sql.DB` is a pool and is safe for concurrent application use, but an unrestricted pool can open multiple SQLite connections and create avoidable writer contention.
3. **WAL still has one writer.** SQLite’s [WAL documentation](https://sqlite.org/wal.html) says readers and a writer can proceed concurrently, but there can still be only one writer at a time. WAL improves read/write overlap; it does not make concurrent writes independent.
4. **WAL is local-host state.** SQLite says WAL does not work over a network filesystem because its shared-memory index requires all processes to be on the same host. Keep the database, `-wal`, and `-shm` files on local storage and include those sidecar files if copying a live WAL database.
5. **WAL sidecar lifecycle matters.** WAL mode persists in the database, and the WAL file is part of the database’s persistent state while connections are open. Use SQLite’s backup/checkpoint mechanisms or a clean shutdown for backups; never copy only the main database file while a live WAL exists.
6. **Use a fixed SQLite baseline.** SQLite’s WAL page documents a rare WAL-reset corruption bug fixed in SQLite 3.51.3 and later. Since modernc v1.57.0 documents SQLite 3.53.3, it is beyond that fixed version; this version comparison is an inference from the two upstream sources, but it is a useful reason to pin a current modernc release rather than an old one.

### Alternatives

| Option | Current package/version | What it is | Fit for this app |
| --- | --- | --- | --- |
| `zombiezen.com/go/sqlite` | v1.4.2, published 2025-05-23, according to [pkg.go.dev](https://pkg.go.dev/zombiezen.com/go/sqlite) | Low-level `*sqlite.Conn` API, built on modernc, CGo-free; deliberately **does not** provide a `database/sql` driver | Excellent when direct SQLite APIs, blob I/O, custom functions, or explicit connection pools are central. Extra abstraction work for ordinary HTTP CRUD, so not the default here. |
| `github.com/ncruces/go-sqlite3` | v0.35.3, published 2026-08-03, according to [pkg.go.dev](https://pkg.go.dev/github.com/ncruces/go-sqlite3) | CGo-free SQLite wrapper translated from a Wasm build; `database/sql` driver is imported from `/driver` and named `sqlite3` | Viable and actively maintained. It adds a Wasm/VFS layer and documents higher memory usage per database connection, so it is less attractive for this small conventional server unless Wasm parity or its direct SQLite API is specifically valuable. |

Both alternatives can build without CGo. The recommendation is modernc because the application wants idiomatic `database/sql`, straightforward DSN pragmas, and the fewest app-specific connection-management decisions.

## 3. DSN, pragmas, and avoiding `database is locked`

Use modernc’s validated shorthand form:

```text
file:./data/pushups.db?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000
```

Equivalent Go initialization:

```go
db, err := sql.Open("sqlite", "file:./data/pushups.db?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000")
if err != nil {
	return err
}
db.SetMaxOpenConns(1)
db.SetMaxIdleConns(1)
if err := db.Ping(); err != nil {
	return err
}
```

The code block is an implementation sketch for the report; the important decisions are:

- `_journal_mode=WAL` enables/persists WAL. SQLite’s [WAL documentation](https://sqlite.org/wal.html) says the successful result is `wal`, readers do not block the writer in the normal case, and only one writer exists.
- `_foreign_keys=on` enables enforcement on each modernc connection. Foreign-key enforcement is not a database-file-wide default; configure it on every connection.
- `_busy_timeout=5000` makes SQLite wait up to 5,000 ms for a transient busy lock before returning `SQLITE_BUSY`. It is a wait policy, not a substitute for short transactions or serialized writes.
- `SetMaxOpenConns(1)` is the decisive in-process lock-avoidance setting for this workload. The [Go `database/sql` documentation](https://pkg.go.dev/database/sql#DB.SetMaxOpenConns) says the default is unlimited and that this method sets the maximum number of open connections. One connection means the HTTP handlers share one serialized SQLite connection rather than allowing the pool to create competing writers.
- `SetMaxIdleConns(1)` keeps that one connection available rather than allowing a later open to create a new connection with a different per-connection state.
- `Ping` is useful immediately after `sql.Open`, because `sql.Open` may defer the actual connection attempt.

The exact repeated-pragma spelling is also valid:

```text
file:./data/pushups.db?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)
```

For a fixed application string, the validated shorthand form is preferable: modernc validates the values before applying them. `_pragma` is more general but is executed verbatim and should only be built from trusted constants. The syntax and validation behavior are specified in [modernc’s DSN documentation](https://pkg.go.dev/modernc.org/sqlite#Driver.Open).

Operational rules that prevent lock errors:

- Keep write transactions short. Do not hold a transaction while rendering HTML, doing network I/O, hashing a password, or waiting for a browser.
- Always close `Rows`, and always commit or roll back a transaction on every path.
- Make each rep-count update one small transaction, ideally with a single `INSERT`/`UPDATE` or a short read-modify-write transaction.
- Do not run the database on NFS/SMB or another network filesystem when WAL is enabled.
- Treat a busy error after the timeout as an operational error to log/retry at the application boundary, not as a reason to increase the pool size.
- If the app later needs high read concurrency, introduce a deliberate read pool and a dedicated single writer. For the current app, one `*sql.DB` connection is simpler and safer.

Do not add `synchronous=NORMAL` merely because it is common in SQLite examples. Keep SQLite’s durability default unless the product explicitly accepts losing recently committed data after power loss; WAL and `synchronous` are separate choices.

## 4. Session authentication, cookies, and CSRF

### Password hashing

The current [golang.org/x/crypto module page](https://pkg.go.dev/golang.org/x/crypto) reports **v0.55.0**, published 2026-08-11. Use this one module for either supported password-hashing option:

- **Argon2id (recommended):** [`argon2.IDKey`](https://pkg.go.dev/golang.org/x/crypto/argon2#IDKey) implements Argon2id and requires the application to choose/store the salt, parameters, and derived key. The package documentation cites RFC 9106’s recommended options: its lower-memory option is `time=3`, `memory=64*1024` KiB, `threads=4`; use a fresh random salt (16 bytes is a reasonable application choice), a 32-byte derived key, and store an explicit algorithm/parameter record so parameters can be upgraded later. Verify with a constant-time comparison.
- **bcrypt (simpler fallback):** [`bcrypt.GenerateFromPassword`](https://pkg.go.dev/golang.org/x/crypto/bcrypt#GenerateFromPassword) and [`CompareHashAndPassword`](https://pkg.go.dev/golang.org/x/crypto/bcrypt#CompareHashAndPassword) provide a self-describing hash format and easy verification. The current docs say bcrypt accepts at most 72 password bytes and has `DefaultCost=10`; benchmark a cost appropriate for the target machine and reject or normalize passwords beyond the bcrypt limit rather than silently truncating them.

Argon2id is the decision for a new app because it is memory-hard and avoids bcrypt’s 72-byte input ceiling. The application should encode something like `argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>` in its user row; this is a storage convention, not a package-provided high-level password format.

### Session design

Use a random opaque session token generated with [`crypto/rand`](https://pkg.go.dev/crypto/rand), for example 32 random bytes encoded with URL-safe base64. Store the session server-side in SQLite with a user ID, expiry, creation time, and revocation state; storing a SHA-256 digest of the token rather than the raw token limits the impact of a database disclosure. Rotate the token after login and delete/revoke it on logout. Do not put the password hash or user identity in a client-readable cookie, and do not need JWTs for this single-process app.

### Cookie for plain HTTP on port 5554

For the explicitly requested `http://<LAN-host>:5554` deployment, use:

```text
Name:     session_id
Path:     /
Domain:   omitted (host-only)
HttpOnly: true
SameSite: Lax
Secure:   false (required for ordinary HTTP)
```

The `Secure` flag means browsers send the cookie only over HTTPS (with localhost exceptions). [MDN’s Set-Cookie reference](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie) also notes that insecure HTTP sites cannot set Secure cookies. Therefore setting `Secure=true` for a normal LAN HTTP URL will usually break login persistence; setting it false is correct for functionality but does not make the connection safe. Anyone able to observe LAN traffic can steal the session token. For a stronger deployment, bind to loopback or put the service behind HTTPS/Tailscale and then turn `Secure=true`.

`SameSite=Lax` is a good default for a normal web UI: it suppresses cookies on cross-site unsafe requests while still allowing normal top-level navigation. `Strict` is stronger but can be inconvenient when entering the app from another site. `SameSite=None` is unnecessary here and requires `Secure`. Keep `HttpOnly=true`; there is no reason for frontend JavaScript to read the session token. Omit `Domain` so the cookie is host-only. Do not use a `__Host-` cookie name in the plain-HTTP mode because that prefix requires Secure, Path=/, and a secure origin.

### CSRF

Use two layers:

1. Wrap the mux with Go 1.25+ [`http.NewCrossOriginProtection().Handler(mux)`](https://pkg.go.dev/net/http#CrossOriginProtection). The current Go docs say it rejects non-safe cross-origin browser requests using `Sec-Fetch-Site` or an `Origin`/`Host` comparison, while always allowing GET, HEAD, and OPTIONS. Never mutate state on those safe methods. Requests without those browser headers are allowed, so this is a strong browser-origin check, not a complete replacement for an application token.
2. Add a synchronizer token for every state-changing `POST`, `PUT`, `PATCH`, and `DELETE`. Generate it with `crypto/rand`, store it with the server-side session (or encrypt/sign a carefully designed token), render it into HTML forms, and require the matching `X-CSRF-Token` header for SPA `fetch` calls. Compare in constant time and return 403 on failure. `SameSite=Lax` is defense in depth, not the sole CSRF control.

This combination is small enough for the app and remains useful if a non-browser client, an old browser, or a future cookie policy bypasses the browser-origin signal. CSRF specifically targets state-changing requests whose cookies are sent automatically; see the [OWASP CSRF description](https://owasp.org/www-community/attacks/csrf).

## 5. ServeMux enhanced routing

Yes, this exact pattern is valid on the chosen toolchain:

```go
mux.HandleFunc("POST /api/reps/{id}", func(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// validate id, authenticate, check CSRF, then write the rep
})
```

The current [ServeMux documentation](https://pkg.go.dev/net/http#ServeMux) defines the general form as `[METHOD ][HOST]/[PATH]`, says a method must be followed by a space or tab, and lists method/path wildcards. `{id}` is a single path segment; `{rest...}` is a remainder wildcard and must be the final segment. `r.PathValue("id")` returns the wildcard match.

Pitfalls:

- A pattern with no method matches every method. `GET` also matches `HEAD`; other method patterns match exactly.
- Wildcards must be complete path segments and their names must be valid Go identifiers. `/{id}` is valid; `/push-{id}` is not.
- Matching unescapes path segments. Do not use a wildcard as a license to accept arbitrary IDs; validate the resulting value and use a typed ID parser.
- Invalid or conflicting patterns panic during registration. Test all patterns at startup.
- A trailing slash pattern denotes a subtree and can trigger a redirect. Go 1.26 changed that redirect to 307, which preserves the method/body but still makes an extra request; prefer explicit patterns for API endpoints.
- The query string is not part of the pattern. Parse and validate query parameters separately.

Do not set `GODEBUG=httpmuxgo121=1`; that compatibility switch restores old pre-1.22 behavior and makes wildcard patterns ordinary literal segments.

## 6. `go:embed`, service worker, manifest, and cache busting

### Layout and serving

Keep the deployable web tree in the package’s module, for example:

```text
web/
  index.html
  app.7e31c9.js
  style.2a08d4.css
  sw.js
  manifest.webmanifest
  icons/icon-192.png
  icons/icon-512.png
```

Embed the tree as an `embed.FS` and serve it with `http.FileServer(http.FS(...))`. The [`embed` package documentation](https://pkg.go.dev/embed) says directory patterns are recursive, exclude dot/underscore files by default, and expose an `io/fs.FS` suitable for `net/http`. If the variable embeds `web`, strip that `web` prefix with `fs.Sub` before passing it to the file server so URLs remain `/index.html`, `/sw.js`, and `/manifest.webmanifest`.

Serve the service worker at **`/sw.js`**, not only at `/static/sw.js`, and register it with an explicit root scope. A service worker’s default scope is the directory containing the script; [MDN’s `register()` documentation](https://developer.mozilla.org/en-US/docs/Web/API/ServiceWorkerContainer/register) explains that a root script naturally controls the whole origin, while a broader scope for a nested script requires `Service-Worker-Allowed`.

Serve the manifest as `/manifest.webmanifest`, link it from every HTML page with `<link rel="manifest" href="/manifest.webmanifest">`, and set `Content-Type: application/manifest+json`. [MDN’s manifest reference](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Manifest) identifies `.webmanifest` and that media type as the standard deployment form. Include at least a name/short name, `start_url`, `display`, and 192px/512px icons for Chromium installability; the [PWA installability guide](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Guides/Making_PWAs_installable) lists those requirements.

### The LAN HTTP limitation

Service-worker registration is restricted to secure contexts. Installable PWAs require HTTPS, with localhost/127.0.0.1 allowed for local development; [MDN’s service-worker documentation](https://developer.mozilla.org/en-US/docs/Web/API/ServiceWorkerContainer/register) and [PWA installability guide](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Guides/Making_PWAs_installable) state those requirements. Therefore:

- `http://localhost:5554` or `http://127.0.0.1:5554` can use a service worker in supported browsers.
- `http://192.168.x.x:5554` or `http://hostname-on-LAN:5554` cannot rely on a service worker or PWA installation.
- Embedding the files is still correct and the app works as a normal web app over HTTP; use HTTPS/Tailscale later if offline/PWA behavior is required across the LAN.

### Cache policy

Use content hashes in the filenames of ordinary assets (`app.<hash>.js`, `style.<hash>.css`, icons if they change). Reference those exact names from `index.html` and the service worker. Set long-lived caching on hashed assets, for example `Cache-Control: public, max-age=31536000, immutable`.

Keep the small mutable entry points at stable URLs and revalidate them:

- `/index.html`: `Cache-Control: no-cache` (or `max-age=0, must-revalidate`), optionally with a content ETag.
- `/manifest.webmanifest`: `Cache-Control: no-cache`.
- `/sw.js`: `Cache-Control: no-cache` and optionally an ETag. Keep the URL stable so browsers can discover a changed worker; put the build ID in the worker’s cache name and precache list.

On service-worker install, precache only the immutable hashed shell assets and use a cache name such as `pushup-shell-<build-id>`. On activate, remove old `pushup-shell-*` caches. Do not precache authenticated HTML or API responses, and do not cache session-bearing API responses unless the data policy explicitly calls for it. A cache-first strategy otherwise remains stale until a new worker is installed; MDN calls out that cache-first content is not refreshed until the service worker version changes in its [PWA caching guide](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Guides/Caching).

Register with `updateViaCache: "none"` if rapid worker updates matter; [MDN documents](https://developer.mozilla.org/en-US/docs/Web/API/ServiceWorkerContainer/register) that this forces the worker script and its imports to update from the network. The service-worker script itself should still be served with `no-cache` and the worker should change its cache version whenever the precached asset set changes.

## Sources

- [Go release history](https://go.dev/doc/devel/release) — Go 1.27.0 and 1.26.0 release dates.
- [Go downloads](https://go.dev/dl/) — current stable version.
- [Go 1.26 release notes](https://go.dev/doc/go1.26) and [Go 1.27 release notes](https://go.dev/doc/go1.27) — standard-library changes.
- [`embed` package](https://pkg.go.dev/embed), [`net/http` ServeMux](https://pkg.go.dev/net/http#ServeMux), and [`database/sql`](https://pkg.go.dev/database/sql) — standard APIs and behavior.
- [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) — current version, SQLite version, API, driver name, DSN parameters.
- [SQLite WAL](https://sqlite.org/wal.html), [SQLite pragmas](https://sqlite.org/pragma.html), and [SQLite transactions](https://sqlite.org/lang_transaction.html) — locking, WAL, and pragma semantics.
- [`zombiezen.com/go/sqlite`](https://pkg.go.dev/zombiezen.com/go/sqlite) and [`github.com/ncruces/go-sqlite3`](https://pkg.go.dev/github.com/ncruces/go-sqlite3) — alternative versions and APIs.
- [`golang.org/x/crypto`](https://pkg.go.dev/golang.org/x/crypto), [`bcrypt`](https://pkg.go.dev/golang.org/x/crypto/bcrypt), and [`argon2`](https://pkg.go.dev/golang.org/x/crypto/argon2) — current module and password APIs.
- [`net/http.Cookie`](https://pkg.go.dev/net/http#Cookie), [MDN Set-Cookie](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie), [Go CrossOriginProtection](https://pkg.go.dev/net/http#CrossOriginProtection), and [OWASP CSRF](https://owasp.org/www-community/attacks/csrf) — session cookie and CSRF guidance.
- [MDN service-worker registration](https://developer.mozilla.org/en-US/docs/Web/API/ServiceWorkerContainer/register), [MDN PWA installability](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Guides/Making_PWAs_installable), [MDN manifest](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Manifest), and [MDN PWA caching](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Guides/Caching) — embed/serve/cache constraints for the PWA layer.
