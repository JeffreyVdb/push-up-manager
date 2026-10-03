# Design

## The world: FLOOR

The surface is the floor you are lying on at 5am, not a fitness dashboard. Concrete-dark, hard-edged, one signal colour that reads like a warning stripe. Nothing rounds off, nothing congratulates, nothing decorates. The interface is a piece of gym equipment: heavy, legible under bad light, indifferent to how you feel about it.

Mode: **Operate**. The visitor is mid-set. The task wins over expression, and the brand lives in the type, the rules, and the numerals.

Chosen dark from the use scene, not from category habit: a phone at arm's length in a dim room, held by someone who just finished a set.

## Palette

Tokens live in `web/app.css` on `:root`.

| Token | Value | Role |
|---|---|---|
| `--ink` | `#070807` | Page ground; the floor |
| `--slab` | `#101311` | Raised working surface |
| `--slab-2` | `#171B19` | Input wells, calendar cells with data |
| `--rule` | `#2A302C` | Hairlines; the only separator |
| `--bone` | `#EFEDE6` | Primary text |
| `--ash` | `#9AA29C` | Secondary text, tinted from the surface hue — never neutral gray |
| `--signal` | `#FF4B12` | Oxide orange. Action, today, the live number |
| `--signal-deep` | `#B32B06` | Pressed actions |
| `--warn` | `#FFC53D` | Errors and destructive confirmation |

The calendar uses a continuous cool-to-hot ramp for logged days: `--heat-1` `#8CAEB8` (cold steel) → `--heat-2` `#B7BF91` at 40% → `--heat-3` `#EFBE62` at 70% → `--heat-4` `#FF4B12` (hot signal). Each total is interpolated in sRGB against the month's peak, so nearby totals do not collapse into coarse buckets. Every logged day uses `--heat-ink` `#1A0A04`, with measured contrast of at least 5.75:1 across the ramp. Zero days retain `--heat-0` `#141715` and `--ash` text. The continuous legend describes logged-day intensity; the numeric total remains the primary readout.

One more accent, `--signal-mid` `#D13C0C`, exists only for the leaderboard's non-leader bars: it clears 3:1 against their track while leaving the leader's `--signal` a clear step above.

## Type

- **Display:** Big Shoulders Display, variable 400–900, self-hosted (`web/fonts/bigshoulders.woff2`, SIL OFL). Uppercase, weight 800–900, tracking `-0.03em`. Used for the wordmark, section headings, and the live rep total.
- **Text/UI:** Archivo, variable 400–800, self-hosted (`web/fonts/archivo.woff2`, SIL OFL). An industrial grotesque, chosen over the usual UI faces because it shares the display face's signage lineage. `font-variant-numeric: tabular-nums` everywhere a number can change.
- Scale steps are deliberate and few: `0.75 / 0.875 / 1 / 1.375 / 2 / 4.5rem`. The rep total is the only element allowed past 4rem.

## Form

- **Radius: 0.** Everything is a rectangle. Depth comes from value steps and hairlines, not from rounding.
- **Shadows** carry a real offset and blur (`0 18px 40px -24px rgb(0 0 0 / .9)`) and appear only on the sticky log bar. No zero-blur block shadows: this world is hard, not neobrutalist costume.
- **Separators** are 1px `--rule`. Emphasis rules are 3px `--signal` on the top edge of a block, never a left border on a card.
- **Structure is stacked bands**, not a grid of equal cards. Each band is titled by a rule and a heading, and bands are separated by generous space with tight internal grouping.

## Components

- **Log bar** — variation chips, a large numeric field, `+5 / +10 / +20` quick keys, and a commit button. Minimum 52px targets. It has two forms:
  - **Under 48rem** it is a modal bottom sheet, closed by default. A 64px circular FAB sits bottom-right; tapping it slides the sheet up, focuses the number field, makes the page behind it `inert`, and dims it with a scrim. Committing a set, the close button, or Escape slide it back down and return focus to the FAB. A tap on the scrim deliberately does **not** close it: losing a half-entered set to a stray tap while picking a variation is a worse failure than having to aim for the X. Screen space on a phone belongs to the calendar, not to a control the user needs for three seconds at a time.
  - **From 48rem up** there is room to keep it permanently docked as one row, and the FAB, scrim, and sheet header are removed entirely.
- **FAB** — the one round element in a rectangular world. A thumb target that reads as a physical button earns the exception; nothing else does.
- **Variation picker** — one trigger showing the current variation, so the control does not grow with the list. It is an ARIA 1.2 combobox with a listbox popup: focus stays on the trigger and `aria-activedescendant` moves, so it never fights the sheet's focus trap. Under 48rem the list is a full-screen overlay above the sheet; from 48rem up it is a dropdown anchored to the trigger. The selected row is `--signal` with a check; the active row takes an inset `--signal` bar.
  The choice is remembered per account in `localStorage`, falling back to the server's last-used entry, then to the first variation.
- **Calendar title** — the band title is the displayed month's total: a display-face tabular count (`12,345`) over a smaller `--ash` label (`this month`, `in March`, `in March 2025`). "This month" follows the server's today, and the count comes from the same response as the grid. It is not a live region; `#cal-label` stays the one announcer. At 360px and below the month nav wraps beneath it.
- **Calendar** — a real `<table>`, Monday first. Each day with data is a `<button>` carrying its interpolated heat color plus its visible total. Today gets `aria-current="date"` and a 2px `--bone` outline outside the cell, separated from the fill by a 1px dark gap. The outline preserves the filled area and contrasts against the grid regardless of heat color. A historical selection uses an inset border.
- **Stat band** — four figures in a row of hairline-separated columns (today, 7 days, month, streak). Deliberately not the hero-metric template: no accent card, no icon, no supporting sub-stat — plain columns with display numerals.
- **Section nav** — two tabs in the top bar, `LOG` and `CREW`, sharing the auth tabs' language: the active one is `--bone` over a 2px `--signal` underline. It switches which panel of the sheet is showing, not which page you are on — the log bar stays docked on both, so a set can still be committed while reading the board. An unanswered incoming request puts a `--signal` count chip on `CREW`, because the banner alone would only be seen by someone already looking at the top of the page.
- **Request banner** — sits inside the sticky chrome, directly under the top bar, so it pins in place instead of scrolling away. A 3px `--signal` top rule and a `--slab` ground mark it as an inserted block rather than a band of the page. Its content aligns to the same 40rem column as the sheet. It is capped at `42svh` and scrolls internally, so a pile-up of requests can never push the app off the screen. Deliberately not dismissible: a request is somebody waiting, and the only ways out are answering it. `DECLINE` is a two-step: it locks the sender out for a week and cannot be taken back, so the row restates the cost in `--warn` and asks for `TURN AWAY` / `KEEP`. The banner lives inside `.chrome`, which is what the log sheet marks `inert` — otherwise the sheet is `aria-modal` while two of its first tab stops sit outside it.
- **The board** — a leaderboard read as a bar chart. Each row is rank, name, total, then a full-width track beneath them, so the rows stack into a chart without a chart library. The bar carries the comparison and the number beside it carries the value, so length is never the only channel. The leader's bar is `--signal` at 100%; everyone else is `--signal-mid` at their share of the leader, with a 2% floor so a non-zero total is never invisible. The track is `--slab-2`, not the heat ramp's base: a bar can only be read as a *share* if its denominator is visible, and the bar itself has to clear 3:1 against that track to carry the comparison at all. Each non-leader row also prints `+35 TO PASS` against the row above — the distance is the number the board exists to produce, and leaving it as subtraction is work for somebody who is out of breath. The board reconciles on user id rather than rebuilding, so a redraw that changed nothing moves nothing at all — which is what makes it safe for the page to redraw whenever the server says something changed. A bar grows from zero only when its row is new to the board; an existing row's bar travels to its new length (420ms, the same exponential ease as the commit slam), growing or shrinking but never refilling, and a row that changed rank slides to the place it now holds instead of teleporting there. `YOU` marks your own row in `--signal`, as a sibling of the truncating name rather than inside it, so a long username cannot clip away the one marker that identifies you. The period tabs (`TODAY / WEEK / MONTH / ALL TIME`) re-sort it, so the top row is always the biggest bar, and the heading names the day, Monday–Sunday date range, or month the window covers — never repeating the tab's own label, since two windows can hold identical numbers and the switch still has to be legible. A single row is not a ranking: it is hidden until there is something to rank, because crowning somebody for a total of zero is the congratulation the product refuses.
- **The crew** — the roster under the board: add-by-username, then each friend with their all-time total and a remove control. A separate `ASKED` list shows requests you sent, and says plainly when one was declined and how many days are left, rather than letting a silently failing retry explain itself.
- **Icons** — authored inline SVG, 1.75px stroke, square caps. No emoji, no glyph substitutes.

## Quick keys

`+5 / +10 / +20 / C` cancel their own pointer press so they never take focus from the number field. The on-screen keyboard therefore stays up and the keys hold still under a repeating thumb, and the new total is left selected so the next keystroke replaces it rather than appending. Cancelling the press also suppresses `:active`, so the pressed state is driven explicitly. Each tap fires a 12 ms haptic tick where the Vibration API exists (Android, including the installed PWA); iOS Safari does not implement it, so it is a silent no-op there.

## Voice on the social surface

Nothing congratulates and nothing pleads. A request reads `alice wants in. Accept?` and the answers are `ACCEPT` / `DECLINE`. An empty board says `NOBODY TO BEAT. ADD SOMEONE BELOW.` A declined request is stated as a fact with its cost attached — `Declined · 5 days` — because hiding it would only make the next retry fail for no visible reason.

## Motion

The motion direction is **mechanical scoreboard**, chosen by the user. A number landing is the signature: changed totals settle through a small vertical compression and brief blur over 420ms using `cubic-bezier(.16,1,.3,1)`. Committing a set keeps the stronger counter slam and one heat pulse on its day. Unchanged refreshes trigger no motion.

The opening scoreboard settles its stats and calendar rows over 480ms, with 45ms between rows. This sequence runs once on entry; refreshes preserve the existing grid. Navigation communicates direction. The calendar and month label enter from the browsing direction over 480ms/340ms, clipped by a viewport that leaves room for the outside today marker. A 2px signal rail travels between selected tabs over 340ms using translation and scaling. Switching Log/Crew settles the destination panel over 420ms. Leaderboard bars scale to their new shares, and overtaking rows slide into place over 420ms. The log sheet retains its 340ms slide.

Scripted motion is cancellable, never queues behind rapid input, and skips hidden or offscreen elements. Data and focus update immediately. All motion respects `prefers-reduced-motion`; changing that preference also cancels animations already running. CSS provides the static state, so navigation and logging remain functional without animation support.

## Browser surfaces

Themed, not defaulted: `::selection` (signal on ink), `caret-color`, `accent-color`, custom scrollbar track and thumb, a 2px `--signal` focus ring with a 2px offset, and `text-underline-offset`.

## Voice

Imperative and short. Buttons name the act: `LOG IT`, `ADD VARIATION`, `SIGN IN`. Empty states state the gap without cheering: "NOTHING LOGGED TODAY." Errors name the problem and the recovery: "That username is taken. Pick another." No exclamation marks, no congratulation for the minimum.
