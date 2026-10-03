# Push-Up Counter

A push-up tracker that ships as **one static Go binary**. The frontend — HTML,
CSS, JavaScript, fonts, icons, web app manifest, and service worker — is
embedded with `go:embed`, so there is no separate frontend to build, serve, or
deploy. SQLite is linked in pure Go, so the binary has no dynamic dependencies
at all.

```
$ file pushup-counter
pushup-counter: ELF 64-bit LSB executable, x86-64, statically linked, stripped
```

## What it does

- Username + password accounts. No email, no verification, no reset.
- Multiple push-up variations per user. Four are seeded (Standard, Wide,
  Diamond, Incline); add, rename, and delete your own.
- Log reps against a variation. **The day always defaults to today** and the
  variation defaults to the last one you used.
- Month calendar with a per-day heat map: every day shows its total, and the
  colour is never the only channel. A continuous cool-to-hot ramp keeps one
  dark foreground on every day with reps, and an outer border marks today.
- Today / 7-day / month / all-time totals, best day, and current streak.
- **Friends and a shared leaderboard.** Add someone by username; they accept or
  decline from a banner pinned under the top bar. The board ranks you and your
  friends for today, this week (Monday–Sunday), this month, and all time, as
  bars sized against the leader.
- Soft refresh on return and while visible, including the server's midnight.
  After five minutes away, the selected day resets to today. Draft reps stay
  intact. Calendar navigation, tab indicators and changing totals animate,
  with reduced-motion support.
- Mobile-first, installable PWA with an offline app shell.

## Quick start

```sh
./install.sh
```

That builds the binary, installs it to `~/.local/bin`, installs the systemd
user units, enables linger, and starts the socket. Then open
<http://localhost:5554>.

To run it without systemd:

```sh
CGO_ENABLED=0 go build -o pushup-counter .
./pushup-counter                      # listens on :5554
LISTEN_ADDR=127.0.0.1:8080 ./pushup-counter
```

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `LISTEN_ADDR` | `:5554` | Address to bind. **Ignored when systemd passes a socket.** |
| `DB_PATH` | `$STATE_DIRECTORY/pushups.db`, else `$XDG_STATE_HOME/pushup-counter/pushups.db`, else `~/.local/state/pushup-counter/pushups.db` | SQLite database file |
| `PUSHUP_SECURE_COOKIES` | unset | Set to `1` to mark the session cookie `Secure` (only once you are actually serving over HTTPS) |

## Deployment

Two systemd **user** units live in `deploy/`:

- `pushup-counter.socket` — owns the listener on port 5554.
- `pushup-counter.service` — `Type=notify`, socket-activated, not enabled.

Because the socket owns the port, **the service does not need to be enabled**:
the first connection to 5554 starts it. Cold start to first byte is well under
100 ms.

```sh
systemctl --user enable --now pushup-counter.socket   # the socket is what you enable
systemctl --user status pushup-counter.service        # started on demand
journalctl --user -u pushup-counter.service -f
```

The database directory is created and owned by systemd via
`StateDirectory=pushup-counter` (mode `0700`), resolving to
`~/.local/state/pushup-counter`.

Linger keeps the user manager alive across logout and reboot:

```sh
sudo loginctl enable-linger "$USER"
loginctl show-user "$USER" --property=Linger --value   # yes
```

The binary implements the systemd protocol properly: it inherits the listener
via `sd_listen_fds`, reports `READY=1` only once the database is open and it is
serving, answers `WatchdogSec` pings, sends `STOPPING=1`, and shuts down
gracefully on `SIGTERM` with a 25 s drain inside systemd's 30 s stop timeout.

## Friends

Two tables carry it. `friendships` stores one row per pair with the lower id
first (a `CHECK` makes that canonical form impossible to violate), so a
friendship cannot exist twice or half-exist. `friend_requests` stores one row
per *ordered* pair, with a status of `pending` or `declined`.

The rules that follow from that:

- A **decline** is kept, not deleted. Its `responded_at` is what blocks the
  sender from asking that person again for seven days, and the sender is told
  how many days are left rather than left to guess why a retry fails.
- The block is **directional**. Declining someone does not stop you from asking
  *them*.
- **Accepting** and **removing** both delete the request rows in *both*
  directions. That is what makes "removed, so ask again right now" work: a
  stale declined row would otherwise still be sitting there blocking it.
- **Crossed requests settle themselves.** If they already have a request open to
  you, typing their name is the same answer as tapping accept — and it overrides
  a decline you handed out earlier, because they have since invited you back.
- Only the **recipient** answers a request. Handlers match on `to_user`, so the
  sender accepting their own request is a 404.

The leaderboard sums the signed-in user plus their accepted friends over four
windows in one query. Nobody outside that circle is ever counted.

## PWA and HTTPS

Service workers and PWA installation require a secure context. That means:

- `http://localhost:5554` — full PWA: installable, offline app shell. ✅
- `http://192.168.x.y:5554` from your phone — works as an ordinary web app;
  the service worker will not register. ⚠️

To get the installable, offline experience on a phone, put the service behind
HTTPS (Tailscale, a reverse proxy with a certificate, …) and set
`PUSHUP_SECURE_COOKIES=1`. Everything else already works over plain HTTP.

## Security notes

- Passwords are hashed with **Argon2id** (RFC 9106 low-memory parameters:
  m=64 MiB, t=3, p=4), each with a fresh 16-byte salt, stored in a
  self-describing format so parameters can be raised later.
- Sessions are opaque 256-bit random tokens. Only a SHA-256 digest is stored,
  so a database disclosure does not hand out live sessions.
- The session cookie is `HttpOnly`, `SameSite=Lax`, host-only. `Secure` is off
  by default because it would break the plain-HTTP LAN deployment the app is
  built for — over plain HTTP the token is visible to anyone on the network.
- CSRF has two layers: Go's `http.CrossOriginProtection` (`Sec-Fetch-Site` /
  `Origin` checks) plus a per-session synchronizer token required on every
  mutating request.
- All queries are scoped by `user_id`; one account cannot read, modify, or
  delete another's data. The one exception is deliberate and narrow: the
  leaderboard reads the totals of accounts you are *mutually* friends with, and
  nothing else about them.
- Only a request's recipient can accept or decline it; the sender matching on
  `to_user` is what enforces that, so a crafted id gets a 404, not a friendship.

## Development

```sh
go test ./...          # API, auth, isolation, validation, assets, friends
go vet ./...
node --test tests/app.test.cjs  # date rollover, resume, week labels, heat ramp
go run ./tools/genicons   # redraw the app icons
```

Assets are embedded from `web/`. A build ID is derived at startup by hashing
every embedded file, then substituted for the `__BUILD__` placeholder in
`index.html`, `app.js`, and `sw.js`. That means asset URLs and service-worker
cache names change exactly when the frontend changes — no build step, no manual
version bumping. An open tab picks up a new build on its next reload.

`research/` holds the grounding notes the implementation was built from
(Go/SQLite stack, frontend/PWA, systemd deployment). `PRODUCT.md` and
`DESIGN.md` record product truth and the visual system.

## Layout

```
main.go          startup, socket activation, sd_notify, graceful shutdown
server.go        routing, embedded assets, cache headers, auth/CSRF middleware
api.go           JSON handlers
store.go         schema and every SQLite query
auth.go          Argon2id hashing, sessions, cookies, validation
server_test.go   API tests
friends_test.go  friend request protocol and leaderboard scoping tests
web/             the entire frontend (embedded)
deploy/          systemd socket + service units
tools/genicons/  icon generator
```
