# Realtime invalidation over Server-Sent Events

Research checked **2026-08-25**. GitHub star counts and activity dates are a
point-in-time snapshot. The target is this repository's Go 1.27 `net/http`
server, cookie/session authentication, vanilla-JavaScript frontend, embedded
service worker, systemd socket activation, and single static binary.

## Decision

**Keep SQLite and add a small, application-owned SSE invalidation hub. Use the
Go standard library for the first implementation.** None of the established Go
SSE libraries satisfies all three of this app's important semantics better than
a small local module:

1. authorization and fan-out are by authenticated user and friendship;
2. notifications are deliberately lossy and coalescing (one pending
   "something changed" signal is enough); and
3. a slow or suspended mobile client must never block a database mutation or a
   different client.

The browser already owns the difficult client behavior: `EventSource`
reconnects, carries the last event ID when IDs are used, and parses the event
stream. The [HTML Standard defines the stream format, reconnection, `204` stop
response, and `Last-Event-ID`](https://html.spec.whatwg.org/dev/server-sent-events.html).
On the Go side, [`http.ResponseController`](https://pkg.go.dev/net/http#ResponseController)
provides wrapper-aware `Flush` and per-write deadlines. What remains is mostly
the app's domain-specific routing policy, which no generic SSE dependency can
remove.

If a dependency is preferred to keep the wire-format, heartbeat, retry, and
write-deadline mechanics outside the app, the best fit is
[`go.jetify.com/sse` v0.1.0](https://github.com/jetify-com/sse/tree/v0.1.0)
**plus the same small local user hub**. It is a better technical fit than the
more popular broker libraries, but its single v0.x release and 119-star adoption
make it a conscious stability trade-off rather than the default recommendation.

The best-established alternative is
[`github.com/tmaxmax/go-sse` v0.11.0](https://github.com/tmaxmax/go-sse/tree/v0.11.0),
but its built-in `Joe` provider synchronously writes every subscriber. That is
the wrong backpressure behavior for suspended phones and multiple devices.

## What “realtime” should mean here

An SSE message should be an **invalidation hint**, not the database change
itself. After a committed mutation, notify:

- the account whose own day/calendar/overview/types changed; and
- the friends whose Crew view is affected.

The receiving page debounces hints and calls the existing JSON loaders. If a
hint is dropped, the stream reconnects, the tab becomes visible, or the browser
comes back from iOS suspension, do one full refresh. This makes the SQLite
database and existing authorization rules remain the source of truth and makes
replay history unnecessary.

The browser connection is one `EventSource("/api/events")` **per open page**.
Every page authenticated as the same user subscribes to the same user ID in the
hub, so laptop, phone, tablet, and multiple tabs all receive the signal. The
[`EventSource` constructor](https://developer.mozilla.org/en-US/docs/Web/API/EventSource/EventSource)
only accepts a URL and the `withCredentials` option; it cannot attach an
application CSRF or authorization header. That fits this same-origin app:
the existing `HttpOnly` session cookie is sent on the GET, and GET does not need
the mutation-only CSRF header.

No event ID or replay buffer is needed. The `open` event (and the existing
`visibilitychange` hook) should trigger a full refresh, so reconnection is
self-healing even when the process restarted and forgot every notification.

## Library/activity snapshot

The “latest push” column is the default-repository `pushed_at` value from
GitHub's first-party repository API, checked 2026-08-25. A recent automation or
branch push is not necessarily recent library code, so the notes also inspect
the latest default-branch commits and tags.

| Option | Stars | Latest repository push | Latest relevant tag/default-branch code | License / dependencies | Result |
| --- | ---: | --- | --- | --- | --- |
| Go `net/http` | — | Go 1.27.0 released 2026-08-19 | Current standard library | BSD-3-Clause; none | **Recommended** |
| [`tmaxmax/go-sse`](https://api.github.com/repos/tmaxmax/go-sse) | 526 | 2025-05-13 UTC | v0.11.0, 2025-05-14 | MIT; zero module dependencies | Best adoption/architecture, but reject default broker semantics |
| [`jetify-com/sse`](https://api.github.com/repos/jetify-com/sse) | 119 | 2025-12-01 UTC | v0.1.0, 2025-05-24; later pushes are dependency/lint maintenance | Apache-2.0; no non-test runtime dependencies | **Best optional transport helper** |
| [`r3labs/sse`](https://api.github.com/repos/r3labs/sse) | 1,045 | 2024-06-26 UTC | v2.10.0/default branch, 2023-01-18 | MPL-2.0; `cenkalti/backoff.v1` | Popular but stale and a poor wrapper/backpressure fit |
| [`antage/eventsource`](https://api.github.com/repos/antage/eventsource) | 498 | 2024-04-09 UTC | default-branch code, 2022-04-22 | MIT; zero dependencies | Reject: hijacks and hand-writes HTTP/1.1 |
| [`gin-contrib/sse`](https://api.github.com/repos/gin-contrib/sse) | 153 | 2026-08-24 UTC | v1.1.1, 2026-03-28 | MIT; test dependencies only | Active encoder/decoder, not a connection or broker library |
| [`mroth/sseserver`](https://api.github.com/repos/mroth/sseserver) | 115 | 2026-07-20 UTC | recent commits are GitHub Actions bumps; latest tag v1.1.2 is 2023-08-02 | AGPL-3.0; `go.rice` and old `azer` packages | Reject: license, weight, and dated architecture |

The Go version/date comes from the [official Go release
history](https://go.dev/doc/devel/release). Star counts are not a quality metric,
but they answer the requested adoption check. On this niche, 500–1,000 stars is
the established tier; Jetify's 119 stars is promising but not equivalent.

## Community and production precedent

The standard-library design is not novel. There are several examples of the
same basic shape in articles and real Go applications: an `http.Handler`, one
channel or client object per open page, a registry/broker, request-context
cleanup, and browser `EventSource`. These are useful experience reports, not
protocol authorities; transport details below are still checked against Go's
`net/http` documentation and the WHATWG HTML Standard.

| Example | Date | Relevant pattern | Adopt, adapt, or reject |
| --- | --- | --- | --- |
| [`zserge/pennybase`](https://github.com/zserge/pennybase) [broker](https://github.com/zserge/pennybase/blob/d65c07d3a4ecae1365ab1f169625907f08c29122/pennybase.go#L512-L551), [SSE handler](https://github.com/zserge/pennybase/blob/d65c07d3a4ecae1365ab1f169625907f08c29122/pennybase.go#L735-L769), and [chat UI](https://github.com/zserge/pennybase/blob/d65c07d3a4ecae1365ab1f169625907f08c29122/examples/chat/templates/index.html#L5-L25) | 2025 | A tiny authenticated, zero-dependency Go backend keys subscriptions by resource, uses buffered client channels and nonblocking publish, unsubscribes on handler exit, and has a UI refetch existing data when an SSE event arrives. | **Clearest near-match.** Key by user instead of resource, use capacity one, authenticate before stream headers, add heartbeat/deadline/`Close`, and handle write/flush errors. |
| [thoughtbot: *Writing a Server Sent Events server in Go*](https://thoughtbot.com/blog/writing-a-server-sent-events-server-in-go) and [working Gist](https://gist.github.com/ismasan/3fb75381cd2deb6bfa9c) | 2014; article updated 2019 | An authenticated-service use case led to a small `Broker` implementing `http.Handler`, with a channel per connection and central registration/removal/broadcast. | **Adopt the module seam.** Reject the sample's unbuffered, serial fan-out and cancellation loop; a slow or gone client can block it. |
| [Alex Pliutau/freeCodeCamp: *How to Implement Server-Sent Events in Go*](https://www.freecodecamp.org/news/how-to-implement-server-sent-events-in-go/) | 2024-08-28 | A current minimal `net/http` handler uses `http.NewResponseController`, `Flush`, `r.Context().Done()`, and vanilla `EventSource`. | **Adopt the transport baseline.** Add the app-specific hub, heartbeat, per-write deadline, and initial flush. |
| [OneUptime: *How to Build Real-time Applications with Go and SSE*](https://oneuptime.com/blog/post/2026-02-01-go-realtime-applications-sse/view) and [author-owned source](https://github.com/oneuptime/blog/blob/24d302bb59396d585dc0fa82650478a6a52c4d34/posts/2026-02-01-go-realtime-applications-sse/README.md#L161-L315) | 2026-02-01 | A broker tracks buffered client channels, skips a client when its buffer is full, unregisters on request cancellation, and writes heartbeat comments in the handler's single `select` loop. | **Closest tutorial pattern.** Reduce the buffer to one coalescing signal, key clients by user, add `Close`, and use `ResponseController`. |
| [PocketBase realtime documentation](https://pocketbase.io/docs/go-realtime/) and [current Go handler source](https://github.com/pocketbase/pocketbase/blob/bc8ffed4e7265a70a6e8de76c0b0b48b945e19ef/apis/realtime.go#L43-L163) | source inspected 2026-08-25 | PocketBase itself has a `SubscriptionsBroker`; one auth record may own several connected clients. Its handler uses `ResponseController`, registers/deferred-unregisters a client, sends an initial connect event, flushes, and exits on request cancellation or timers. | **Strong production validation of the hub shape.** Its topic-management POST, event payloads, IDs, and timers are more machinery than invalidation hints need. |
| [ntfy SSE handler](https://github.com/binwiederhier/ntfy/blob/f1bdb6bfe180fd2912ba9dbcd471b12d81428c84/server/server.go#L1386-L1508) and [topic fan-out](https://github.com/binwiederhier/ntfy/blob/f1bdb6bfe180fd2912ba9dbcd471b12d81428c84/server/topic.go#L49-L121) | source inspected 2026-08-25 | A production Go notification service protects the stream with its normal authorization middleware, registers topics with a user ID and cancellation function, unregisters with `defer`, selects on the request context, and sends keepalives. It isolates blocking subscribers so one does not hold up the rest. | **Adopt auth-before-subscribe, deferred cleanup, heartbeat, and slow-client isolation.** Our single handler writer plus capacity-one signal is simpler than ntfy's per-subscriber goroutines and response-writer lock. |

### What these examples actually prove

Pennybase is the closest code-level precedent found. It is an application, not
an SSE dependency, so its 826-star adoption and 2025-07-07 repository push are
context rather than a library recommendation
([GitHub metadata, checked 2026-08-25](https://api.github.com/repos/zserge/pennybase)).
Its broker is almost the proposed hub: a map from resource to subscriber set,
buffered channels, and nonblocking sends
([source](https://github.com/zserge/pennybase/blob/d65c07d3a4ecae1365ab1f169625907f08c29122/pennybase.go#L518-L551)).
Its authenticated SSE handler subscribes, defers unsubscribe, selects between
events and `r.Context().Done()`, writes, and flushes
([source](https://github.com/zserge/pennybase/blob/d65c07d3a4ecae1365ab1f169625907f08c29122/pennybase.go#L735-L764)).
Most importantly, its chat example reacts to SSE create events by refetching the
normal messages endpoint rather than treating the event as the canonical state
([template](https://github.com/zserge/pennybase/blob/d65c07d3a4ecae1365ab1f169625907f08c29122/examples/chat/templates/index.html#L5-L25)).
Mutations publish only after the store operation succeeds
([source](https://github.com/zserge/pennybase/blob/d65c07d3a4ecae1365ab1f169625907f08c29122/pennybase.go#L628-L689)).
Together, that is the same invalidation-then-reload approach proposed here.

Pennybase should not be copied verbatim: it has no hub shutdown or heartbeat,
uses a ten-event buffer where one coalescing signal is enough, and ignores
write/flush errors. It is strong evidence that the architecture is ordinary Go
application code; our contract makes the lifecycle and slow-client policy more
explicit.

The thoughtbot article is the closest historical precedent for a deliberately
small broker living beside an existing Go HTTP application. Its current Gist
still sends to every unbuffered client channel serially
([source](https://gist.github.com/ismasan/3fb75381cd2deb6bfa9c#file-sse-go-L112-L137)),
so it validates the architecture but also demonstrates the backpressure bug we
should avoid. It is a pattern to modernize, not code to copy.

Pliutau's 2024 example confirms that the modern transport loop is short with
the standard library: construct a `ResponseController`, write the event, flush,
and use the request context for disconnects. The extra code in this app is not
an alternative SSE implementation; it is the unavoidable policy for mapping an
authenticated user to all of their pages and friends.

OneUptime's current source is especially aligned with this app's semantics. It
uses buffered per-client channels and a nonblocking `default` case so a full
client is skipped
([broker source](https://github.com/oneuptime/blog/blob/24d302bb59396d585dc0fa82650478a6a52c4d34/posts/2026-02-01-go-realtime-applications-sse/README.md#L197-L227)).
It also explicitly keeps event and heartbeat writes in the same handler
goroutine because `http.ResponseWriter` is not safe for concurrent use
([handler source](https://github.com/oneuptime/blog/blob/24d302bb59396d585dc0fa82650478a6a52c4d34/posts/2026-02-01-go-realtime-applications-sse/README.md#L290-L315)).
Those are exactly the two most important concurrency decisions in the proposed
design. Since an invalidation carries no unique information, a buffer of one is
better than the example's buffer of ten.

PocketBase provides a useful reality check because it is the migration option
that prompted this research. Its official documentation explicitly says one
authenticated user can have multiple realtime clients across tabs, browsers,
and devices, and exposes those clients through a subscription broker
([documentation](https://pocketbase.io/docs/go-realtime/)). The source then
implements the familiar register, initial write/flush, message loop,
request-cancellation, and deferred-unregister lifecycle
([handler](https://github.com/pocketbase/pocketbase/blob/bc8ffed4e7265a70a6e8de76c0b0b48b945e19ef/apis/realtime.go#L76-L163)).
The proposed local hub is the small subset of that architecture this app needs.

ntfy shows the same patterns under heavier production requirements. It wraps
the SSE endpoint in its existing topic-read authorization
([route](https://github.com/binwiederhier/ntfy/blob/f1bdb6bfe180fd2912ba9dbcd471b12d81428c84/server/server.go#L680-L689)),
registers and unregisters subscriptions around the handler, and responds both
to external cancellation and `r.Context().Done()`
([handler](https://github.com/binwiederhier/ntfy/blob/f1bdb6bfe180fd2912ba9dbcd471b12d81428c84/server/server.go#L1410-L1508)).
Its topic publisher copies subscribers and invokes blocking subscribers
independently so a slow one cannot delay another
([fan-out](https://github.com/binwiederhier/ntfy/blob/f1bdb6bfe180fd2912ba9dbcd471b12d81428c84/server/topic.go#L106-L121)).
For a lossy invalidation signal, nonblocking capacity-one channels achieve the
same isolation without a goroutine per publish.

## Candidate analysis

### 1. Standard library: smallest and safest for these semantics

`net/http` already supplies all HTTP streaming primitives needed:

- Default HTTP/1.x and HTTP/2 response writers implement
  [`http.Flusher`](https://pkg.go.dev/net/http#Flusher).
- [`http.NewResponseController`](https://pkg.go.dev/net/http#NewResponseController)
  can traverse wrappers that implement `Unwrap() http.ResponseWriter`, and its
  [`Flush`](https://pkg.go.dev/net/http#ResponseController.Flush) and
  [`SetWriteDeadline`](https://pkg.go.dev/net/http#ResponseController.SetWriteDeadline)
  methods return errors.
- The request context is canceled when the client connection closes or the
  handler returns; a handler can select on `r.Context().Done()`.

A focused internal module would own:

- `map[userID]set[subscriber]`, protected by a mutex;
- a capacity-one channel per page connection;
- nonblocking publish (`select { case ch <- signal: default: }`) so repeated
  invalidations coalesce and a slow consumer cannot delay the mutation;
- subscribe/unsubscribe tied to the request context;
- a global close/cancel path for server shutdown; and
- the handler's 15-second comment heartbeat, five-second per-write deadline,
  event write, and flush.

The wire data can remain a constant such as:

```text
event: invalidate
data: {}

```

and a heartbeat is just `: keep-alive\n\n`. The HTML Standard explicitly
suggests a comment line about every 15 seconds to avoid legacy proxy timeouts
([authoring notes](https://html.spec.whatwg.org/multipage/server-sent-events.html#authoring-notes)).
There is no generic serialization problem because the message has no domain
payload.

This is not reimplementing a socket stack: Go owns HTTP, the browser owns SSE
parsing/reconnection, and the local code owns only this app's notification
policy. Put it behind a small API such as `Subscribe(userID)`,
`Notify(userIDs...)`, `ServeHTTP`, and `Close` so handler code stays as lean as
the rest of this project.

### 2. `tmaxmax/go-sse`: strongest established library, wrong default backpressure

Strengths:

- 526 stars, MIT, no external dependencies, Go 1.22 minimum, and a tagged
  v0.11.0 release. See its [module file](https://github.com/tmaxmax/go-sse/blob/v0.11.0/go.mod)
  and [v0.11.0 commit](https://github.com/tmaxmax/go-sse/commit/e3ddbdfdcf69aacfe83cbeafcc20e169f55fccb2).
- Implements `http.Handler`, supports authorization/topic selection through
  `OnSession`, has per-topic fan-out, optional replay, and an explicit
  [`Shutdown`](https://github.com/tmaxmax/go-sse/blob/v0.11.0/server.go#L187-L201).
- Its upgrader deliberately walks `Unwrap()` wrappers and accepts either
  `FlushError() error` or `http.Flusher`
  ([source](https://github.com/tmaxmax/go-sse/blob/v0.11.0/session.go#L114-L149)).
  After this app adds `statusRecorder.Unwrap`, it is compatible with the current
  logging middleware.

The disqualifying detail is documented by the library itself: Joe sends events
**synchronously**, and one blocking subscriber holds up the others
([Joe documentation](https://github.com/tmaxmax/go-sse/blob/v0.11.0/joe.go#L84-L99)).
Its loop calls `Send` and `Flush` inline for each matching subscriber
([source](https://github.com/tmaxmax/go-sse/blob/v0.11.0/joe.go#L223-L250)).
There is no built-in heartbeat or per-write deadline in the server/session path.
For tiny, infrequent events this will usually work because the OS socket buffer
accepts the write, but a dead or badly stalled connection is allowed to block a
publisher and every following subscriber. That is avoidable risk on mobile.

The provider interface is well designed, so a custom nonblocking Provider is
possible. But once the app writes that provider and adds write-deadline and
heartbeat policy, most of the application-specific hub still exists. The
dependency then buys robust event formatting and an extensibility surface more
than it buys a finished solution.

The project is also pre-v1 and v0.11.0 changed public callbacks and logging;
the project's own [changelog](https://github.com/tmaxmax/go-sse/blob/v0.11.0/CHANGELOG.md#0110---2025-05-14)
records the breaking changes. This is acceptable if pinned, but reinforces the
case for the stable standard-library surface.

### 3. `go.jetify.com/sse`: best transport helper, immature adoption

Jetify does **not** provide a broker. It provides the part that a local broker
would otherwise repeat in each connection handler:

- `Upgrade(context, ResponseWriter)` sets the SSE/cache/proxy headers, sends a
  retry field, flushes the response, starts a heartbeat, and closes on request
  cancellation
  ([source](https://github.com/jetify-com/sse/blob/v0.1.0/sse.go#L20-L93));
- it follows `Unwrap()` to discover a flusher and write-deadline support
  ([source](https://github.com/jetify-com/sse/blob/v0.1.0/sse.go#L96-L131));
- `Conn` serializes concurrent sends, sets a per-send write deadline, encodes,
  and flushes
  ([source](https://github.com/jetify-com/sse/blob/v0.1.0/sse.go#L183-L226)); and
- defaults are a 15-second heartbeat, three-second retry, and five-second write
  timeout
  ([source](https://github.com/jetify-com/sse/blob/v0.1.0/options.go#L58-L68)).

This matches the existing stack: it is framework-agnostic `net/http`, needs no
frontend package, preserves the single binary, and has no non-test runtime
dependencies ([module file](https://github.com/jetify-com/sse/blob/v0.1.0/go.mod)).
Systemd socket activation is transparent because it only sees the
`http.ResponseWriter` supplied by the existing server.

Trade-offs:

- only one tag, v0.1.0, and 119 stars;
- the newest default-branch commits in December 2025 are internal dependency
  updates rather than major application-code evolution
  ([history](https://github.com/jetify-com/sse/commits/main/)); and
- the app still must implement user subscriptions, friend fan-out,
  capacity-one coalescing, and coordinated shutdown.

If the team wants a reusable SSE transport dependency despite the maturity
trade-off, this is the preferred one. Pin v0.1.0 and test its wrapper,
heartbeat, disconnect, and shutdown behavior in this repository.

### 4. `r3labs/sse`: stars do not overcome stale internals

R3Labs has the largest star count in this comparison, but v2.10.0's default
branch ended in January 2023
([last commit](https://github.com/r3labs/sse/commit/c6d5381ee3ca63828b321c16baa008fd6c0b4564)).
Its handler performs a direct `w.(http.Flusher)` assertion
([source](https://github.com/r3labs/sse/blob/v2.10.0/http.go#L16-L22)), so it fails
behind this app's current `statusRecorder` wrapper.

Backpressure is also only partially controlled. `TryPublish` can drop when a
stream's input queue is full
([source](https://github.com/r3labs/sse/blob/v2.10.0/server.go#L125-L140)),
but the stream loop sends to each subscriber's capacity-64 channel with a
blocking send
([fan-out](https://github.com/r3labs/sse/blob/v2.10.0/stream.go#L69-L80),
[channel creation](https://github.com/r3labs/sse/blob/v2.10.0/stream.go#L104-L116)).
A full slow-subscriber channel therefore stops that stream's loop.

`AutoReplay` defaults to true, and the event log appends every content-bearing
event
([defaults](https://github.com/r3labs/sse/blob/v2.10.0/server.go#L35-L49),
[event log](https://github.com/r3labs/sse/blob/v2.10.0/event_log.go#L16-L23)).
That must be disabled for invalidations or the in-memory log grows for no
product benefit. There is no automatic heartbeat. It also makes the client
select a stream through a query parameter, requiring an authorization wrapper
to prevent subscription to another user's stream. This is more integration and
risk than either of the recommended paths.

### 5. Other libraries

- `antage/eventsource` has useful nonblocking per-consumer queues and write
  deadlines, but it type-asserts `http.Hijacker`, takes over the connection, and
  writes an HTTP/1.1 response manually
  ([source](https://github.com/antage/eventsource/blob/c4aae935d5bd7fe166d8d55339f2ed7f09d9d017/consumer.go#L42-L58)).
  That is incompatible with this wrapper today, sacrifices normal HTTP/2
  handling, and is unnecessary for SSE.
- `gin-contrib/sse` is not tied to Gin at the encoder layer and is actively
  maintained, but its API is only event encode/decode/render
  ([encoder source](https://github.com/gin-contrib/sse/blob/v1.1.1/sse-encoder.go)).
  It does not provide connection lifecycle, heartbeat, backpressure, fan-out,
  authorization, or shutdown, so it saves less than Jetify.
- `mroth/sseserver` advertises high throughput, but the project is AGPL-3.0,
  brings legacy runtime dependencies, exposes its own routes/admin surface, and
  its recent commits are CI dependency bumps rather than core evolution
  ([history](https://github.com/mroth/sseserver/commits/master/),
  [module file](https://github.com/mroth/sseserver/blob/v1.1.2/go.mod)).

## Required repository changes independent of library choice

These are compatibility requirements, not design changes. None affects the
HTML/CSS layout or visual behavior.

### Preserve streaming capabilities through middleware

[`statusRecorder`](../server.go#L123-L147) embeds `http.ResponseWriter` but does
not expose `http.Flusher` or `Unwrap`. A direct `w.(http.Flusher)` assertion
therefore fails even though Go's underlying writer supports streaming. Add:

```go
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
```

Then prefer `http.NewResponseController(w).Flush()` in local code. Both
tmaxmax and Jetify also follow the same standard `Unwrap` convention. Avoid a
hand-written `Flush()` that discards the underlying flush error when the
controller can preserve it.

### Explicitly end streams during shutdown

The current server calls `http.Server.Shutdown` with a 25-second bound.
[`Shutdown`](https://pkg.go.dev/net/http#Server.Shutdown) closes listeners and
idle connections, then waits for active handlers to become idle. An SSE handler
is active until its stream context is canceled, so shutdown must first close the
hub/cancel the stream contexts. Registering a notification with
[`Server.RegisterOnShutdown`](https://pkg.go.dev/net/http#Server.RegisterOnShutdown)
is one option; alternatively, close the hub from the same signal path just
before `Shutdown`. The close action must be nonblocking and handlers must select
on it.

tmaxmax explicitly tells callers to invoke its `Server.Shutdown` from
`RegisterOnShutdown`; Jetify closes on request context but still needs an
application-wide cancellation signal. A standard-library hub naturally owns
that signal.

### Bypass the service worker's API cache for `/api/events`

The current service worker routes every GET under `/api/` through
`networkFirst`, clones successful responses, and **awaits** `cache.put`
([source](../web/sw.js#L38-L79)). The Service Worker specification requires
`Cache.put` to read all bytes of the response body before completing
([algorithm](https://w3c.github.io/ServiceWorker/#cache-put)). An SSE response
does not finish during normal operation, so the worker would not return it to
`EventSource`; cloning an unbounded response is also an unbounded buffering
risk. MDN likewise notes that [`Cache.put` consumes the response
body](https://developer.mozilla.org/en-US/docs/Web/API/Cache/put) and that a
cloned body can buffer without limit when branches are consumed at different
rates ([`Response.clone`](https://developer.mozilla.org/en-US/docs/Web/API/Response/clone)).

Exclude `/api/events` before the generic `/api/` branch and let the browser make
a plain network request. Do not cache the stream.

### Keep the server timeout shape

The existing server has `ReadHeaderTimeout` and `IdleTimeout` but no global
`WriteTimeout`. That is suitable for SSE: `IdleTimeout` concerns waiting for the
next request, while a long global write deadline would put a fixed lifetime on
the stream. Use a short **per-write** deadline through `ResponseController` (or
Jetify's connection) instead, refreshed for each event/heartbeat.

Systemd socket activation and the one-binary deployment need no change. SSE is
an ordinary long-running HTTP GET on the inherited listener.

## Suggested implementation contract

Regardless of transport choice, keep the module narrow:

```text
Hub.Subscribe(userID) -> (<-chan Signal, unsubscribe)
Hub.Notify(userIDs...)
Hub.Close()
Handler(userID, ResponseWriter, Request)
```

### Compact implementation blueprint

Keep transport, lifecycle, and application fan-out separate:

```text
Hub
  mutex
  subscribers[userID] -> set of subscriber
  done                    // closed once by Close

subscriber
  signal channel, capacity 1

Subscribe(userID)
  reject if hub is closed
  add a distinct subscriber (one per tab/device)
  return its signal channel and an idempotent removal function

Notify(userIDs...)
  deduplicate user IDs
  for every matching subscriber:
    nonblocking send of an empty signal; full means already invalidated

Close()
  idempotently close hub.done; do not race by closing subscriber channels
  every handler selects hub.done, exits, and runs its removal function
```

The route first resolves the existing session and returns `401` if necessary;
only then does it call `Handler(userID, w, r)`. The handler subscribes before
its initial response so an invalidation during connection setup remains queued.
It sets `text/event-stream` and no-cache/no-store headers, creates an
`http.ResponseController`, writes and flushes an initial comment, and owns the
only goroutine that ever writes to that response. Its loop is:

```text
select
  signal:
    set a fresh short write deadline
    write "event: invalidate\ndata: {}\n\n"
    flush and exit on either error
  heartbeat ticker:
    set a fresh short write deadline
    write ": keep-alive\n\n"
    flush and exit on either error
  request context done:
    exit
  hub done:
    exit
```

The browser owns one same-origin `EventSource("/api/events")`. Both `open`
(including reconnect) and `invalidate` schedule the existing loader functions
through one short debounce; `visibilitychange` does the same when the page
becomes visible. The service worker bypasses this URL. Mutations commit first,
then call `Notify` for the owner and affected friends. Notification failure is
never a database failure.

This blueprint incorporates the good parts of Pennybase and OneUptime while
closing their lifecycle gaps. It is intentionally less general than
PocketBase's subscription protocol: no public topics, event history, IDs,
payload schema, or client-side subscription-management endpoint are needed.

Properties to test:

1. unauthenticated `/api/events` returns 401 before any stream headers;
2. initial headers and flush make `EventSource` reach `open` immediately;
3. two connections for one user both receive one invalidation;
4. unrelated users receive nothing;
5. a friend-affecting committed mutation notifies the owner and relevant
   friends;
6. a full subscriber channel coalesces/drops without blocking `Notify`;
7. heartbeat comments do not dispatch browser messages;
8. disconnect removes the subscriber;
9. hub/server shutdown ends every handler within the existing grace period;
10. the service worker bypasses `/api/events`; and
11. reconnect/foreground performs a full data refresh.

The mutation must commit successfully before calling `Notify`. Notification is
best-effort: do not roll back a committed rep because a browser disconnected.
Snapshot the target user IDs from the same post-commit state and keep network
writes completely outside the SQLite transaction.

## Final comparison

| Question | Standard library + local hub | Jetify + local hub | tmaxmax default server/Joe |
| --- | --- | --- | --- |
| Keeps existing `net/http`, auth, assets, systemd listener | Yes | Yes | Yes |
| Adds runtime dependency | No | One v0.1 module | One v0.11 module |
| User/friend authorization logic still local | Yes | Yes | Mostly via topics/`OnSession` |
| Nonblocking, capacity-one invalidation coalescing | Exactly controllable | Exactly controllable in local hub | No; synchronous socket writes |
| Wrapper-aware flush | `ResponseController` after `Unwrap` | Yes after `Unwrap` | Yes after `Unwrap` |
| Heartbeat and per-write deadline | About 20 local lines | Built in | Must be added/worked around |
| Replay | Deliberately none | Deliberately none | Optional, unnecessary |
| API maturity | Go compatibility promise | v0.1.0 | v0.11.0 |
| Recommendation | **First choice** | Good optional helper | Not for this backpressure policy |

The standard-library implementation is the least risky answer for this small
app. It does not require changing the database, API payloads, CSS, markup,
layout, or visual state. If avoiding even the small transport loop is worth a
pinned pre-v1 dependency, use Jetify for the connection and encoding layer while
keeping the local hub and its tests.

## Primary sources

- [Go `net/http` package](https://pkg.go.dev/net/http) — Flusher,
  ResponseController, request contexts, Server timeouts, and shutdown.
- [Go release history](https://go.dev/doc/devel/release) — current Go release.
- [WHATWG HTML Server-Sent Events](https://html.spec.whatwg.org/dev/server-sent-events.html)
  and [authoring notes](https://html.spec.whatwg.org/multipage/server-sent-events.html#authoring-notes)
  — protocol, reconnect, IDs, MIME type, and heartbeat comments.
- [MDN EventSource constructor](https://developer.mozilla.org/en-US/docs/Web/API/EventSource/EventSource)
  — browser API and credential option.
- [Service Worker Cache.put algorithm](https://w3c.github.io/ServiceWorker/#cache-put),
  [MDN Cache.put](https://developer.mozilla.org/en-US/docs/Web/API/Cache/put),
  and [MDN Response.clone](https://developer.mozilla.org/en-US/docs/Web/API/Response/clone)
  — why an endless stream must bypass the current cache strategy.
- [`zserge/pennybase` broker](https://github.com/zserge/pennybase/blob/d65c07d3a4ecae1365ab1f169625907f08c29122/pennybase.go#L512-L551),
  [handler](https://github.com/zserge/pennybase/blob/d65c07d3a4ecae1365ab1f169625907f08c29122/pennybase.go#L735-L769),
  and [chat example](https://github.com/zserge/pennybase/blob/d65c07d3a4ecae1365ab1f169625907f08c29122/examples/chat/templates/index.html#L5-L25)
  — closest small authenticated-app precedent for nonblocking fan-out and
  invalidation followed by a normal data reload.
- [thoughtbot's Go SSE broker article](https://thoughtbot.com/blog/writing-a-server-sent-events-server-in-go)
  and [source Gist](https://gist.github.com/ismasan/3fb75381cd2deb6bfa9c)
  — historical small-broker precedent and blocking-fan-out counterexample.
- [Alex Pliutau's 2024 standard-library example](https://www.freecodecamp.org/news/how-to-implement-server-sent-events-in-go/)
  — modern `ResponseController`, request-context, and `EventSource` baseline.
- [OneUptime's 2026 article](https://oneuptime.com/blog/post/2026-02-01-go-realtime-applications-sse/view)
  and [author-owned source](https://github.com/oneuptime/blog/blob/24d302bb59396d585dc0fa82650478a6a52c4d34/posts/2026-02-01-go-realtime-applications-sse/README.md#L161-L315)
  — nonblocking buffered fan-out and single-writer heartbeat pattern.
- [PocketBase Go realtime documentation](https://pocketbase.io/docs/go-realtime/)
  and [realtime handler](https://github.com/pocketbase/pocketbase/blob/bc8ffed4e7265a70a6e8de76c0b0b48b945e19ef/apis/realtime.go#L43-L163)
  — production subscription broker and multi-device connection lifecycle.
- [ntfy SSE handler](https://github.com/binwiederhier/ntfy/blob/f1bdb6bfe180fd2912ba9dbcd471b12d81428c84/server/server.go#L1386-L1508)
  and [topic fan-out](https://github.com/binwiederhier/ntfy/blob/f1bdb6bfe180fd2912ba9dbcd471b12d81428c84/server/topic.go#L49-L121)
  — production authorization, cleanup, keepalive, and slow-subscriber
  isolation patterns.
- [`tmaxmax/go-sse` source at v0.11.0](https://github.com/tmaxmax/go-sse/tree/v0.11.0)
  and [GitHub repository metadata](https://api.github.com/repos/tmaxmax/go-sse).
- [`jetify-com/sse` source at v0.1.0](https://github.com/jetify-com/sse/tree/v0.1.0)
  and [GitHub repository metadata](https://api.github.com/repos/jetify-com/sse).
- [`r3labs/sse` source at v2.10.0](https://github.com/r3labs/sse/tree/v2.10.0)
  and [GitHub repository metadata](https://api.github.com/repos/r3labs/sse).
- [`antage/eventsource` source](https://github.com/antage/eventsource/tree/c4aae935d5bd7fe166d8d55339f2ed7f09d9d017)
  and [GitHub repository metadata](https://api.github.com/repos/antage/eventsource).
- [`gin-contrib/sse` source at v1.1.1](https://github.com/gin-contrib/sse/tree/v1.1.1)
  and [GitHub repository metadata](https://api.github.com/repos/gin-contrib/sse).
- [`mroth/sseserver` source at v1.1.2](https://github.com/mroth/sseserver/tree/v1.1.2)
  and [GitHub repository metadata](https://api.github.com/repos/mroth/sseserver).
