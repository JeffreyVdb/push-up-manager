# Daily reflections and quote providers

Research checked **2026-10-24** against provider documentation, source repositories,
and direct HTTPS requests from the development machine. HTTP results below are
point-in-time observations, not uptime guarantees.

## Decision

Use a small Go adapter for multiple providers, cache accepted results, and ship
15 original reflections with the app. Prefer QuoteSlate's thematic filtering;
fall back to ZenQuotes with local content filtering. Affirmations is a viable
third adapter for gentle encouragement. Always finish with the bundled collection
when a provider is unavailable or has no suitable content.

The purpose is a moment of presence after movement: kindness, breath, gratitude,
and the relationship between caring for the body and making room for the mind.
Avoid competitive achievement slogans and promises that exercise cures mental
illness. These are product recommendations, not claims made by the providers.

## Provider comparison

| Provider | Integration | Constraints and fit | Live check |
| --- | --- | --- | --- |
| [QuoteSlate](https://github.com/Musheer360/QuoteSlate) | `GET https://quoteslate.vercel.app/api/quotes/random?count=15&tags=mindfulness,gratitude,self-reflection&maxLength=220` returns an array with `quote`, `author`, and `tags`. | No key; supports CORS. Documented limit: 100 requests per 15 minutes per IP. Batch/cache results and link attribution to the project. Its shared host is intended for personal/small applications; the author asks public/commercial projects to self-host. | Random and tags endpoints returned HTTP 429. The documented adapter remains useful, but cannot be the only source. |
| [ZenQuotes](https://docs.zenquotes.io/zenquotes-documentation/) | `GET https://zenquotes.io/api/quotes` returns a batch with `q` and `a`. Ignore the supplied HTML `h`. | Free limit: 5 requests per 30 seconds per IP. Free access requires visible attribution linking to `https://zenquotes.io/`. CORS and keyword filtering need paid access; use the Go server and filter the free batch locally. Docs explicitly recommend caching batches. | Batch returned HTTP 200 with valid JSON. |
| [Affirmations](https://github.com/annthurium/affirmations) | `GET https://www.affirmations.dev/` returns `{ "affirmation": "…" }`. | No key in the documented public request; repository is MIT licensed. No category filter or published rate limit in its README. Encouraging, but less specifically spiritual; reject unsuitable copy and retain offline fallback. It supplies no author, so label it as an affirmation instead of inventing one. | Returned HTTP 200 and valid JSON. |

QuoteSlate's [tag data](https://github.com/Musheer360/QuoteSlate/blob/main/data/tags.json)
contains all three recommended tags. A direct inspection of its
[quote dataset](https://github.com/Musheer360/QuoteSlate/blob/main/data/quotes.json)
found 92 entries matching at least one of them. Categories do not guarantee that
every entry has the requested tone, so a small local acceptance filter is still
appropriate. Preserve author attribution exactly as supplied; the API does not
establish primary historical provenance for every quotation.

ZenQuotes' [terms](https://docs.zenquotes.io/terms-and-conditions/) describe
personal, noncommercial use and do not grant a general public-domain license to
the collection. Its API documentation authorizes the cache-oriented integration
above. Do not treat either provider's software license as proof that every
historical quote has been independently cleared. Bundle original writing instead
of copying a permanent external quote anthology.

## Alternatives checked

- [Quotable](https://github.com/lukePeavey/quotable) documents author/tag filters
  and a limit of 180 requests/minute/IP. Its public API hostname failed DNS
  resolution during this check. Exclude it from the default chain rather than
  adding another predictable delay.
- [QuotesDB](https://quotes-db.vercel.app/) documents category and author filters.
  Its categories endpoint returned HTTP 200 and included `Mindfulness`, but both
  filtered quote requests returned HTTP 504; random quotes worked. The inspected
  documentation did not provide clear rate-limit or reuse guidance. Prefer the
  better-documented providers above.
- [Quoterism](https://www.quoterism.com/developer) documents a free tier of 300
  requests/day and 60/minute, but requires an account and API key. It is an option
  if credentials are introduced later, not a zero-setup default.

## Application behavior

These are implementation recommendations:

1. Keep provider details behind one same-origin endpoint. Send no account, goal,
   exercise, or calendar data to providers. Give each request a short timeout,
   cap response size, and validate JSON/text. Never render provider HTML.
2. Cache batches for at least a day and share upstream fetches across users.
   Cache failures briefly too; a provider outage must not trigger an upstream
   request for every page visit. Treat HTTP 429 and invalid/empty responses as
   fallback conditions. Respect `Retry-After` when present.
3. Store the selected reflection by the user's local calendar date. Reopening
   the app or recording another set should retain that day's selection.
4. Compare normalized quote text across all providers, not provider-specific
   IDs. Exclude yesterday's text, and preferably the most recently displayed
   text even when visits are farther apart. If an API only returns a duplicate,
   choose a different bundled reflection instead of retrying indefinitely.
5. Persist selection history and a small accepted pool on the device so offline
   visits retain the same daily experience. Include the original collection in
   the app assets cached by the service worker. Corrupt/unavailable local storage
   should fall back to an in-memory selection without blocking exercise logging.
6. Mark original entries “Daily reflection.” External entries show the author
   when available and a visible provider link. Do not substitute famous names for
   unattributed text.

## Offline collection

These 15 reflections were written for this app. They are original prompts, not
quotations attributed to a spiritual teacher or a historical text.

1. Let each movement be a small act of care for the mind that lives within you.
2. Feel the ground beneath your hands. You are supported in this moment.
3. Strength grows in effort; peace grows in the kindness you bring to it.
4. Take one quiet breath and let your body tell you that you are here.
5. You have made space for yourself today. Let that be enough for this moment.
6. Move with attention, rest with gratitude, and carry a little stillness onward.
7. The body can be a doorway back to the present. Enter gently.
8. Your practice is a place to return to yourself, one breath at a time.
9. Let the steadiness you found in movement soften the rest of your day.
10. Care for your body as you would tend a garden: patiently, with room to grow.
11. Pause after the effort. Notice the quiet that was waiting underneath.
12. There is room for both determination and gentleness in the same breath.
13. Offer yourself the patience you would offer someone you love.
14. Today you listened, moved, and returned. Let gratitude meet you here.
15. Carry this small moment of presence with you beyond the last repetition.
