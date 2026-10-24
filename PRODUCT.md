# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Single static Go binary (Go 1.27, `CGO_ENABLED=0`) serving an embedded frontend via `go:embed`; SQLite through `modernc.org/sqlite`. Frontend is hand-written HTML/CSS/vanilla JS with no build step and no runtime CDN, so every asset ships inside the binary. Deployed as a socket-activated systemd **user** service on port 5554.

*Inferred from the user's explicit brief; not from an interview.*

## Users

One self-coached lifter logging their own push-ups, plus the few people they compare themselves against. Primary situation is mid-session on a phone: standing or kneeling on the floor, breathing hard, sweaty hands, one thumb free, often in poor light. The job is "record the set I just finished in under three seconds and get back down," with a secondary job of "show me whether I have been consistent."

*Inferred from the brief (mobile-friendly, PWA, rep logging).*

## Product Purpose

Log push-up reps per day, per push-up variation, and make the resulting consistency visible at a glance. Success is a user who logs every set without friction and can see their streak and month at once. A second, smaller job: see how you stack up against the handful of people you actually train against.

## Positioning

Not a general fitness tracker. One movement, tracked honestly, with an interface that behaves like a tally counter rather than a workout app: the day defaults to today, the variation defaults to whatever was used last, and logging a set is a single thumb action.

## Operating Context

- Used between sets, so the primary control must be reachable and hittable without looking carefully.
- Runs on a home LAN / personal machine, reached from a phone browser and installable to the home screen.
- Self-hosted and single-user-ish; accounts exist to separate people, not to support an organization. Friends are people the user knows by username off-platform — there is no directory and no search, so adding someone means already knowing their name.

## Capabilities and Constraints

- Username + password accounts, optional confirmed recovery email and password reset links. Suggest adding an email to accounts without one; dismissing the suggestion is permanent for the account. Settings manages email confirmation and named passkeys, and passkeys support passwordless sign-in. *(Explicit user direction, 2026-10-23; supersedes the original no-email constraint.)*
- Multiple user-defined push-up types; each user gets a starter set (Standard, Wide, Diamond, Incline) they can rename or delete.
- Friends, added by username. A request must be accepted by its recipient; declining it locks the sender out of asking that person again for seven days. Removing a friend clears the history both ways, so either side can ask again immediately. *(Explicit user constraint.)*
- Board offers **Everyone**, a top ten across all app users for best day/month/year, this month/year, most distinct variations in a day, and average daily reps. Average includes rest days from the first logged day through today. Positive scores only; tied scores share ranks, usernames settle the ten-person cutoff. Records include their period and use server calendar dates. Global rankings require a connection; unsynced reps count after syncing. **My crew** retains the friend-only board for today, the current Monday–Sunday week, the current month and all time. *(Explicit user direction, 2026-10-24.)*
- Reps are logged as entries against (type, day). The day always defaults to today; past days can be chosen. Future days are rejected.
- Month calendar view showing per-day totals at a glance. Daily goals are dated: a saved target starts on the server's current day and applies until replaced; earlier dates retain their target and days before the first goal cannot be marked complete. Saving twice on a date replaces that date's target. Settings explains these consequences before saving. Goal history is account-scoped and cached offline; changes require a connection. *(Explicit user direction, 2026-10-24.)*
- Calendar checkmarks and explicit progress distinguish achieved goals from activity heat. Reaching a goal reveals a reflective quote with reduced-motion-aware animation. Quotes lean toward breath, kindness, presence and caring for the body as a way to care for the mind. Two API adapters fall back to 15 original bundled reflections; selection stays stable per date on a device and avoids adjacent-day repeats when history is available. This calmer completion voice is an explicit exception to the retained logging language. *(Explicit user direction, 2026-10-24.)*
- Phone interaction is a priority: goal controls are touch-sized, Settings fills the phone screen with a pinned close header and safe-area padding, and completing a goal brings its reflection into view. Installed-app offline/reopen/sync behavior remains part of the core experience. *(Explicit user emphasis, 2026-10-24.)*
- The server's date remains authoritative. Soft refresh at midnight, at least once a minute while visible, and on return keeps the calendar and leaderboard current. Returning after five minutes away selects today without clearing draft reps; live background updates preserve deliberate history browsing.
- Must be mobile-friendly and a progressive web app. After an online sign-in, log sets offline even after reloading, retain their original day, and sync without duplicates on reconnect or reopening. Browser-local caches and pending sets are isolated by account; variations, crew and deletion of synced entries still require a connection. *(Offline logging requested 2026-10-23.)*
- Service workers and PWA install require a secure context: full PWA behavior works on `localhost` and over HTTPS, and degrades to a plain web app over LAN HTTP.
- Database lives in systemd's `StateDirectory` (`~/.local/state/pushup-counter/pushups.db`).

## Brand Commitments

Name: **Push-Up Counter**. The default visual world is **ROOTED**, a plant-based community garden: sage working surfaces, forest actions, paper and lavender panels, a leaf mark, and Fraunces display type with Archivo text. The user requested a complete vegan/plant-based redesign and explicitly retained the tough-love language. This is a visual identity, not a dietary feature or a claim about nutrition, health, or performance. *(Explicit user direction, 2026-10-23.)*

Voice remains rough, brutal, David Goggins hard-mentality: imperative, unsentimental, no congratulation for the minimum. Keep direct commands and existing competitive language, including `LOG IT`, “No zero days.” and “Reps get logged or they did not happen.” The botanical presentation does not soften that voice. *(Explicit user constraint, reaffirmed 2026-10-23.)*

The original dark **FLOOR** appearance remains available through the **Classic theme** switch in Settings, both before and after sign-in. The choice belongs to the browser, persists locally when storage is available, and synchronizes across tabs. It is not an account preference or a server setting. The plant theme is the default; logging, account, variation, and crew behavior are shared by both themes.

## Evidence on Hand

None. There are no testimonials, user counts, benchmarks, or third-party claims, and none may be invented. Every number shown comes from reps somebody actually logged — the user's own, an accepted friend's, or an app user's aggregate leaderboard record.

## Product Principles

1. **Logging beats browsing.** The path from opening the app to a recorded set is the product; everything else is secondary.
2. **Defaults do the work.** Today, and the last-used variation, are already selected. Typing a number is the only required act.
3. **Show the truth, including the gaps.** Empty days are visible, not hidden or softened.
4. **One movement, done seriously.** No feature sprawl into other exercises or training plans. Social supports friend comparison and app-wide top-ten records: a roster and leaderboards, no feed, no likes, no comments. *(Amended 2026-08-24 at the user's explicit request; the original principle ruled out social features outright.)*
5. **Thumb-first.** Every primary control is reachable and large enough to hit while out of breath.

## Accessibility & Inclusion

Touch targets at least 48×48 CSS px, met from 390 px up. The one exception is arithmetic: a seven-column month grid cannot reach 48 px wide at a 320 px viewport without horizontal scroll, so calendar cells land at 39×48 there. Pinch zoom preserved. Calendar intensity never the only channel — every day carries its numeric total and a full accessible name. Leaderboard bar length is likewise never the only channel: every row shows its number, and each row has a full accessible name naming its rank. Both tab sets follow the ARIA tabs pattern with a roving tabindex, and an incoming friend request is announced by a labelled region rather than by colour alone.
