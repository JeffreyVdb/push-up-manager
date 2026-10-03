# Frontend/PWA recommendation for the push-up counter

**Decision date:** 2026-08-24  
**Constraints:** mobile-first; offline-capable; no runtime CDN; static assets embedded in one Go binary with `go:embed`; preferably no frontend build step.

## Executive recommendation

Build the UI with semantic HTML, vanilla JavaScript, and a small hand-written modern CSS layer. Embed every local asset—HTML shell, JS, CSS, manifest, service worker, icons, and any optional library—into the Go binary. Use a service worker for the app shell and a network-first strategy for safe, cacheable API `GET`s; keep mutations in the application’s offline outbox rather than trying to cache `POST` requests.

The product has four small interaction surfaces—login, push-up-type selection, rep entry, and a month heatmap. That is enough state to justify disciplined modules, but not enough rendering complexity to justify a framework runtime. Direct DOM updates keep startup, debugging, offline updates, and the embedded asset graph straightforward. The application-shell model is explicitly intended for a small static UI whose page-specific content arrives from APIs. [Chrome’s application-shell guidance](https://developer.chrome.com/docs/workbox/app-shell-model) supports this split.

If the UI grows into many independently updated screens, choose **Preact + htm** as the escape hatch: it offers a component model while staying small and allowing tagged-template markup without JSX. Vendor the exact files locally; do not load them from a CDN. For the current feature set, the extra runtime and rendering abstraction are unnecessary.

## 1. Frontend approach

### Recommended structure

Use a small set of ordinary modules with explicit state boundaries:

| Module | Responsibility |
| --- | --- |
| `state` | Current user, selected push-up type, month, pending sync items |
| `api` | `fetch` calls, auth headers/cookies, response validation, online/offline status |
| `views` | Render login, type list, rep-entry screen, and calendar into stable containers |
| `calendar` | Month math, heat levels, accessible labels, previous/next month controls |
| `outbox` | Persist unsent rep records locally; retry only idempotent or safely deduplicated mutations |
| `sw-registration` | Register the service worker and expose an explicit “update available” action |

Prefer stable semantic containers and event delegation over replacing the entire document. Re-render the smallest region whose state changed. Use `<form>` for login and rep submission so keyboard, VoiceOver, and browser submission behavior remain useful even if JavaScript is delayed.

### Options compared

| Approach | Strengths | Costs and fit for this app |
| --- | --- | --- |
| **Vanilla JS + CSS** | Zero runtime dependency; no compiler; easiest `go:embed` story; direct control over input focus, keyboard behavior, offline state, and accessible table markup | Requires a little discipline around state and rendering; that is a reasonable cost for four screens |
| **Preact + htm** | Preact provides a small virtual-DOM component model and can run in the browser without transpilation; htm supplies tagged-template syntax. Preact describes itself as a thin DOM abstraction and publishes its small-runtime goal in its official guide. [Preact guide](https://preactjs.com/guide/v10/getting-started) · [Preact repository](https://github.com/preactjs/preact) · [htm repository](https://github.com/developit/htm) | Adds two vendored runtimes, a render/update lifecycle, and more moving parts. Good second choice if screens and shared components multiply; overkill for the current counter |
| **Alpine.js** | Declarative behavior directly in HTML; useful for small toggles and menus. [Alpine home](https://alpinejs.dev/) · [Alpine installation](https://alpinejs.dev/essentials/installation) | Template expressions and framework lifecycle are unnecessary for the current amount of state. It is still easy to vendor, but its value is mostly in avoiding hand-written event wiring on larger markup-heavy pages |
| **petite-vue** | About 6 KB, DOM-based, progressive-enhancement-oriented, and usable without a build setup. [petite-vue README](https://github.com/vuejs/petite-vue) | Its own README describes the project as relatively new and subject to API changes; expressions use `new Function()`, which conflicts with a strict CSP. The counter does not need Vue-compatible template expressions |
| **htmx** | A single dependency-free browser file and no build system; excellent when the server returns HTML fragments. [htmx documentation](https://htmx.org/docs) | A push-up counter needs client-local state, immediate rep entry, an offline outbox, and a calendar rendered from local data. Server-driven fragment exchange is a poor center of gravity for offline-first behavior, although htmx could still be useful for a mostly online admin screen |

The library choice will not by itself create a good mobile experience. The decisive details are a focused rep-entry flow, a large touch surface, predictable focus, explicit offline feedback, and a calendar whose colors are supplemented by text and accessible names.

### Why not standard Vue/React with a build?

They are valid products, but they work against the “single static binary with little tooling” constraint. A build adds a generated asset graph and a release step that must stay synchronized with the Go embed set. If the app later needs that complexity, Preact + htm is a smaller migration seam than bringing in a full framework first.

## 2. Drop-in CSS choices

### Current versions and vendorable files

Versions below were checked against the npm registry on 2026-08-24. The byte counts are local measurements of the exact minified package file: raw bytes, then bytes after the default `gzip` command. They are useful for comparison, not a substitute for the Go binary’s final compressed size.

| Library | Current version | Exact file to vendor | Raw | gzip | What it is |
| --- | ---: | --- | ---: | ---: | --- |
| Pico.css | **2.1.1** | `css/pico.min.css` | 83,319 B | 11,709 B | Semantic, class-light baseline with form/button/layout styles |
| Water.css | **2.1.1** | `out/water.min.css` | 22,668 B | 3,586 B | Classless styling for simple documents; not a component system |
| Simple.css | **2.3.7** | `simple.min.css` | 9,429 B | 2,790 B | Very small classless stylesheet; useful baseline, little app-specific UI |
| Open Props | **1.7.23** | `open-props.min.css` | 29,566 B | 7,681 B | Design tokens/custom properties and utility primitives, not a drop-in component library |
| Tailwind CSS | **4.3.3** | **No drop-in runtime file**; vendor the project’s generated `app.css` | — | — | Utility generator; the useful production CSS is generated from the classes used by the app |

The version links are the package records for [Pico 2.1.1](https://www.npmjs.com/package/@picocss/pico/v/2.1.1), [Water.css 2.1.1](https://www.npmjs.com/package/water.css/v/2.1.1), [Simple.css 2.3.7](https://www.npmjs.com/package/simpledotcss/v/2.3.7), [Open Props 1.7.23](https://www.npmjs.com/package/open-props/v/1.7.23), and [Tailwind CSS 4.3.3](https://www.npmjs.com/package/tailwindcss/v/4.3.3). The official repositories and sites describe the intended roles: [Pico](https://picocss.com/), [Water.css](https://watercss.kognise.dev/), [Simple.css](https://simplecss.org/), and [Open Props](https://open-props.style/).

### Recommendation

Choose **Pico.css 2.1.1**, vendoring `css/pico.min.css`, plus a small app stylesheet for the counter and calendar. It is the best drop-in baseline here because it already styles semantic forms, buttons, tables, responsive layout, and light/dark color schemes; its official site documents semantic HTML, responsive behavior, and `prefers-color-scheme` support. [Pico features](https://picocss.com/)

Use Water.css or Simple.css only if the goal is an extremely plain document and the team is happy to write nearly all app controls themselves. Use Open Props only if the team specifically wants a token layer; it is not a substitute for component styling. Do not choose Tailwind for this constraint: the official production path generates CSS, while the Play CDN is a development convenience rather than a runtime dependency to ship in an offline app. [Tailwind installation](https://tailwindcss.com/docs/installation) · [Tailwind Play CDN](https://tailwindcss.com/docs/installation/play-cdn)

## 3. PWA manifest and installability

Installability and offline capability are separate:

* A service worker is essential for this app’s offline shell and sync behavior, but it is no longer a universal Chrome menu-install requirement. Chrome removed the service-worker `fetch`-handler requirement for menu installation in mobile Chrome 108 and desktop Chrome 112. [Chrome’s install-criteria update](https://developer.chrome.com/blog/update-install-criteria) · [MDN installability guide](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Guides/Making_PWAs_installable)
* Chrome’s install promotion also has browser/user conditions such as HTTPS, not already being installed, and engagement heuristics. These are separate from the manifest JSON. [Chrome install criteria](https://web.dev/articles/install-criteria)

### Android Chrome minimum manifest subset

For the Chrome install-promotion criteria documented currently, provide:

* `name` **or** `short_name`.
* `icons` containing valid PNG icons labeled `192x192` and `512x512`.
* `start_url`.
* `display`, or an accepted `display_override` path; use `display: "standalone"` here.
* No `prefer_related_applications`, or set it to `false`.
* Serve the app over HTTPS in production; `localhost` and loopback are allowed for development.

The canonical current Chrome list is [web.dev’s install criteria](https://web.dev/articles/install-criteria); MDN gives the same Chromium requirements and HTTPS/localhost condition [in its installability guide](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Guides/Making_PWAs_installable). A `512x512` `purpose: "maskable"` icon is strongly recommended for Android adaptive surfaces, but it is an additional icon, not a replacement for the ordinary `192x192` and `512x512` entries. [web.dev manifest guidance](https://web.dev/learn/pwa/web-app-manifest)

### iOS Safari and iOS/iPadOS Home Screen behavior

iOS does not have the same Chrome install-promotion contract. Modern iOS can add a website from the Share menu, and WebKit’s Safari 26 announcement says Home Screen websites now open as web apps by default unless the user chooses the browser-bookmark behavior. Therefore, there is no iOS-specific JSON field that should be called “required for installability” in the Chrome sense. [WebKit, News from WWDC25](https://webkit.org/blog/16993/news-from-wwdc25-web-technology-coming-this-fall-in-safari-26-beta) · [MDN manifest overview](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Manifest)

Still ship a manifest with the cross-platform fields above. Safari has supported manifest-declared icons for years, but an HTML `apple-touch-icon` takes precedence when present. [WebKit, Safari 15.4](https://webkit.org/blog/12445/new-webkit-features-in-safari-15-4/)

For compatibility and predictable iOS branding, include:

```html
<link rel="apple-touch-icon" sizes="180x180" href="/icons/apple-touch-icon-180.png">
```

The icon is not a current iOS installation prerequisite. It is the compatibility path and explicit precedence path. Apple’s reference lists `180x180` for iPhone Retina and `167x167`/`152x152` for iPad variants; one 180px square PNG is a practical minimum, with the other sizes optional if iPad-specific raster quality matters. [Apple’s Web Application configuration guide](https://developer.apple.com/library/archive/documentation/AppleApplications/Reference/SafariWebContent/ConfiguringWebApplications/ConfiguringWebApplications.html)

The old iOS-only tags are optional compatibility controls, not current universal install requirements:

```html
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="default">
<meta name="apple-mobile-web-app-title" content="Push-Ups">
```

`apple-mobile-web-app-capable` requests the legacy standalone presentation; `apple-mobile-web-app-status-bar-style` only has an effect with standalone mode; and `apple-mobile-web-app-title` overrides the launch-icon title. Apple documents all three, including their limitations. [Apple supported meta tags](https://developer.apple.com/library/archive/documentation/AppleApplications/Reference/SafariHTMLRef/Articles/MetaTags.html) · [Apple Web Application configuration](https://developer.apple.com/library/archive/documentation/AppleApplications/Reference/SafariWebContent/ConfiguringWebApplications/ConfiguringWebApplications.html)

For a 2026-targeted app, keep them if older iOS support or explicit legacy standalone behavior is a requirement. If the target is only current Safari, they are not needed to make the app installable; avoid proliferating legacy tags merely because older PWA checklists required them. Current web.dev guidance explicitly warns that old proprietary tags should not be used as a substitute for a manifest. [web.dev manifest guidance](https://web.dev/learn/pwa/web-app-manifest)

An `apple-touch-startup-image` is likewise optional legacy polish, not an installability requirement. Do not add a matrix of startup images unless a specific older-iOS launch-screen requirement justifies the maintenance cost; Apple documents the link as optional. [Apple Web Application configuration](https://developer.apple.com/library/archive/documentation/AppleApplications/Reference/SafariWebContent/ConfiguringWebApplications/ConfiguringWebApplications.html)

### Minimal manifest to ship

This is intentionally a little more complete than the strict Chrome minimum because stable `id`, `scope`, colors, and a maskable icon make the installed identity and launch behavior predictable:

```json
{
  "id": "/",
  "name": "Push-Up Counter",
  "short_name": "Push-Ups",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "theme_color": "#1769aa",
  "background_color": "#f7f9fc",
  "icons": [
    {
      "src": "/icons/icon-192.png",
      "type": "image/png",
      "sizes": "192x192"
    },
    {
      "src": "/icons/icon-512.png",
      "type": "image/png",
      "sizes": "512x512"
    },
    {
      "src": "/icons/icon-512-maskable.png",
      "type": "image/png",
      "sizes": "512x512",
      "purpose": "maskable"
    }
  ]
}
```

Link it from the shell as `<link rel="manifest" href="/manifest.webmanifest">`. `theme_color` and `background_color` improve browser/OS presentation but are not in the strict minimum list; use opaque colors, not transparency or CSS variables. The manifest `theme_color` can be overridden by an HTML `theme-color` meta element. [web.dev manifest fields](https://web.dev/learn/pwa/web-app-manifest)

Use `display: "standalone"` as the cross-platform default. The manifest specification defines `fullscreen`, `standalone`, `minimal-ui`, and `browser`; Chromium additionally recognizes `window-controls-overlay` for suitable desktop windows. `standalone` is the least surprising mobile app-like mode and works with the iOS compatibility path. [W3C Web Application Manifest](https://www.w3.org/TR/appmanifest) · [web.dev install criteria](https://web.dev/articles/install-criteria)

For browser chrome and Android status-bar theming, add an HTML fallback/override:

```html
<meta name="theme-color" content="#1769aa">
<meta name="theme-color" content="#0f1720" media="(prefers-color-scheme: dark)">
<meta name="color-scheme" content="light dark">
```

Do not depend on `theme-color` for iOS status-bar control; the Apple status-bar meta tag is a separate, optional compatibility mechanism.

## 4. Service worker for app shell + API

### Strategy

1. **Precache the shell at install:** `/`, the HTML shell, the app CSS, the app JS, the manifest, and local icons. The shell should be small and contain the stable layout; page data arrives after boot. [Chrome app-shell model](https://developer.chrome.com/docs/workbox/app-shell-model)
2. **Shell/static assets:** cache-first, because the service worker owns versioned shell cache names and replaces them during activation.
3. **API `GET`:** network-first, with a cached response fallback. Save only successful same-origin `GET` responses, and consider omitting API caching entirely for sensitive per-user responses.
4. **API mutations:** network-only from the service worker. Do not intercept or cache `POST`, `PUT`, `PATCH`, or `DELETE`. If offline rep logging is required, the page writes a durable outbox record and later sends a deduplicated mutation; Cache Storage is not a mutation queue.
5. **Updates:** install the new worker into a new versioned cache, let it wait by default, ask the user to refresh, then send `SKIP_WAITING`. This prevents an already-open page from being controlled by a worker whose shell/API assumptions do not match the old page. `skipWaiting()` is explicitly an opt-in lifecycle shortcut. [web.dev service-worker lifecycle](https://web.dev/articles/service-worker-lifecycle)

### Minimal vanilla service worker

This pattern deliberately has no CDN dependency and has a fixed `/sw.js` URL. Replace the asset list and version during a release. In a generated build, a tiny release script can keep this list in sync; a hand-maintained list is acceptable for a small no-build app.

```js
const VERSION = "shell-v1";
const SHELL = VERSION;
const RUNTIME = `${VERSION}-runtime`;
const API = `${VERSION}-api`;

const PRECACHE = [
  "/",
  "/index.html",
  "/app.css",
  "/app.js",
  "/manifest.webmanifest",
  "/icons/icon-192.png",
  "/icons/icon-512.png",
  "/icons/icon-512-maskable.png"
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(SHELL).then((cache) => cache.addAll(PRECACHE))
  );
  // Do not call skipWaiting() here. Activate after the user accepts an update.
});

self.addEventListener("activate", (event) => {
  event.waitUntil((async () => {
    const keep = new Set([SHELL, RUNTIME, API]);
    const keys = await caches.keys();
    await Promise.all(
      keys.filter((key) => !keep.has(key)).map((key) => caches.delete(key))
    );
    await self.clients.claim();
  })());
});

self.addEventListener("message", (event) => {
  if (event.data?.type === "SKIP_WAITING") self.skipWaiting();
});

self.addEventListener("fetch", (event) => {
  const request = event.request;
  // This guard prevents accidental caching of POST/PUT/PATCH/DELETE.
  if (request.method !== "GET") return;

  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;

  if (url.pathname.startsWith("/api/")) {
    event.respondWith(networkFirst(request, API));
    return;
  }

  if (request.mode === "navigate") {
    event.respondWith(cacheFirst(new Request("/index.html"), SHELL));
    return;
  }

  event.respondWith(cacheFirst(request, RUNTIME));
});

async function cacheFirst(request, cacheName) {
  const cached = await caches.match(request);
  if (cached) return cached;

  const response = await fetch(request);
  if (response.ok) {
    const cache = await caches.open(cacheName);
    await cache.put(request, response.clone());
  }
  return response;
}

async function networkFirst(request, cacheName) {
  try {
    const response = await fetch(request);
    if (response.ok) {
      const cache = await caches.open(cacheName);
      await cache.put(request, response.clone());
    }
    return response;
  } catch {
    const cached = await caches.match(request);
    return cached || new Response(
      JSON.stringify({ offline: true }),
      { status: 503, headers: { "Content-Type": "application/json" } }
    );
  }
}
```

The `fetch` event is the right interception point for request-aware routing; the service-worker fetch model also supports waiting for background cache updates with `event.waitUntil()`. [MDN `FetchEvent`](https://developer.mozilla.org/en-US/docs/Web/API/FetchEvent) Workbox documents the same network-first and cache-first trade-offs. [Workbox runtime caching](https://developer.chrome.com/docs/workbox/caching-resources-during-runtime)

In the page, register `/sw.js` after load. When `registration.waiting` exists, show an update button; on confirmation, post `{ type: "SKIP_WAITING" }` to the waiting worker and reload once on `controllerchange`. Keep `/sw.js` at the same URL across releases—change its contents and cache names, rather than changing the script URL—because an old cached shell can otherwise keep the browser from discovering the new worker. [web.dev service-worker lifecycle](https://web.dev/articles/service-worker-lifecycle)

## 5. Mobile UX requirements

### Viewport and safe areas

Use this viewport declaration:

```html
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
```

`viewport-fit=cover` lets a full-bleed app use the whole display; pair it with safe-area padding so controls are not hidden by a notch, rounded corner, or home indicator. [MDN viewport reference](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/meta/name/viewport) · [MDN `env()`](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Values/env)

```css
:root {
  --safe-top: env(safe-area-inset-top, 0px);
  --safe-right: env(safe-area-inset-right, 0px);
  --safe-bottom: env(safe-area-inset-bottom, 0px);
  --safe-left: env(safe-area-inset-left, 0px);
}

body {
  margin: 0;
  padding: var(--safe-top) var(--safe-right) var(--safe-bottom) var(--safe-left);
}

.bottom-actions {
  padding-bottom: calc(1rem + var(--safe-bottom));
}
```

Do not globally set `user-scalable=no` or `maximum-scale=1`; preserve pinch zoom for accessibility. Use `100dvh` for a full-height app region when it is appropriate, because mobile browser chrome and the virtual keyboard make a fixed `100vh` layout unreliable. [MDN viewport reference](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/meta/name/viewport)

### Rep entry

For whole-number reps, prefer a text field with a numeric keyboard hint and validate the value in JavaScript/server-side:

```html
<label for="reps">Reps</label>
<input
  id="reps"
  name="reps"
  type="text"
  inputmode="numeric"
  pattern="[0-9]*"
  maxlength="4"
  enterkeyhint="done"
  autocomplete="off"
  aria-describedby="reps-help">
<small id="reps-help">Whole numbers only.</small>
```

`inputmode="numeric"` is a keyboard hint, not validation. `type="number"` is reasonable when native number semantics and `min`/`max` are wanted, but it can expose steppers and browser-specific number parsing; a text field plus numeric input mode gives the counter tighter control over whole-number entry. [MDN `inputMode`](https://developer.mozilla.org/en-US/docs/Web/API/HTMLElement/inputMode) · [MDN number input](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/input/number)

Make the number itself dominant: keep the focused input visible above the keyboard, offer large increment/decrement or preset buttons, and submit with the keyboard’s Done action. Announce validation errors in an adjacent `aria-live="polite"` region; never make the user infer an error from color alone.

### Touch, zoom, and sizing

Use `touch-action: manipulation` on counter buttons and other controls where supported to signal that the control does not need double-tap gesture handling. Do not apply it indiscriminately to the whole page, and do not remove pinch zoom with viewport flags. [MDN touch-action](https://developer.mozilla.org/en-US/docs/Web/CSS/touch-action)

Use at least **48×48 CSS px** for primary counter buttons and icon buttons, with visible spacing between adjacent controls. WCAG 2.2’s AA minimum is 24×24 CSS px with exceptions; 48px is the more comfortable mobile implementation target. [W3C WCAG 2.2 Target Size](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html)

```css
button,
input,
select,
.counter-key {
  min-block-size: 48px;
}

button,
.counter-key {
  min-inline-size: 48px;
  touch-action: manipulation;
}
```

### Dark mode

Use system preference without JavaScript, and keep text/heatmap contrast accessible in both modes:

```css
:root {
  color-scheme: light dark;
  --surface: #f7f9fc;
  --text: #17202a;
}

@media (prefers-color-scheme: dark) {
  :root {
    --surface: #101820;
    --text: #f4f7fb;
  }
}

body {
  background: var(--surface);
  color: var(--text);
}
```

`prefers-color-scheme` is the right CSS-level signal; Pico also follows it out of the box. [MDN `prefers-color-scheme`](https://developer.mozilla.org/en-US/docs/Web/CSS/@media/prefers-color-scheme) · [Pico color schemes](https://picocss.com/)

## 6. Accessible month heatmap

### Hand-roll it

Hand-roll the month grid. Month arithmetic, seven columns, four to six rows, and a discrete heat-level mapping are small enough to own. A general calendar widget would bring a larger interaction model than this read-only history view needs, and a date library is unnecessary unless the backend already has difficult timezone/calendar rules.

### Markup pattern

For a read-only month, use a native `<table>`, not an ARIA grid. Native table semantics let assistive technology understand rows and columns without requiring the keyboard behavior of a spreadsheet. If each day opens an edit/details view, put a real `<button>` inside each non-empty `<td>`.

```html
<section aria-labelledby="month-heading">
  <h2 id="month-heading">August 2026</h2>

  <table class="heatmap" aria-describedby="heatmap-legend">
    <caption>Push-ups per day for August 2026</caption>
    <thead>
      <tr>
        <th scope="col">Mon</th>
        <th scope="col">Tue</th>
        <th scope="col">Wed</th>
        <th scope="col">Thu</th>
        <th scope="col">Fri</th>
        <th scope="col">Sat</th>
        <th scope="col">Sun</th>
      </tr>
    </thead>
    <tbody>
      <tr>
        <td aria-hidden="true"></td>
        <td aria-hidden="true"></td>
        <td aria-hidden="true"></td>
        <td aria-hidden="true"></td>
        <td data-level="3">
          <button type="button" aria-label="Saturday, August 1, 2026: 42 push-ups">
            <time datetime="2026-08-01">1</time>
            <span aria-hidden="true">42</span>
          </button>
        </td>
        <td data-level="0">
          <button type="button" aria-label="Sunday, August 2, 2026: 0 push-ups">
            <time datetime="2026-08-02">2</time>
            <span aria-hidden="true">0</span>
          </button>
        </td>
      </tr>
    </tbody>
  </table>

  <p id="heatmap-legend">
    <span aria-hidden="true">Less</span>
    <span class="swatch level-0"></span>
    <span class="swatch level-1"></span>
    <span class="swatch level-2"></span>
    <span class="swatch level-3"></span>
    <span class="swatch level-4"></span>
    <span>More. Every day also announces its numeric total.</span>
  </p>
</section>
```

Implementation rules:

* Put the full localized date and total in the button’s accessible name, for example `Monday, August 24, 2026: 0 push-ups`; do not make the heat color the only data channel.
* Keep the numeric total visibly available at a comfortable size, or include it in a visually-hidden text span if the chosen compact design cannot show it.
* Use `aria-current="date"` on today when it is in the visible month. Use `aria-pressed` on the day button only if the app has a selected day. Do not add `aria-selected` to ordinary table cells; it is for selectable widgets such as a grid/listbox pattern.
* Empty leading/trailing cells should be non-focusable and can be `aria-hidden="true"`; do not render fake day buttons for dates outside the month.
* Provide previous/next month buttons with accessible names and preserve focus sensibly after changing the month.
* Use discrete classes or `data-level="0"` through `data-level="4"` for color intensity. Define separate light/dark palettes and ensure the day label/total remains readable.

If the calendar becomes a true keyboard-navigable date picker, follow the WAI-ARIA date-picker/grid pattern rather than improvising roles: a grid has rows and cells, selection state, and defined arrow/home/end keyboard behavior. [WAI-ARIA date-picker example](https://www.w3.org/TR/2019/WD-wai-aria-practices-1.2-20191218/examples/dialog-modal/datepicker-dialog.html) · [MDN ARIA grid role](https://developer.mozilla.org/en-US/docs/Web/Accessibility/ARIA/Reference/Roles/grid_role)

## Sources

### Frontend and CSS

* [Preact getting started](https://preactjs.com/guide/v10/getting-started)
* [Preact repository](https://github.com/preactjs/preact)
* [htm repository](https://github.com/developit/htm)
* [Alpine.js](https://alpinejs.dev/)
* [Alpine.js installation](https://alpinejs.dev/essentials/installation)
* [petite-vue repository and README](https://github.com/vuejs/petite-vue)
* [htmx documentation](https://htmx.org/docs)
* [Pico CSS](https://picocss.com/)
* [Water.css](https://watercss.kognise.dev/)
* [Simple.css](https://simplecss.org/)
* [Open Props](https://open-props.style/)
* [Tailwind installation](https://tailwindcss.com/docs/installation)
* [Tailwind Play CDN](https://tailwindcss.com/docs/installation/play-cdn)
* [npm: Pico.css 2.1.1](https://www.npmjs.com/package/@picocss/pico/v/2.1.1)
* [npm: Water.css 2.1.1](https://www.npmjs.com/package/water.css/v/2.1.1)
* [npm: Simple.css 2.3.7](https://www.npmjs.com/package/simpledotcss/v/2.3.7)
* [npm: Open Props 1.7.23](https://www.npmjs.com/package/open-props/v/1.7.23)
* [npm: Tailwind CSS 4.3.3](https://www.npmjs.com/package/tailwindcss/v/4.3.3)

### PWA and service workers

* [MDN manifest overview](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Manifest)
* [MDN making PWAs installable](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Guides/Making_PWAs_installable)
* [web.dev install criteria](https://web.dev/articles/install-criteria)
* [web.dev web app manifest](https://web.dev/learn/pwa/web-app-manifest)
* [Chrome: Revisiting installability criteria](https://developer.chrome.com/blog/update-install-criteria)
* [W3C Web Application Manifest](https://www.w3.org/TR/appmanifest)
* [Apple: Configuring Web Applications](https://developer.apple.com/library/archive/documentation/AppleApplications/Reference/SafariWebContent/ConfiguringWebApplications/ConfiguringWebApplications.html)
* [Apple: Supported Meta Tags](https://developer.apple.com/library/archive/documentation/AppleApplications/Reference/SafariHTMLRef/Articles/MetaTags.html)
* [WebKit: Safari 15.4 features](https://webkit.org/blog/12445/new-webkit-features-in-safari-15-4/)
* [WebKit: News from WWDC25 / Safari 26 beta](https://webkit.org/blog/16993/news-from-wwdc25-web-technology-coming-this-fall-in-safari-26-beta)
* [Chrome Workbox application-shell model](https://developer.chrome.com/docs/workbox/app-shell-model)
* [web.dev service-worker lifecycle](https://web.dev/articles/service-worker-lifecycle)
* [MDN `FetchEvent`](https://developer.mozilla.org/en-US/docs/Web/API/FetchEvent)
* [Chrome Workbox runtime caching](https://developer.chrome.com/docs/workbox/caching-resources-during-runtime)

### Mobile and accessibility

* [MDN viewport meta reference](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/meta/name/viewport)
* [MDN `env()`](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Values/env)
* [MDN `inputMode`](https://developer.mozilla.org/en-US/docs/Web/API/HTMLElement/inputMode)
* [MDN number input](https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/input/number)
* [MDN `touch-action`](https://developer.mozilla.org/en-US/docs/Web/CSS/touch-action)
* [MDN `prefers-color-scheme`](https://developer.mozilla.org/en-US/docs/Web/CSS/@media/prefers-color-scheme)
* [W3C WCAG 2.2 Target Size](https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html)
* [WAI-ARIA date-picker example](https://www.w3.org/TR/2019/WD-wai-aria-practices-1.2-20191218/examples/dialog-modal/datepicker-dialog.html)
* [MDN ARIA grid role](https://developer.mozilla.org/en-US/docs/Web/Accessibility/ARIA/Reference/Roles/grid_role)
