---
name: Push-Up Counter — ROOTED
description: A community garden visual world with the original tough-love voice.
colors:
  sage: "#edf3e7"
  paper: "#fcfdf8"
  sage-well: "#e1eadb"
  sage-rule: "#c6d3bd"
  forest-text: "#203d2e"
  muted-leaf: "#52634e"
  forest: "#356443"
  deep-forest: "#264b32"
  leaf-bar: "#648052"
  rust-warning: "#9a3f24"
  heat-empty: "#f0f3e9"
  heat-shoot: "#e1ebcf"
  heat-sprout: "#c4db9d"
  heat-leaf: "#a0c479"
  heat-full: "#80ae5f"
  heat-text: "#20351d"
  lavender: "#ece7f1"
  plum: "#665270"
  day-rule: "#cfc5d6"
  day-muted: "#62546a"
  selection: "#cadba9"
  classic-ink: "#070807"
  classic-slab: "#101311"
  classic-slab-2: "#171b19"
  classic-rule: "#2a302c"
  classic-bone: "#efede6"
  classic-ash: "#9aa29c"
  classic-signal: "#ff4b12"
  classic-deep: "#b32b06"
  classic-mid: "#d13c0c"
  classic-warn: "#ffc53d"
  classic-heat-empty: "#141715"
  classic-heat-cold: "#8caeb8"
  classic-heat-mid: "#b7bf91"
  classic-heat-warm: "#efbe62"
  classic-heat-text: "#1a0a04"
typography:
  display:
    fontFamily: '"Fraunces", Georgia, serif'
    fontSize: "clamp(2.25rem, 4vw, 3.5rem)"
    fontWeight: 500
    lineHeight: 1.12
    letterSpacing: "-.04em"
  wordmark:
    fontFamily: '"Fraunces", Georgia, serif'
    fontSize: "clamp(3.5rem, 13vw, 4.75rem)"
    fontWeight: 600
    lineHeight: 1.03
    letterSpacing: "-.04em"
  title:
    fontFamily: '"Fraunces", Georgia, serif'
    fontSize: "1.25rem"
    fontWeight: 500
    letterSpacing: "-.02em"
  number:
    fontFamily: '"Fraunces", Georgia, serif'
    fontSize: "2rem"
    fontWeight: 500
    lineHeight: 1
    letterSpacing: "-.02em"
  body:
    fontFamily: '"Archivo", system-ui, -apple-system, sans-serif'
    fontSize: ".9375rem"
    lineHeight: 1.5
  action:
    fontFamily: '"Archivo", system-ui, -apple-system, sans-serif'
    fontSize: ".75rem"
    fontWeight: 600
    letterSpacing: ".015em"
  classic-display:
    fontFamily: '"Big Shoulders", "Arial Narrow", sans-serif'
    fontSize: "clamp(3rem, 16vw, 4.5rem)"
    fontWeight: 900
    lineHeight: .84
    letterSpacing: "-.03em"
rounded:
  quick-key: "6px"
  mobile-day: "7px"
  control: "8px"
  day: "10px"
  picker: "12px"
  panel: "16px"
  log-pill: "28px"
  circle: "50%"
  classic: "0"
spacing:
  compact: ".5rem"
  control: "1rem"
  form: "1.25rem"
  panel: "1.5rem"
  mobile-section: "1.75rem"
  section: "2rem"
  desktop-gap: "2.5rem"
components:
  button-primary:
    backgroundColor: "{colors.forest}"
    textColor: "{colors.paper}"
    typography: "{typography.action}"
    rounded: "{rounded.control}"
    padding: "0 1rem"
  button-primary-hover:
    backgroundColor: "{colors.deep-forest}"
    textColor: "{colors.paper}"
  button-outline:
    textColor: "{colors.forest-text}"
    typography: "{typography.action}"
    rounded: "{rounded.control}"
    padding: "0 1rem"
  button-ghost:
    textColor: "{colors.muted-leaf}"
    typography: "{typography.action}"
    rounded: "{rounded.control}"
    padding: "0 1rem"
  input:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.forest-text}"
    rounded: "{rounded.control}"
    padding: "0 .875rem"
  calendar-panel:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.forest-text}"
    rounded: "{rounded.panel}"
    padding: "1.5rem"
  selected-day-panel:
    backgroundColor: "{colors.lavender}"
    textColor: "{colors.forest-text}"
    rounded: "{rounded.panel}"
    padding: "1.5rem"
  mobile-log:
    backgroundColor: "{colors.forest}"
    textColor: "{colors.paper}"
    rounded: "{rounded.log-pill}"
    height: "56px"
    padding: "0 1.25rem"
---

# Design System: Push-Up Counter

## Overview

**Creative North Star: "ROOTED — the community garden"**

ROOTED is the default plant-based visual world: a sage working surface, paper calendar, lavender selected day, forest actions, and a leaf beside a serif wordmark. The garden metaphor expresses repeated effort and visible accumulation. The interface remains a practical counter used between sets.

The user explicitly kept the tough-love language. Botanical form and unsentimental commands coexist: “No zero days.”, “Reps get logged or they did not happen.” and `LOG IT` remain direct. This identity introduces no dietary, health, or performance claims.

The original FLOOR world remains a complete Classic theme: dark concrete, oxide orange, condensed display type, hard edges and stacked bands. `web/app.css` retains that system and shared behavior; `web/garden.css` scopes the new appearance to `data-theme="plant"`. New visual work must preserve that boundary.

**Key Characteristics:**

- Sage page, paper calendar, lavender selected day, forest actions.
- Fraunces display and numbers; Archivo text and controls.
- A task grid on desktop and a thumb-first stack on mobile.
- The same rough imperative voice and logging workflow in both themes.

## Colors

The default palette pairs pale botanical grounds with dark green text; lavender identifies the selected-day workspace without competing with the action color. The frontmatter owns exact values, extracted from `web/garden.css`; its `classic-*` entries record the retained values from `web/app.css`.

### Primary

- **Forest** (`--signal`) identifies primary actions, focus, current-day outline and active rails. **Deep forest** (`--signal-deep`) handles hover and pressed actions.
- **Leaf bar** (`--signal-mid`) fills non-leader bars; the leader uses forest. Keep the visible track and printed totals.
- The calendar ramps from **shoot** through **sprout** and **leaf** to **full leaf** (`--heat-1` through `--heat-4`). Positive totals interpolate continuously in sRGB relative to the month's peak, with stops at 0%, 40%, 70% and 100%. Every logged cell uses **heat text**; empty days use **heat empty** and muted text. Numeric totals and accessible names remain authoritative.

### Secondary

- **Lavender** (`--petal`) grounds the selected-day panel; **plum** (`--plum`) colors its heading and total. That panel locally substitutes **day rule** and **day muted** for dividers and secondary text.
- **Rust warning** (`--warn`) identifies errors and destructive confirmations alongside explicit wording.

### Neutral

- **Sage** (`--ink`) is the page ground, not an occasional accent wash.
- **Paper** (`--slab`) supports calendar, fields, Settings and logging surfaces.
- **Sage well** (`--slab-2`) supports secondary fills, requests and comparison tracks; **sage rule** (`--rule`) separates controls and rows.
- **Forest text** (`--bone`) is primary text; **muted leaf** (`--ash`) is secondary text. Selection uses its own pale green token with forest text.

Classic uses the same CSS property roles with its own dark values. Its logged-day ramp remains cold steel → muted olive → amber → oxide orange, with one dark foreground. A theme change must recolor existing calendar cells through variables without changing the data.

**The Numeric Truth Rule.** Color and bar length supplement explicit numbers; neither is the sole carrier of activity or rank.

## Typography

**Display:** Fraunces, with Georgia and serif fallbacks; locally bundled variable weights 400–700 under the SIL Open Font License. **Text/UI:** Archivo, with system-ui and platform sans-serif fallbacks; locally bundled weights 400–800. Classic retains Big Shoulders, with Arial Narrow and sans-serif fallbacks, at weights 400–900. Font assets and license information live in `web/fonts/`; no runtime font service is required.

Fraunces supplies the organic wordmark, headings and large numbers. Archivo keeps commands and dense records legible. Tabular numerals prevent changing totals from shifting their columns.

- **Display:** the introductory heading uses the frontmatter display role; mobile fixes it at 2.5rem. The sign-in wordmark uses its separate wordmark role.
- **Titles:** general headings use the title role. The calendar and board title step up to 1.75rem; the calendar title becomes 1.5rem on mobile.
- **Numbers:** summary totals use the number role, reduced to 1.875rem on mobile. Selected-day totals use 2.5rem. Calendar totals use Archivo at 1.0625rem, reduced to .875rem on mobile.
- **Body:** the frontmatter body role governs the default plant surface. Text/password inputs remain 1rem.
- **Actions:** buttons and tabs use the action role and retain uppercase commands. Field labels use .8125rem without forced uppercase; compact metadata uses .6875rem.

Classic keeps uppercase condensed headings and heavier numerals; do not apply the plant type overrides globally.

## Layout

The default shell and desktop logging dock share a maximum width of 76rem. Desktop sheet padding is 2.5rem above, 2rem at the sides and 14rem below to clear the dock. The header is at least 80px high. The log view uses a two-column task grid (1.55:1, with a 2.5rem gap): introduction and statistics above, calendar on the left, selected day and variations on the right. Crew places its paper leaderboard panel beside the roster.

At 48–64rem, columns narrow to 1.35:1, gaps and outer padding reduce to 1.5rem, panel padding reduces to 1rem, and calendar controls stack. From 48rem, the dock is a single row: variation, number with quick keys, commit. Classic retains its original 40rem stacked sheet and dock arrangement.

Below 48rem, the plant view stacks introduction, statistics, calendar, selected day and variations with 1.75rem gaps. The header drops to 72px, the username and wordmark suffix hide, and side padding is 1rem with safe-area protection. The paper calendar extends .5rem into each gutter to protect seven-column legibility. Crew also stacks. The floating `LOG A SET` action opens the existing modal logging sheet. Bottom clearance and safe-area insets protect the action and content.

At widths below 359px the header tightens further. Calendar controls remain numeric and readable at 320px; narrow seven-column cells are the documented exception to the general 48px touch-target goal. Do not introduce horizontal page scrolling to force wider cells.

Settings shares the phone breakpoint in both themes: below 48rem it fills the viewport (100dvh), with square corners, 1rem side padding and safe-area padding above and below. Its opaque, ruled header keeps the close control visible while forms scroll. Desktop retains the 28rem dialog, 1.5rem padding and viewport height limit; the header remains sticky there too.

## Elevation & Depth

Most hierarchy comes from tonal panels, spacing and thin rules. The calendar and selected day are flat; shadows identify controls that float above content. The dock uses `--shadow-lift` (0 -12px 36px -20px rgb(32 61 46 / .22)); the mobile action uses a downward shadow (0 8px 24px -8px rgb(32 61 46 / .35)). Settings uses a native dialog and a dark translucent backdrop (rgb(7 24 17 / .6)). Classic retains its original darker dock shadow.

Shared motion keeps the existing mechanical scoreboard grammar: changed numbers land over 420ms; calendar and month navigation communicate direction over 340ms; tab rails travel over 340ms; sheets slide over 340ms. Plant changes the logged-day pulse to a small leaf-settle scale from .94 to 1. Motion uses `cubic-bezier(.16, 1, .3, 1)`, is cancellable and does not queue behind input. Unchanged refreshes do not animate. Reduced-motion preferences disable or cancel animation while data and focus update immediately.

## Shapes

Plant controls use softly curved corners: quick keys (6px), buttons and fields (8px), calendar days (10px desktop, 7px mobile), desktop picker (12px), and major panels and desktop Settings (16px). The mobile sheet rounds its top corners only; its full-screen picker and Settings have square corners. The mobile logging action is a 56px-high pill with 28px ends and a minimum width of 144px. Icon buttons and request badges are circular.

Classic retains square controls and panels, with its original circular mobile action. The shared theme switch is a deliberate rounded control in both worlds. Do not translate plant radii into Classic by editing unscoped selectors.

## Components

### Actions, fields and navigation

Primary actions use forest and paper text, darkening on hover or press. Outline actions use a rule border that turns forest on hover. Ghost actions use muted text and forest on hover; icon buttons add a sage-well hover fill. Visible keyboard focus is a 2px signal outline with a 2px offset. Text inputs use paper, a rule border, 52px minimum height and a signal focus border. Disabled buttons retain their shared reduced opacity and unavailable cursor.

The `LOG` / `CREW`, account and leaderboard period tabs retain their ARIA tab behavior and roving focus. A moving signal underline identifies selection. An incoming request adds a numeric circular badge to `CREW`. The log control remains available on the crew surface.

### Calendar, statistics and selected day

The calendar is a Monday-first table inside the paper panel. Each day carries a visible date, total and full accessible name. Today has `aria-current="date"` and an outer signal outline separated from the fill; historical selection uses an inset border. The month total and month navigation keep their existing meanings and announcements. Preserve the four-column statistics row and its thin dividers; figures remain distinct from the calendar's month total.

The calendar follows horizontal touch, pen and mouse drags: right reveals the previous month, left reveals the next. A deliberate drag or short quick flick commits; short drags settle back over 240ms. Outgoing and incoming months slide together over 340ms, while arrow controls and keyboard activation remain available. Vertical scrolling and pinch zoom remain native, and swiping never selects a date. Reduced motion retains swipe navigation without spatial movement.

The lavender selected-day panel holds its date, total and entry rows. Variations sit below it without another enclosing panel. Inline removal controls and empty states keep the original direct wording.

Goal completion adds an SVG check and a bottom rule to the calendar cell without changing activity heat, today or selection styling. A text legend and daily-goal shortcut precede the grid; the completed-days count explains that each date uses its own target. Accessible day names include the target and completion state.

The selected-day goal sits between thin rules with explicit total/target text, the target's start date and a narrow signal-colored progress bar. Completion reveals a check and a reflection with smaller attribution and an underlined source link when available. ROOTED uses Fraunces (1.25rem, weight 500) for the reflection; Classic uses the shared body face (1.125rem). This calm completion copy is the requested exception to the rough logging voice. Crossing the target reveals the check and reflection over 700ms; unchanged refreshes do not replay it. A committed set that reaches the target brings the goal panel into view when needed. Reduced motion preserves the result without animation or smooth scrolling.

### Logging and variation picker

Mobile logging uses the original modal bottom sheet: opening focuses the numeric field, makes the background inert and shows a scrim. Commit, Close or Escape closes it and returns focus to the floating action. A stray scrim tap does not discard the workflow. Desktop keeps logging docked and visible.

The variation trigger is an ARIA combobox with a listbox popup, full-screen on mobile and anchored on desktop. Focus stays on the trigger while the active descendant moves; selection uses signal fill and a check. The per-account last-used variation preference is separate from the browser-wide theme preference.

Quick keys `+5 / +10 / +20 / C` preserve number-field focus and selection so repeated tapping does not dismiss the keyboard. The explicitly driven press state and 12ms Vibration API tick remain shared; unsupported vibration is a no-op.

### Crew and requests

The board remains a bar comparison: rank, name, total, visible track and `TO PASS` distance. Forest identifies the leader and leaf-bar fills other shares, with a floor for nonzero totals. `YOU` remains outside the truncating name. Existing rows reconcile by user ID and travel to changed positions or totals; unchanged refreshes do not refill bars. The board remains hidden when there is only one person to rank.

The request banner stays in sticky chrome and is capped at 42svh with internal scrolling. Plant uses sage-well fill without Classic's signal top rule. Accept/decline behavior, destructive confirmation, announcement and the `ASKED` list remain shared. Keep competitive empty states and clear consequences.

### Settings and theme persistence

Settings is available on both sign-in and app headers. Its native modal dialog provides focus containment, Escape dismissal and focus return. The **Classic theme** switch selects `classic` when on and `plant` when off; plant is the default for missing or unknown preferences.

Signed-in Settings places **Daily goal** between appearance and account recovery. Its numeric field keeps the shared 1rem input text and requests a numeric phone keyboard. The preview names the start date and explains same-day replacement before the save action; earlier targets appear in a collapsible history. Goal controls and status messages inherit the existing field, action and alert treatments in both themes.

`web/theme.js` runs before styles and reads `pushup-counter:theme` from localStorage. It updates the root theme, browser color scheme, theme-color metadata and favicon; storage events synchronize tabs, including a return to plant when storage is cleared. Switching requires no reload and preserves drafts and app state. When saving is denied, the theme still changes for the visit and Settings states: “Theme changed for this visit. Enable browser storage to remember it.” Theme remains a browser-local preference; recovery email, permanent reminder dismissal and passkey registrations are persisted through the account APIs.

### Account, recovery and connection states

Signed-in Settings adds **Recovery email** and **Passkeys** below the daily goal. Account sections use a thin rule, 2rem top margin and 1.5rem top padding within the responsive Settings surface. Forms retain the shared field stack and 52px inputs. Action rows wrap with .5rem gaps and 48px minimum button height. Named passkeys use plain divided rows with a removal action; long names and addresses wrap. Account controls inherit the active theme's typography, colors and shapes.

Email remains optional. The account suggestion uses sage-well fill, a thin rule, direct text and `ADD EMAIL` / dismissal actions. A saved dismissal persists for the account. Settings distinguishes verified and pending addresses and exposes confirmation resend and removal when applicable. Email changes and passkey registration/removal require the current password; destructive removal uses explicit confirmation. Passkey availability, request progress, cancellation and failures use text status or alert messages beside the relevant form. Do not replace the password path with the passkey alternative.

Sign-in alternatives sit below the password form with .75rem spacing and a 1.25rem top margin. The passkey action appears in supported secure browser contexts. `/forgot-password`, `/reset-password` and `/confirm-email` reuse the existing narrow sign-in layout, shared labels, fields, primary action and `BACK TO SIGN IN` link. Reset uses two new-password fields; confirmation uses its own status and action. Errors use the shared alert and `role="alert"`; progress and outcomes use `role="status"`. Busy forms disable their actions and restore them when the request completes.

Offline and pending-sync feedback appears above app content in a labelled region, using `--slab-2` fill, `--bone` text, a thin rule and 1rem padding. Text states how many sets remain on this device and whether they are waiting or syncing; reconnect or sign-in actions remain explicit. Variations, crew and deleting synced entries require a connection. Blocked pending sets show their count, variation, original date and error, with applicable repair actions and a confirmed discard action. Keep numeric status and explanatory copy alongside controls; color alone never conveys whether a set reached the server.

### Identity and browser surfaces

The plant header uses the authored inline leaf SVG with rounded caps and joins. Shared icons remain authored SVG; Classic keeps square caps. The installed app retains the existing tally mark, recolored in forest and sage. The manifest and Apple touch icon remain plant-themed even when Classic is selected; the in-app favicon and theme-color metadata follow the current theme. Native installed iOS status-bar appearance has not been verified.

Selection, caret, accent color, scrollbars and link underline offset remain themed. The theme choice is not an accessibility mode: both appearances must retain focus, numeric alternatives, pinch zoom and reduced-motion behavior.

## Do's and Don'ts

### Do:

- **Do** keep the rough imperative voice in both visual worlds.
- **Do** scope botanical color, type, shape and layout changes to the plant theme.
- **Do** keep logged totals and accessible names alongside calendar color and leaderboard bars.
- **Do** preserve drafts, focus and the existing logging workflow when switching themes.
- **Do** ship fonts and icons locally with the embedded app shell.

### Don't:

- **Don't** replace the retained Classic stylesheet with a partial dark approximation.
- **Don't** turn the plant identity into dietary advice, training promises or invented evidence.
- **Don't** round every shared component globally or replace the task layout with uniform cards.
- **Don't** make theme selection an account or server setting without a separate product decision.
- **Don't** animate unchanged data or delay data and focus updates behind motion.
