/* A quiet daily reflection, available without a network or browser storage. */
"use strict";
(function (root) {
  const originals = [
    "Let each movement be a small act of care for the mind that lives within you.",
    "Feel the ground beneath your hands. You are supported in this moment.",
    "Strength grows in effort; peace grows in the kindness you bring to it.",
    "Take one quiet breath and let your body tell you that you are here.",
    "You have made space for yourself today. Let that be enough for this moment.",
    "Move with attention, rest with gratitude, and carry a little stillness onward.",
    "The body can be a doorway back to the present. Enter gently.",
    "Your practice is a place to return to yourself, one breath at a time.",
    "Let the steadiness you found in movement soften the rest of your day.",
    "Care for your body as you would tend a garden: patiently, with room to grow.",
    "Pause after the effort. Notice the quiet that was waiting underneath.",
    "There is room for both determination and gentleness in the same breath.",
    "Offer yourself the patience you would offer someone you love.",
    "Today you listened, moved, and returned. Let gratitude meet you here.",
    "Carry this small moment of presence with you beyond the last repetition.",
  ].map((text) => ({ text, author: "Daily reflection", source: "", source_url: "" }));
  const normalize = (text) => text.toLowerCase().replace(/[^\p{L}\p{N}]/gu, "");
  const sources = { QuoteSlate: "https://github.com/Musheer360/QuoteSlate", ZenQuotes: "https://zenquotes.io/" };
  const valid = (q) => q && typeof q.text === "string" && q.text.length >= 15 && q.text.length <= 520 && typeof q.author === "string" && q.author.length <= 120 && (q.source_url === "" || (Object.hasOwn(sources, q.source) && sources[q.source] === q.source_url));
  const offsetDay = (day, offset) => new Date(Date.parse(`${day}T12:00:00Z`) + offset * 86400000).toISOString().slice(0, 10);

  function create(storage) {
    const memory = new Map();
    function read(user) {
      if (memory.has(user)) return memory.get(user);
      let saved;
      try { saved = JSON.parse(storage?.getItem(`pushup-reflections:${user}`)); } catch { /* use bundled reflections */ }
      const record = {
        history: Object.fromEntries(Object.entries(saved?.history || {}).filter(([day, q]) => /^\d{4}-\d{2}-\d{2}$/.test(day) && valid(q))),
        pool: Array.isArray(saved?.pool) ? saved.pool.filter(valid).slice(0, 15) : [],
        fetched: typeof saved?.fetched === "string" ? saved.fetched : "",
        last: valid(saved?.last) ? saved.last : null,
      };
      memory.set(user, record);
      return record;
    }
    function write(user, record) {
      // Keep browsing history bounded without losing yesterday's selection.
      record.history = Object.fromEntries(Object.entries(record.history).sort(([a], [b]) => b.localeCompare(a)).slice(0, 60));
      try { storage?.setItem(`pushup-reflections:${user}`, JSON.stringify(record)); } catch { /* memory still works */ }
    }
    function forDay(day, user) {
      const record = read(user);
      if (record.history[day]) return record.history[day];
      const exclude = new Set([record.history[offsetDay(day, -1)], record.history[offsetDay(day, 1)], record.last].filter(Boolean).map((q) => normalize(q.text)));
      const online = record.pool.filter((q) => !exclude.has(normalize(q.text)));
      const offline = originals.filter((q) => !exclude.has(normalize(q.text)));
      const pool = online.length ? online : offline;
      const index = Math.floor(Date.parse(`${day}T12:00:00Z`) / 86400000);
      const quote = pool[((index % pool.length) + pool.length) % pool.length];
      record.history[day] = record.last = quote;
      write(user, record);
      return quote;
    }
    async function refresh(day, user, request) {
      const record = read(user);
      if (record.fetched === day) return;
      record.fetched = day;
      write(user, record);
      try {
        const result = await request();
        record.pool = Array.isArray(result.quotes) ? result.quotes.filter(valid).slice(0, 15) : [];
        write(user, record);
      } catch { /* the existing pool and bundled reflections remain available */ }
    }
    return { forDay, refresh };
  }
  let storage;
  try { if (typeof window !== "undefined") storage = root.localStorage; } catch { /* denied storage */ }
  root.PushupQuotes = create(storage);
  if (typeof module !== "undefined") module.exports = { create, originals, normalize };
})(typeof window === "undefined" ? globalThis : window);
