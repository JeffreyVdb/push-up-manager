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

- Username + password accounts. No email, no email verification, no password reset flow. *(Explicit user constraint.)*
- Multiple user-defined push-up types; each user gets a starter set (Standard, Wide, Diamond, Incline) they can rename or delete.
- Friends, added by username. A request must be accepted by its recipient; declining it locks the sender out of asking that person again for seven days. Removing a friend clears the history both ways, so either side can ask again immediately. *(Explicit user constraint.)*
- One shared leaderboard over the signed-in user plus their accepted friends, for today, the current Monday–Sunday week, the current month, and all time. It never counts anybody outside that circle.
- Reps are logged as entries against (type, day). The day always defaults to today; past days can be chosen. Future days are rejected.
- Month calendar view showing per-day totals at a glance.
- The server's date remains authoritative. Soft refresh at midnight, at least once a minute while visible, and on return keeps the calendar and leaderboard current. Returning after five minutes away selects today without clearing draft reps; live background updates preserve deliberate history browsing.
- Must be mobile-friendly and a progressive web app (installable, offline app shell).
- Service workers and PWA install require a secure context: full PWA behavior works on `localhost` and over HTTPS, and degrades to a plain web app over LAN HTTP.
- Database lives in systemd's `StateDirectory` (`~/.local/state/pushup-counter/pushups.db`).

## Brand Commitments

Name: **Push-Up Counter**. Voice pinned by the user: rough, brutal, David Goggins hard-mentality. Imperative, unsentimental, no congratulation for the minimum. *(Explicit user constraint.)*

## Evidence on Hand

None. There are no testimonials, user counts, benchmarks, or third-party claims, and none may be invented. Every number shown comes from reps somebody actually logged — the user's own, or an accepted friend's.

## Product Principles

1. **Logging beats browsing.** The path from opening the app to a recorded set is the product; everything else is secondary.
2. **Defaults do the work.** Today, and the last-used variation, are already selected. Typing a number is the only required act.
3. **Show the truth, including the gaps.** Empty days are visible, not hidden or softened.
4. **One movement, done seriously.** No feature sprawl into other exercises or training plans. Social exists only as comparison against people you chose: a roster and a leaderboard, no feed, no likes, no comments. *(Amended 2026-08-24 at the user's explicit request; the original principle ruled out social features outright.)*
5. **Thumb-first.** Every primary control is reachable and large enough to hit while out of breath.

## Accessibility & Inclusion

Touch targets at least 48×48 CSS px, met from 390 px up. The one exception is arithmetic: a seven-column month grid cannot reach 48 px wide at a 320 px viewport without horizontal scroll, so calendar cells land at 39×48 there. Pinch zoom preserved. Calendar intensity never the only channel — every day carries its numeric total and a full accessible name. Leaderboard bar length is likewise never the only channel: every row shows its number, and each row has a full accessible name naming its rank. Both tab sets follow the ARIA tabs pattern with a roving tabindex, and an incoming friend request is announced by a labelled region rather than by colour alone.
