/* FLOOR — push-up counter client. No framework, no build step. */
"use strict";

const $ = (id) => document.getElementById(id);

// CSS handles the declarative side; this is for the motion driven from script.
const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

const state = {
  csrf: "",
  username: "",
  today: "",
  types: [],
  selectedTypeId: 0,
  selectedDay: "",
  cal: { year: 0, month: 0 },
  authMode: "login",
  view: "log",
  crew: { friends: [], incoming: [], outgoing: [], leaderboard: [] },
  period: "daily",
};

/* ---------------------------------------------------------- api */

async function api(method, path, body) {
  const opts = {
    method,
    headers: { Accept: "application/json" },
    credentials: "same-origin",
  };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  if (method !== "GET" && state.csrf) opts.headers["X-CSRF-Token"] = state.csrf;

  let res;
  try {
    res = await fetch(path, opts);
  } catch {
    throw new Error("No connection to the server. Try again when you are back online.");
  }
  let data = null;
  if (res.status !== 204) {
    try { data = await res.json(); } catch { data = null; }
  }
  if (!res.ok) {
    const err = new Error((data && data.error) || `Request failed (${res.status}).`);
    err.status = res.status;
    throw err;
  }
  return data;
}

/* ---------------------------------------------------------- utils */

function showError(el, msg) {
  if (!msg) { el.hidden = true; el.textContent = ""; return; }
  el.textContent = msg;
  el.hidden = false;
}

// Writing a value an element already holds still counts as a mutation, which
// is how a live region re-announces a month that did not change. Every write a
// background refresh can reach compares first.
function setText(el, text) { if (el.textContent !== text) el.textContent = text; }
function setAttr(el, name, value) { if (el.getAttribute(name) !== value) el.setAttribute(name, value); }
// The reflected boolean setter writes the attribute every time, so an unguarded
// `hidden` is a mutation on every refresh — including on the one live region.
function setHidden(el, on) { if (el.hidden !== on) el.hidden = on; }

// One motion per element: a second tap retargets instead of queueing effects.
// Values and interaction state are committed first; motion never delays input.
const motions = new Map();
const MOTION_EASE = "cubic-bezier(.16, 1, .3, 1)";
function move(el, frames, duration = 380, delay = 0) {
  motions.get(el)?.cancel();
  motions.delete(el);
  if (reduceMotion.matches || document.hidden || !el.animate) return;
  const rect = el.getBoundingClientRect();
  if (!rect.width || !rect.height || rect.bottom < 0 || rect.top > window.innerHeight) return;
  const animation = el.animate(frames, { duration, delay, fill: "backwards", easing: MOTION_EASE });
  motions.set(el, animation);
  animation.addEventListener("finish", () => {
    if (motions.get(el) === animation) motions.delete(el);
  }, { once: true });
}

function setNumber(el, value) {
  const next = String(value);
  const previous = el.textContent;
  if (previous === next) return;
  setText(el, next);
  if (!/^[\d,]+$/.test(previous)) return;
  move(el, [
    { transform: "translateY(-.18em) scaleY(1.12)", filter: "blur(1px)", opacity: .45 },
    { transform: "none", filter: "blur(0)", opacity: 1 },
  ], 420);
}

function moveTabRail(tab) {
  if (!tab || !tab.offsetWidth) return;
  const group = tab.parentElement;
  group.style.setProperty("--rail-x", `${tab.offsetLeft}px`);
  group.style.setProperty("--rail-width", String(tab.offsetWidth));
  group.classList.add("has-rail");
}

function syncTabRails() {
  document.querySelectorAll(".vtab.is-on, .ptab.is-on").forEach(moveTabRail);
}

// Puts `order` in place under `parent`, keeping every element that survived.
// An element that is never replaced never drops the focus it is holding, which
// is what a background refresh used to do to anyone browsing by keyboard.
function reconcile(parent, order) {
  const live = new Set(order);
  // A row can be taken away from under the focus — a friend removed, a request
  // answered, both from another device. Nothing catches the focus when it goes,
  // so note where it stood and hand it on once the list has settled.
  let orphan = null;
  let kept = 0;
  for (const el of [...parent.children]) {
    if (live.has(el)) { kept++; continue; }
    if (el.contains(document.activeElement)) {
      const acts = [...el.querySelectorAll("button:not([disabled])")];
      orphan = { at: kept, slot: Math.max(0, acts.indexOf(document.activeElement)) };
    }
    el.remove();
  }

  // insertBefore on a connected node is a remove and then an insert, and
  // removing the node holding focus runs the focus fixup, so a row that moves
  // up — a variation renamed elsewhere resorts the list — throws focus to
  // <body>. moveBefore carries focus and the rest of a node's state across the
  // move. Chrome 133+; elsewhere the pass below puts the focus back.
  const canMove = typeof parent.moveBefore === "function";
  const held = document.activeElement;
  for (const [i, el] of order.entries()) {
    if (parent.children[i] === el) continue;
    const ref = parent.children[i] || null;
    // moveBefore only takes a node already in the tree; a freshly built row
    // has no parent yet.
    if (canMove && el.parentNode === parent) parent.moveBefore(el, ref);
    else parent.insertBefore(el, ref);
  }
  if (held && held !== document.activeElement && held.isConnected) {
    held.focus({ preventScroll: true });
  }
  if (orphan) rehome(parent, orphan.at, orphan.slot);
}

// Focus was standing on a row that is now gone. Give it to the same control on
// whatever row took that place, so a keyboard or screen-reader user carries on
// where they were instead of being dropped at the top of the page.
function rehome(parent, at, slot) {
  const row = parent.children[Math.min(at, parent.children.length - 1)];
  const acts = row ? row.querySelectorAll("button:not([disabled])") : [];
  const heir = acts[Math.min(slot, acts.length - 1)];
  if (heir) { heir.focus({ preventScroll: true }); return; }
  // The list emptied. It can still take the focus itself and name what it is —
  // unless the whole region went with the last row, and then the section nav is
  // the nearest thing left on screen.
  if (parent.offsetParent) {
    parent.tabIndex = -1;
    parent.focus({ preventScroll: true });
    return;
  }
  const nav = document.querySelector(".vtab.is-on");
  if (nav) nav.focus({ preventScroll: true });
}

// A short haptic tick. Android Chrome supports the Vibration API, including
// in an installed PWA; iOS Safari does not implement it at all, so this is a
// silent no-op there rather than a broken feature.
function buzz(pattern) {
  if (typeof navigator.vibrate !== "function") return;
  try {
    navigator.vibrate(pattern);
  } catch {
    // Blocked by the browser (no user activation, hidden page): not worth
    // reporting, the tap already gave visual feedback.
  }
}

let toastTimer = 0;
function toast(msg) {
  const el = $("toast");
  el.textContent = msg;
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.hidden = true; }, 2400);
}

const MONTHS = ["January","February","March","April","May","June",
  "July","August","September","October","November","December"];
const WEEKDAYS = ["Sunday","Monday","Tuesday","Wednesday","Thursday","Friday","Saturday"];

// Band title for the displayed month. "Current" is the server's today, not
// state.cal, so the title cannot claim "this month" for a month already over.
function calTitle(total, year, month, today) {
  const [ty, tm] = today.split("-").map(Number);
  let label = "this month";
  if (year !== ty || month !== tm) {
    label = `in ${MONTHS[month - 1]}${year === ty ? "" : ` ${year}`}`;
  }
  return { count: total.toLocaleString("en-US"), label };
}

// Parse a YYYY-MM-DD as a local date, avoiding the UTC shift of new Date(str).
function parseDay(s) {
  const [y, m, d] = s.split("-").map(Number);
  return new Date(y, m - 1, d);
}
function fmtDay(s) {
  const d = parseDay(s);
  return `${WEEKDAYS[d.getDay()]}, ${MONTHS[d.getMonth()]} ${d.getDate()}, ${d.getFullYear()}`;
}
// Short form for the commit button, which has little room.
function fmtDayShort(s) {
  const d = parseDay(s);
  return `${MONTHS[d.getMonth()].slice(0, 3)} ${d.getDate()}`;
}
function pad2(n) { return String(n).padStart(2, "0"); }
function dayKey(y, m, d) { return `${y}-${pad2(m)}-${pad2(d)}`; }

/* ---------------------------------------------------------- auth */

function setAuthMode(mode) {
  state.authMode = mode;
  for (const tab of document.querySelectorAll(".tab")) {
    const on = tab.dataset.mode === mode;
    tab.classList.toggle("is-on", on);
    tab.setAttribute("aria-selected", String(on));
  }
  const creating = mode === "register";
  $("auth-submit").textContent = creating ? "CREATE ACCOUNT" : "SIGN IN";
  $("auth-pass").autocomplete = creating ? "new-password" : "current-password";
  $("pass-hint").hidden = !creating;
  showError($("auth-error"), "");
}

async function submitAuth(event) {
  event.preventDefault();
  const btn = $("auth-submit");
  const username = $("auth-user").value.trim();
  const password = $("auth-pass").value;

  if (username.length < 3) {
    showError($("auth-error"), "Username needs at least 3 characters.");
    $("auth-user").focus();
    return;
  }
  if (password.length < 8) {
    showError($("auth-error"), "Password needs at least 8 characters. Make it longer.");
    $("auth-pass").focus();
    return;
  }

  btn.disabled = true;
  const label = btn.textContent;
  btn.textContent = "…";
  try {
    const path = state.authMode === "register" ? "/api/register" : "/api/login";
    const res = await api("POST", path, { username, password });
    state.csrf = res.csrf_token;
    state.username = res.username;
    state.today = res.today;
    $("auth-pass").value = "";
    await enterApp();
  } catch (err) {
    showError($("auth-error"), err.message);
  } finally {
    btn.disabled = false;
    btn.textContent = label;
  }
}

async function logout() {
  disconnectEvents();
  try { await api("POST", "/api/logout"); } catch { /* sign out locally anyway */ }
  state.csrf = "";
  state.username = "";
  $("app").hidden = true;
  $("gate").hidden = false;
  $("auth-user").focus();
}

/* ---------------------------------------------------------- types */

// One variation row. Built once per type, then written in place: the list is
// re-rendered on every hint now, and rebuilding it would take the focus off a
// delete button somebody had tabbed to.
function buildType(id) {
  const li = document.createElement("li");
  li.dataset.typeId = String(id);

  const name = document.createElement("span");
  name.className = "t-name";

  const total = document.createElement("span");
  total.className = "t-total tnum";

  const del = document.createElement("button");
  del.type = "button";
  del.className = "btn btn-ghost btn-icon";
  del.innerHTML = '<svg class="ico" aria-hidden="true"><use href="#i-trash"></use></svg>';
  // Looked up at click time: a kept row outlives a rename from another device,
  // and the confirmation has to name what the type is called now.
  del.addEventListener("click", () => {
    const t = state.types.find((x) => x.id === id);
    if (t) deleteType(t);
  });

  li.append(name, total, del);
  return li;
}

function emptyTypeRow() {
  const li = document.createElement("li");
  li.className = "empty";
  li.dataset.typeId = "none";
  li.textContent = "NO VARIATIONS YET. ADD ONE.";
  return li;
}

function renderTypes() {
  const list = $("type-list");
  const kept = new Map();
  for (const li of list.children) kept.set(li.dataset.typeId, li);

  const order = state.types.map((t) => {
    const li = kept.get(String(t.id)) || buildType(t.id);
    const [name, total, del] = li.children;
    setText(name, t.name);
    setText(total, `${t.total} total`);
    setAttr(del, "title", `Delete ${t.name}`);
    setAttr(del, "aria-label", `Delete ${t.name}`);
    return li;
  });
  if (order.length === 0) order.push(kept.get("none") || emptyTypeRow());
  reconcile(list, order);

  renderTrigger();
}

// The last variation is remembered per account on this device, so the app
// opens on what you actually use rather than on whatever sorts first.
function lastTypeKey() {
  return `pushup:lastType:${state.username}`;
}

function rememberType(id) {
  try {
    localStorage.setItem(lastTypeKey(), String(id));
  } catch {
    // Private mode or blocked storage: the server's last-used value still
    // covers this, so there is nothing to recover from.
  }
}

function recallType() {
  try {
    return parseInt(localStorage.getItem(lastTypeKey()), 10) || 0;
  } catch {
    return 0;
  }
}

function selectedType() {
  return state.types.find((t) => t.id === state.selectedTypeId) || null;
}

function renderTrigger() {
  const current = selectedType();
  const trigger = $("type-trigger");
  setText($("type-current"), current ? current.name : "No variations yet");
  trigger.disabled = state.types.length === 0;
  $("log-submit").disabled = state.types.length === 0;
}

/* ---------------------------------------------------------- variation picker */

// ARIA 1.2 combobox with a listbox popup: focus never leaves the trigger, so
// the picker cannot fight the log sheet's focus trap.
function buildOption(id) {
  const li = document.createElement("li");
  li.className = "opt";
  li.id = `opt-${id}`;
  li.setAttribute("role", "option");
  li.dataset.id = String(id);

  const name = document.createElement("span");
  name.className = "opt-name";

  const total = document.createElement("span");
  total.className = "opt-total tnum";

  // createElementNS is required for SVG inside an HTML document.
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("class", "ico");
  svg.setAttribute("aria-hidden", "true");
  const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
  use.setAttribute("href", "#i-check");
  svg.append(use);

  li.append(name, total, svg);
  li.addEventListener("click", () => {
    chooseType(id);
    closePicker();
  });
  return li;
}

// Reconciled like everything else: a refresh landing while the picker is open
// must not move the highlight, drop aria-activedescendant, or scroll the list.
function renderOptions() {
  const list = $("type-listbox");
  const kept = new Map();
  for (const li of list.children) kept.set(li.dataset.id, li);

  reconcile(list, state.types.map((t) => {
    const li = kept.get(String(t.id)) || buildOption(t.id);
    const [name, total] = li.children;
    setText(name, t.name);
    setText(total, String(t.total));
    setAttr(li, "aria-selected", String(t.id === state.selectedTypeId));
    setAttr(li, "aria-label", `${t.name}, ${t.total} push-ups logged`);
    return li;
  }));
}

function pickerIsOpen() {
  return !$("type-picker").hidden;
}

function positionPicker() {
  // Only the docked layout anchors the panel; the mobile picker is full width.
  if (!docked()) return;
  const rect = $("type-trigger").getBoundingClientRect();
  const panel = $("picker-panel");
  panel.style.setProperty("--picker-x", `${Math.round(rect.left)}px`);
  panel.style.setProperty("--picker-y", `${Math.round(window.innerHeight - rect.top + 8)}px`);
  panel.style.setProperty("--picker-w", `${Math.round(rect.width)}px`);
}

function openPicker() {
  if (state.types.length === 0) return;
  renderOptions();
  $("type-picker").hidden = false;
  positionPicker();
  $("type-trigger").setAttribute("aria-expanded", "true");
  setActiveOption(state.selectedTypeId || (state.types[0] && state.types[0].id));
  $("type-trigger").focus({ preventScroll: true });
}

function closePicker() {
  if (!pickerIsOpen()) return;
  $("type-picker").hidden = true;
  const trigger = $("type-trigger");
  trigger.setAttribute("aria-expanded", "false");
  trigger.removeAttribute("aria-activedescendant");
  trigger.focus({ preventScroll: true });
}

function setActiveOption(id) {
  for (const el of $("type-listbox").querySelectorAll(".opt.is-active")) {
    el.classList.remove("is-active");
  }
  const el = document.getElementById(`opt-${id}`);
  // Nothing to point at — the option was deleted from another device — and
  // aria-activedescendant may not name an element that is gone.
  if (!el) { $("type-trigger").removeAttribute("aria-activedescendant"); return; }
  el.classList.add("is-active");
  el.scrollIntoView({ block: "nearest" });
  $("type-trigger").setAttribute("aria-activedescendant", el.id);
}

function activeOptionID() {
  const el = $("type-listbox").querySelector(".opt.is-active");
  return el ? parseInt(el.dataset.id, 10) : 0;
}

function moveActive(delta) {
  const ids = state.types.map((t) => t.id);
  if (ids.length === 0) return;
  const current = ids.indexOf(activeOptionID());
  const next = Math.min(ids.length - 1, Math.max(0, (current < 0 ? 0 : current) + delta));
  setActiveOption(ids[next]);
}

function chooseType(id) {
  state.selectedTypeId = id;
  rememberType(id);
  renderTrigger();
}

function pickerKeydown(event) {
  const open = pickerIsOpen();

  if (!open) {
    if (["ArrowDown", "ArrowUp", "Enter", " "].includes(event.key)) {
      event.preventDefault();
      openPicker();
    }
    return;
  }

  switch (event.key) {
    case "ArrowDown": event.preventDefault(); moveActive(1); break;
    case "ArrowUp": event.preventDefault(); moveActive(-1); break;
    case "Home": event.preventDefault(); setActiveOption(state.types[0].id); break;
    case "End": event.preventDefault(); setActiveOption(state.types[state.types.length - 1].id); break;
    case "Enter":
    case " ":
      event.preventDefault();
      if (activeOptionID()) chooseType(activeOptionID());
      closePicker();
      break;
    case "Escape":
      event.preventDefault();
      event.stopPropagation();
      closePicker();
      break;
    case "Tab":
      closePicker();
      break;
    default:
      break;
  }
}

async function loadTypes() {
  const res = await api("GET", "/api/types");
  state.types = res.types;

  const known = new Set(state.types.map((t) => t.id));
  if (!known.has(state.selectedTypeId)) {
    // This device's last choice wins, then the last one used anywhere, then
    // simply the first variation the account has.
    const remembered = recallType();
    if (known.has(remembered)) {
      state.selectedTypeId = remembered;
    } else if (known.has(res.last_used_type_id)) {
      state.selectedTypeId = res.last_used_type_id;
    } else {
      state.selectedTypeId = state.types[0] ? state.types[0].id : 0;
    }
  }
  // Read before the render disables the trigger: disabling the element holding
  // focus already drops it, so afterwards there is nothing left to ask.
  const wasPicking = pickerIsOpen() || document.activeElement === $("type-trigger");
  renderTypes();

  if (state.types.length === 0) {
    // The last variation was deleted from another device. An open picker has
    // nothing to show and a disabled trigger cannot hold the focus, so close it
    // and point whoever was standing there at the only thing left to do.
    closePicker();
    if (wasPicking) {
      // The log bar is docked on both views, so the add-variation field can be
      // on the panel that is not showing; the section nav always is.
      const target = sheetIsOpen() ? $("log-close") : $("type-name");
      (target.offsetParent ? target : document.querySelector(".vtab.is-on")).focus({ preventScroll: true });
    }
  } else if (pickerIsOpen()) {
    renderOptions();
    // The highlighted option can be one that was just deleted elsewhere, and
    // aria-activedescendant may not point at nothing.
    if (!document.getElementById(`opt-${activeOptionID()}`)) setActiveOption(state.selectedTypeId);
  }
}

async function createType(event) {
  event.preventDefault();
  const input = $("type-name");
  const name = input.value.trim();
  if (!name) {
    showError($("type-error"), "Give the variation a name first.");
    input.focus();
    return;
  }
  try {
    const created = await api("POST", "/api/types", { name });
    input.value = "";
    showError($("type-error"), "");
    state.selectedTypeId = created.id;
    await loadTypes();
    toast(`${created.name} added`);
  } catch (err) {
    showError($("type-error"), err.message);
  }
}

async function deleteType(t) {
  const ok = confirm(`Delete "${t.name}" and every rep logged against it? This cannot be undone.`);
  if (!ok) return;
  try {
    await api("DELETE", `/api/types/${t.id}`);
    if (state.selectedTypeId === t.id) state.selectedTypeId = 0;
    await Promise.all([loadTypes(), loadCalendar(), loadDay(state.selectedDay), loadStats()]);
    toast(`${t.name} deleted`);
  } catch (err) {
    showError($("type-error"), err.message);
  }
}

/* ---------------------------------------------------------- keyboard inset */

// A `position: fixed` sheet is anchored to the layout viewport, which the
// on-screen keyboard does not shrink on iOS Safari, so the keyboard covers it.
// Publish the difference between the layout and visual viewports as
// --kb-inset and let CSS lift the sheet by that much. Where the browser does
// shrink the layout viewport (Chrome Android with
// interactive-widget=resizes-content) the difference is 0, so the two
// mechanisms compose instead of double-counting.
// The chrome grows when a request banner appears, so its height cannot be a
// constant in CSS. Publish it and let scroll-padding-top follow.
function trackChromeHeight() {
  const chrome = document.querySelector(".chrome");
  if (!chrome) return;
  const apply = () => {
    const h = Math.round(chrome.getBoundingClientRect().height);
    document.documentElement.style.setProperty("--chrome-h", `${h}px`);
  };
  if (typeof ResizeObserver === "function") new ResizeObserver(apply).observe(chrome);
  window.addEventListener("resize", apply);
  apply();
}

function trackKeyboardInset() {
  const vv = window.visualViewport;
  if (!vv) return;

  const apply = () => {
    const inset = Math.max(0, window.innerHeight - vv.height - vv.offsetTop);
    // Ignore a few stray pixels from rounding or a collapsing URL bar.
    const value = inset > 24 ? Math.round(inset) : 0;
    document.documentElement.style.setProperty("--kb-inset", `${value}px`);
    $("log").classList.toggle("kb-up", value > 0);
  };

  vv.addEventListener("resize", apply);
  vv.addEventListener("scroll", apply);
  apply();
}

/* ---------------------------------------------------------- log sheet */

// On a phone the log bar is a modal sheet behind the FAB; from 48rem up it is
// permanently docked and none of this applies.
const docked = () => window.matchMedia("(min-width: 48rem)").matches;

function openSheet() {
  if (docked()) return;
  $("log").classList.add("is-open");
  $("scrim").hidden = false;
  // Force a frame so the scrim transitions in rather than snapping.
  void $("scrim").offsetWidth;
  $("scrim").classList.add("is-open");
  $("fab").classList.add("is-hidden");
  $("fab").setAttribute("aria-expanded", "true");
  // The rest of the page is inert while the sheet owns the screen.
  $("app").querySelector(".sheet").inert = true;
  document.querySelector(".chrome").inert = true;
  document.body.style.overflow = "hidden";

  const reps = $("reps");
  reps.focus({ preventScroll: true });
  reps.select();
}

function closeSheet({ restoreFocus = true } = {}) {
  if (docked()) return;
  closePicker();
  $("log").classList.remove("is-open");
  $("scrim").classList.remove("is-open");
  $("fab").classList.remove("is-hidden");
  $("fab").setAttribute("aria-expanded", "false");
  $("app").querySelector(".sheet").inert = false;
  document.querySelector(".chrome").inert = false;
  document.body.style.overflow = "";
  showError($("log-error"), "");
  // Drop focus so the on-screen keyboard retracts with the sheet.
  $("reps").blur();

  // Keep the scrim in the tree until it has faded out.
  setTimeout(() => {
    if (!$("log").classList.contains("is-open")) $("scrim").hidden = true;
  }, 340);

  if (restoreFocus) $("fab").focus({ preventScroll: true });
}

function sheetIsOpen() {
  return $("log").classList.contains("is-open");
}

// Keep focus inside the sheet while it is modal.
function trapFocus(event) {
  if (event.key !== "Tab" || !sheetIsOpen()) return;
  const focusable = $("log").querySelectorAll(
    'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])'
  );
  if (focusable.length === 0) return;
  const first = focusable[0];
  const last = focusable[focusable.length - 1];
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}

/* ---------------------------------------------------------- logging */

async function submitLog(event) {
  event.preventDefault();
  const input = $("reps");
  const count = parseInt(input.value, 10);

  if (!Number.isFinite(count) || count <= 0) {
    showError($("log-error"), "Enter how many reps you did.");
    input.focus();
    input.select();
    return;
  }
  if (!state.selectedTypeId) {
    showError($("log-error"), "Pick a variation first.");
    return;
  }

  const btn = $("log-submit");
  btn.disabled = true;
  committing = true;
  try {
    // Day is omitted when it is today; the server defaults to today anyway.
    const body = { type_id: state.selectedTypeId, count };
    if (state.selectedDay && state.selectedDay !== state.today) body.day = state.selectedDay;

    const entry = await api("POST", "/api/reps", body);
    showError($("log-error"), "");
    rememberType(entry.type_id);
    // Close first so the slam animation happens on a visible page.
    closeSheet();
    // The other half of the gate: `committing` holds the next refresh off, but
    // one already in flight owns the loaders, and its older responses would
    // land after this commit's and put back what the set just changed. Only
    // the loaders wait — the POST above already went out.
    if (refreshRun) await refreshRun;
    // A set submitted across midnight defaults to the server's new day.
    if (!body.day) adoptToday(entry.day);
    // Settled, not Promise.all: all() rejects on the first loader to fail, so
    // `committing` cleared and the deferred refresh started while the other
    // four were still in flight. A loader that fails here only leaves the
    // screen behind — the set is saved, and nothing may say otherwise; the
    // next hint or foreground redraws it.
    dropStreamIfSignedOut(await Promise.allSettled([
      loadDay(entry.day), loadCalendar(), loadStats(), loadTypes(), loadFriends(),
    ]));
    slam(entry.day);
    toast(`+${entry.count} ${entry.type_name}`);
    if (docked()) input.select();
  } catch (err) {
    // Only the POST reaches here, and only a failed POST means no set landed.
    showError($("log-error"), err.message);
  } finally {
    btn.disabled = false;
    committing = false;
    drainPending();
  }
}

// The one authored moment: the committed total lands.
function slam(day) {
  const total = $("day-total");
  total.classList.remove("slam");
  void total.offsetWidth;
  total.classList.add("slam");

  const cell = document.querySelector(`.day[data-day="${day}"]`);
  if (cell) pulseDay(cell);
}

// One heat pulse on a day cell, restarted rather than queued. The stylesheet's
// reduced-motion override takes it out for anyone who asked for that.
function pulseDay(cell) {
  cell.classList.remove("pulse");
  void cell.offsetWidth;
  cell.classList.add("pulse");
}

async function deleteEntry(id) {
  try {
    await api("DELETE", `/api/reps/${id}`);
    await Promise.all([loadDay(state.selectedDay), loadCalendar(), loadStats(), loadTypes()]);
    toast("Entry removed");
  } catch (err) {
    showError($("log-error"), err.message);
  }
}

/* ---------------------------------------------------------- day */

// One logged set. Built once per entry id, then written in place.
function buildEntry(id) {
  const li = document.createElement("li");
  li.dataset.entryId = String(id);

  const count = document.createElement("span");
  count.className = "e-count tnum";

  const name = document.createElement("span");
  name.className = "e-name";

  const time = document.createElement("span");
  time.className = "e-time";

  const del = document.createElement("button");
  del.type = "button";
  del.className = "btn btn-ghost btn-icon";
  del.title = "Delete this entry";
  del.innerHTML = '<svg class="ico" aria-hidden="true"><use href="#i-trash"></use></svg>';
  del.addEventListener("click", () => deleteEntry(id));

  li.append(count, name, time, del);
  return li;
}

// Bumped by every call, so a response for a day the user has already moved off
// is dropped instead of redrawing the day they are looking at now.
let dayGen = 0;

async function loadDay(day) {
  state.selectedDay = day;
  const gen = ++dayGen;
  const res = await api("GET", `/api/day/${day}`);
  if (gen !== dayGen) return;

  setText($("day-label"), day === state.today ? `Today · ${fmtDay(day)}` : fmtDay(day));
  if (committing) setText($("day-total"), String(res.total));
  else setNumber($("day-total"), res.total);
  $("day-today").hidden = day === state.today;
  setText($("log-day"), day === state.today ? "today" : fmtDayShort(day));

  const list = $("day-entries");
  setHidden($("day-empty"), res.entries.length > 0);

  const kept = new Map();
  for (const li of list.children) kept.set(li.dataset.entryId, li);

  reconcile(list, res.entries.map((e) => {
    const li = kept.get(String(e.id)) || buildEntry(e.id);
    const [count, name, time, del] = li.children;
    setText(count, String(e.count));
    // The name travels with the type, so a rename elsewhere lands here too.
    setText(name, e.type_name);
    const at = new Date(e.created_at);
    setText(time, Number.isNaN(at.getTime())
      ? ""
      : at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }));
    setAttr(del, "aria-label", `Delete ${e.count} ${e.type_name}`);
    return li;
  }));

  markSelectedDay();
}

/* ---------------------------------------------------------- calendar */

function heatColor(total, peak) {
  if (total <= 0) return "var(--heat-0)";
  const ratio = Math.min(1, total / Math.max(1, peak));
  // Continuous interpolation keeps neighbouring totals distinct. All four
  // stops have enough luminance for the same ink foreground throughout.
  const stops = [0, 0.4, 0.7, 1];
  const high = stops.findIndex((stop, i) => i > 0 && ratio <= stop);
  const mix = ((ratio - stops[high - 1]) / (stops[high] - stops[high - 1])) * 100;
  return `color-mix(in srgb, var(--heat-${high}), var(--heat-${high + 1}) ${mix.toFixed(3)}%)`;
}

// The grid itself: the cells of one month, empty of totals. Only the month
// decides the shape, so this runs when the month changes and never on a
// refresh — rebuilding it would throw a keyboard out of the cell it is on.
function buildMonth(body, res) {
  const pad = () => {
    const td = document.createElement("td");
    td.className = "pad";
    td.setAttribute("aria-hidden", "true");
    return td;
  };

  body.replaceChildren();
  let row = document.createElement("tr");
  for (let i = 0; i < res.lead_blank; i++) row.append(pad());

  for (let d = 1; d <= res.days; d++) {
    if (row.children.length === 7) { body.append(row); row = document.createElement("tr"); }

    const key = dayKey(res.year, res.month, d);
    const td = document.createElement("td");
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "day";
    btn.dataset.day = key;

    const num = document.createElement("span");
    num.className = "d";
    num.textContent = String(d);

    const val = document.createElement("span");
    val.className = "t tnum";
    val.setAttribute("aria-hidden", "true");

    btn.append(num, val);
    btn.addEventListener("click", () => loadDay(key));

    td.append(btn);
    row.append(td);
  }

  while (row.children.length > 0 && row.children.length < 7) row.append(pad());
  if (row.children.length) body.append(row);
}

// Bumped by every call, so a response for a month the user has already browsed
// off cannot revert what they are looking at.
let calGen = 0;

async function loadCalendar() {
  const { year, month } = state.cal;
  const gen = ++calGen;
  const res = await api("GET", `/api/calendar?year=${year}&month=${month}`);
  if (gen !== calGen) return;

  // The label is a live region: writing the same month re-announces it.
  setText($("cal-label"), `${MONTHS[res.month - 1]} ${res.year}`);

  // Same response as the grid below, so title and cells never disagree.
  const title = calTitle(res.month_total, res.year, res.month, res.today);
  setNumber($("cal-count"), title.count);
  setText($("cal-count-label"), title.label);

  const totals = new Map(res.totals.map((t) => [t.day, t.total]));
  const peak = res.totals.reduce((max, t) => Math.max(max, t.total), 0);

  const body = $("cal-body");
  const shape = `${res.year}-${res.month}`;
  const previousShape = body.dataset.shape;
  const rebuilt = body.dataset.shape !== shape;
  if (rebuilt) {
    body.dataset.shape = shape;
    buildMonth(body, res);
  }

  for (const btn of body.querySelectorAll(".day")) {
    const key = btn.dataset.day;
    const total = totals.get(key) || 0;
    const val = btn.children[1];
    // A number that moves during a background refresh is somebody else's set
    // landing. It gets the same single heat pulse a committed day gets, and
    // only then — a commit has its own slam and does not run through here.
    if (!rebuilt && refreshing && val.textContent !== String(total)) pulseDay(btn);

    setText(val, String(total));
    setAttr(btn, "data-active", String(total > 0));
    const color = heatColor(total, peak);
    if (btn.style.getPropertyValue("--day-heat") !== color) btn.style.setProperty("--day-heat", color);
    setAttr(btn, "aria-label", `${fmtDay(key)}: ${total} push-ups`);
    // Today rolls over under a page that stays open all night.
    if (key === res.today) setAttr(btn, "aria-current", "date");
    else btn.removeAttribute("aria-current");
  }

  markSelectedDay();
  if (rebuilt && !previousShape) {
    // The opening scoreboard settles row by row, once. Background refreshes
    // keep the same grid and never replay this sequence.
    [...body.rows].forEach((row, index) => move(row, [
      { transform: "translateY(16px)", opacity: .15 },
      { transform: "none", opacity: 1 },
    ], 480, index * 45));
  } else if (rebuilt) {
    const [year, month] = previousShape.split("-").map(Number);
    const direction = res.year * 12 + res.month > year * 12 + month ? 1 : -1;
    move($("cal"), [
      { transform: `translateX(${direction * 64}px)`, opacity: .15 },
      { transform: "none", opacity: 1 },
    ], 480);
    move($("cal-label"), [
      { transform: `translateX(${direction * 10}px)`, opacity: .5 },
      { transform: "none", opacity: 1 },
    ], 340);
  }
}

// Cells survive a refresh now, so taking the mark off the selected day only to
// put it straight back would be a write the rest of this path avoids.
function markSelectedDay() {
  for (const el of document.querySelectorAll(".day.is-sel")) {
    if (el.dataset.day !== state.selectedDay) el.classList.remove("is-sel");
  }
  const cell = document.querySelector(`.day[data-day="${state.selectedDay}"]`);
  if (cell && !cell.classList.contains("is-sel")) cell.classList.add("is-sel");
}

function shiftMonth(delta) {
  let { year, month } = state.cal;
  month += delta;
  if (month < 1) { month = 12; year--; }
  if (month > 12) { month = 1; year++; }
  state.cal = { year, month };
  loadCalendar().catch((err) => toast(err.message));
}

/* ---------------------------------------------------------- stats */

async function loadStats() {
  const s = await api("GET", "/api/overview");
  setNumber($("s-today"), s.today);
  setNumber($("s-week"), s.week);
  setNumber($("s-month"), s.month);
  setNumber($("s-streak"), s.streak);
  setText($("s-all"), String(s.all_time));
  setText($("s-best"), String(s.best_day));
}

/* ---------------------------------------------------------- views */

// Two panels behind one nav, not two pages: the log bar stays docked on both,
// so a set can still be committed while reading the board.
function setView(view) {
  const previous = state.view;
  state.view = view;
  for (const tab of document.querySelectorAll(".vtab")) {
    const on = tab.dataset.view === view;
    tab.classList.toggle("is-on", on);
    tab.setAttribute("aria-selected", String(on));
    tab.tabIndex = on ? 0 : -1;
  }
  $("view-log").hidden = view !== "log";
  $("view-crew").hidden = view !== "crew";
  syncTabRails();
  if (view !== previous) move($("view-" + view), [
    { transform: "translateY(18px)", opacity: .25 },
    { transform: "none", opacity: 1 },
  ], 420);
  // A hash keeps the section across a reload without a router.
  const want = view === "crew" ? "#crew" : "";
  if ((location.hash || "") !== want) history.replaceState(null, "", location.pathname + want);
}

// Roving tabindex, per the ARIA tabs pattern.
function wireTablist(selector, activate) {
  const tabs = [...document.querySelectorAll(selector)];
  for (const tab of tabs) {
    tab.addEventListener("click", () => activate(tab, true));
    tab.addEventListener("keydown", (event) => {
      const step = event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
      if (step === 0) return;
      event.preventDefault();
      const next = tabs[(tabs.indexOf(tab) + step + tabs.length) % tabs.length];
      next.focus();
      activate(next, false);
    });
  }
}

/* ---------------------------------------------------------- friends */

// Mirrors declineBlockDays in api.go.
const DECLINE_BLOCK_DAYS = 7;

async function loadFriends() {
  const res = await api("GET", "/api/friends");
  state.crew = res;
  renderRequests();
  renderBoard();
  renderCrew();
}

// One incoming request. Nothing about it can change while it is open — it is
// answered or it is gone — so it is built once and never written again.
function buildRequest(req) {
  const li = document.createElement("li");
  li.className = "req";
  li.dataset.reqId = String(req.id);

  const text = document.createElement("p");
  text.className = "req-text";
  const who = document.createElement("strong");
  who.textContent = req.username;
  text.append(who, document.createTextNode(" wants in. Accept?"));

  const acts = document.createElement("div");
  acts.className = "req-acts";

  const yes = document.createElement("button");
  yes.type = "button";
  yes.className = "btn btn-primary btn-sm";
  yes.textContent = "ACCEPT";
  yes.addEventListener("click", () => answerRequest(req, "accept"));

  const no = document.createElement("button");
  no.type = "button";
  no.className = "btn btn-outline btn-sm";
  no.textContent = "DECLINE";
  no.addEventListener("click", () => armDecline(li, req));

  acts.append(yes, no);
  li.append(text, acts);
  return li;
}

// The ids already announced, so each arrival speaks once and nothing else does.
let said = new Set();

function renderRequests() {
  const pending = state.crew.incoming;
  const banner = $("requests");
  const badge = $("nav-badge");

  setHidden(banner, pending.length === 0);
  setHidden(badge, pending.length === 0);
  setText(badge, String(pending.length));
  setText($("req-count"), pending.length > 1 ? `${pending.length} waiting` : "");

  // The announcement is one sentence, kept out of the banner: the buttons stay
  // reachable and silent, and a row rewritten mid-decline says nothing. It is
  // driven by which request ids are new, not by how many are pending: a count
  // says nothing when one request is answered as another arrives, and repeats
  // the survivor when two become one. A refresh that changed nothing finds
  // nothing new and writes nothing, so it stays silent.
  const say = $("req-say");
  const fresh = pending.filter((req) => !said.has(req.id));
  said = new Set(pending.map((req) => req.id));
  if (pending.length === 0) setText(say, "");
  // Written straight, not compared: the same sentence for a different sender
  // has to speak again, and only a real arrival gets this far.
  else if (fresh.length === 1) say.textContent = `${fresh[0].username} wants in`;
  else if (fresh.length > 1) say.textContent = `${fresh.length} friend requests waiting`;

  // Reconciling on request id means a refresh that changed nothing inserts
  // nothing. It also leaves a row mid-decline alone rather than yanking the
  // confirmation out from under the answer.
  const list = $("request-list");
  const kept = new Map();
  for (const li of list.children) kept.set(li.dataset.reqId, li);
  reconcile(list, pending.map((req) => kept.get(String(req.id)) || buildRequest(req)));
}

// Declining locks the sender out for a week and cannot be taken back, so the
// cost is stated and confirmed before it happens. Deleting a variation already
// gets a confirmation; this is worse, because it lands on somebody else.
function armDecline(li, req) {
  li.classList.add("is-confirming");
  const text = li.querySelector(".req-text");
  const acts = li.querySelector(".req-acts");
  text.replaceChildren();
  const who = document.createElement("strong");
  who.textContent = req.username;
  text.append(
    document.createTextNode("Turn "), who,
    document.createTextNode(` away? They cannot ask again for ${DECLINE_BLOCK_DAYS} days.`)
  );

  acts.replaceChildren();
  const confirm = document.createElement("button");
  confirm.type = "button";
  confirm.className = "btn btn-primary btn-sm";
  confirm.textContent = "TURN AWAY";
  confirm.addEventListener("click", () => answerRequest(req, "decline"));

  const keep = document.createElement("button");
  keep.type = "button";
  keep.className = "btn btn-outline btn-sm";
  keep.textContent = "KEEP";
  // Only this row backs out, and the answer keeps the focus it was given.
  keep.addEventListener("click", () => {
    const fresh = buildRequest(req);
    li.replaceWith(fresh);
    fresh.querySelector(".req-acts").lastElementChild.focus({ preventScroll: true });
  });

  acts.append(confirm, keep);
  confirm.focus({ preventScroll: true });
}

async function answerRequest(req, action) {
  try {
    await api("POST", `/api/friends/requests/${req.id}/${action}`, {});
    // Answering can settle whatever the add-by-username form was complaining
    // about, so that message must not outlive it.
    showError($("friend-error"), "");
    await loadFriends();
    toast(action === "accept"
      ? `${req.username} is on your list`
      : `${req.username} turned away · ${DECLINE_BLOCK_DAYS} days`);
  } catch (err) {
    toast(err.message);
    // The request may have been withdrawn or already answered elsewhere.
    await loadFriends().catch(() => {});
  }
}

// One board row's skeleton. Built once per user, then updated in place.
function buildRow(userID) {
  const li = document.createElement("li");
  li.className = "lb";
  li.dataset.userId = String(userID);

  const rank = document.createElement("span");
  rank.className = "lb-rank tnum";

  const name = document.createElement("span");
  name.className = "lb-name";
  const who = document.createElement("span");
  who.className = "lb-who";
  // The username truncates; the YOU marker must not, or it disappears on
  // exactly the long name that makes a row hard to identify.
  const you = document.createElement("span");
  you.className = "lb-you";
  you.textContent = "YOU";
  you.hidden = true;
  // The distance to the row above is what the board exists to produce, and
  // leaving it as subtraction is work for somebody who is out of breath.
  const gap = document.createElement("span");
  gap.className = "lb-gap tnum";
  gap.hidden = true;
  name.append(who, you, gap);

  const num = document.createElement("span");
  num.className = "lb-num tnum";

  const track = document.createElement("span");
  track.className = "lb-track";
  const bar = document.createElement("span");
  bar.className = "lb-bar";
  track.append(bar);

  li.append(rank, name, num, track);
  return li;
}

// A row that changed rank slides to the place it now holds instead of
// teleporting there: the board is a ranking, so the overtake is the
// information. Mirrors --ease; WAAPI cannot read a custom property.
function slideIntoPlace(li, top) {
  if (top === undefined || reduceMotion.matches) return;
  const delta = top - li.getBoundingClientRect().top;
  if (!delta) return;
  move(li, [{ transform: `translateY(${delta}px)` }, { transform: "none" }], 420);
}

// The board is sorted by whichever window is showing, so the biggest bar is
// always the row on top.
//
// It reconciles on user_id rather than rebuilding: re-creating a row restarts
// the entrance keyframe, which is what used to make every bar refill from zero
// on a refresh that had changed nothing at all.
function renderBoard() {
  const key = state.period;
  const rows = [...state.crew.leaderboard].sort(
    (a, b) => b[key] - a[key] || a.username.localeCompare(b.username)
  );
  const peak = rows.reduce((max, r) => Math.max(max, r[key]), 0);

  // "ALL TIME" has no scope to name, and repeating a tab's own label would say
  // nothing; the other two windows name the day and the month they cover.
  const day = parseDay(state.today || "1970-01-01");
  const scope = {
    daily: `${MONTHS[day.getMonth()].slice(0, 3)} ${day.getDate()}`.toUpperCase(),
    weekly: weekLabel(state.today),
    monthly: MONTHS[day.getMonth()].toUpperCase(),
    all_time: "",
  };
  $("board-window").textContent = scope[key] || "";

  const board = $("board");
  // One row is not a ranking. Crowning someone #1 for a total of zero is the
  // congratulation for the minimum the product exists to refuse.
  const alone = rows.length <= 1;
  board.hidden = alone;
  $("board-empty").hidden = !alone;
  if (alone) { board.replaceChildren(); return; }

  // Writing a value it already holds changes nothing and starts no transition,
  // which is what makes an unchanged re-render completely still.
  const fill = (li, i) => {
    const row = rows[i];
    const [rank, name, num, track] = li.children;
    const [who, you, gap] = name.children;
    const ahead = i > 0 ? rows[i - 1][key] - row[key] : 0;

    setText(rank, String(i + 1));
    setText(who, row.username);
    setHidden(you, !row.me);
    setHidden(gap, !(i > 0 && ahead > 0));
    // Cleared when hidden as well, or a row promoted to first keeps the text
    // saying who it is still chasing.
    setText(gap, gap.hidden ? "" : `+${ahead} TO PASS`);
    setNumber(num, row[key]);

    li.classList.toggle("is-me", row.me);
    li.classList.toggle("is-top", peak > 0 && row[key] === peak);
    // A non-zero total always keeps a visible sliver of bar.
    const share = peak > 0 ? Math.max(row[key] > 0 ? 2 : 0, (row[key] / peak) * 100) : 0;
    track.firstChild.style.setProperty("--share", (share / 100).toFixed(4));

    const behind = i > 0 && ahead > 0 ? `, ${ahead} behind ${rows[i - 1].username}` : "";
    setAttr(li, "aria-label",
      `${i + 1}. ${row.username}${row.me ? " (you)" : ""}, ${row[key]} push-ups${behind}`);
  };

  const kept = new Map();
  for (const li of board.children) kept.set(Number(li.dataset.userId), li);

  const order = rows.map((row, i) => {
    const li = kept.get(row.user_id);
    if (li) return li;
    // A row nobody has seen before is filled while it is still detached, so
    // its entrance grows straight to the right length, and is marked so the
    // keyframe applies to it and to nothing else on the board.
    const fresh = buildRow(row.user_id);
    fill(fresh, i);
    fresh.classList.add("is-new");
    fresh.addEventListener("animationend", () => fresh.classList.remove("is-new"), { once: true });
    return fresh;
  });

  // Where every surviving row sits before anything moves.
  const from = new Map();
  for (const li of order) if (li.isConnected) from.set(li, li.getBoundingClientRect().top);

  const live = new Set(order);
  for (const li of [...board.children]) if (!live.has(li)) li.remove();
  for (const [i, li] of order.entries()) {
    if (board.children[i] !== li) board.insertBefore(li, board.children[i] || null);
  }
  for (const li of order) slideIntoPlace(li, from.get(li));

  // Contents last: moving a node cancels the transitions running on it, so a
  // bar can only travel to its new length once its row has stopped moving.
  for (const [i, li] of order.entries()) if (from.has(li)) fill(li, i);
}

function weekLabel(today) {
  const start = parseDay(today);
  start.setDate(start.getDate() - (start.getDay() + 6) % 7);
  const end = new Date(start);
  end.setDate(end.getDate() + 6);
  const first = `${MONTHS[start.getMonth()].slice(0, 3)} ${start.getDate()}`;
  const last = start.getMonth() === end.getMonth()
    ? String(end.getDate())
    : `${MONTHS[end.getMonth()].slice(0, 3)} ${end.getDate()}`;
  return `${first} – ${last}`.toUpperCase();
}

// One crew member. A username never changes, so only the total is written.
function buildFriend(f) {
  const li = document.createElement("li");
  li.dataset.friendId = String(f.id);

  const name = document.createElement("span");
  name.className = "f-name";

  const meta = document.createElement("span");
  meta.className = "f-meta tnum";

  const cut = document.createElement("button");
  cut.type = "button";
  cut.className = "btn btn-ghost btn-icon";
  cut.title = `Remove ${f.username}`;
  cut.setAttribute("aria-label", `Remove ${f.username}`);
  cut.innerHTML = '<svg class="ico" aria-hidden="true"><use href="#i-userout"></use></svg>';
  cut.addEventListener("click", () => removeFriend(f));

  li.append(name, meta, cut);
  return li;
}

function renderCrew() {
  const totals = new Map(state.crew.leaderboard.map((r) => [r.user_id, r.all_time]));

  const list = $("friend-list");
  setHidden($("friend-empty"), state.crew.friends.length > 0);

  // Keyed on user id: a friend's total moves on every set they log, and the
  // row carries a remove button somebody may be standing on.
  const kept = new Map();
  for (const li of list.children) kept.set(li.dataset.friendId, li);

  reconcile(list, state.crew.friends.map((f) => {
    const li = kept.get(String(f.id)) || buildFriend(f);
    const [name, meta] = li.children;
    setText(name, f.username);
    setText(meta, `${totals.get(f.id) ?? 0} all time`);
    return li;
  }));

  // Sent requests, so a decline is explained rather than left to guess at.
  // Sending one now invalidates the sender's own pages, so this list moves on
  // a hint like everything else and is reconciled like everything else.
  const sent = $("sent-list");
  setHidden($("h-sent"), state.crew.outgoing.length === 0);

  const asked = new Map();
  for (const li of sent.children) asked.set(li.dataset.reqId, li);

  reconcile(sent, state.crew.outgoing.map((req) => {
    const li = asked.get(String(req.id)) || buildSent(req);
    const meta = li.children[1];
    const declined = req.status === "declined";
    if (meta.classList.contains("is-blocked") !== declined) meta.classList.toggle("is-blocked", declined);
    setText(meta, declined ? retryLabel(req.retry_after) : "Waiting");
    return li;
  }));
}

// One request this account sent. Only its answer can change.
function buildSent(req) {
  const li = document.createElement("li");
  li.dataset.reqId = String(req.id);

  const name = document.createElement("span");
  name.className = "f-name";
  name.textContent = req.username;

  const meta = document.createElement("span");
  meta.className = "f-meta";

  li.append(name, meta);
  return li;
}

// retryLabel turns the server's timestamp into the only thing the sender
// actually needs: whether they can ask again yet.
function retryLabel(retryAfter) {
  if (!retryAfter) return "Declined";
  const until = new Date(retryAfter);
  if (Number.isNaN(until.getTime())) return "Declined";
  const days = Math.ceil((until - Date.now()) / 86400000);
  if (days <= 0) return "Declined · ask again";
  return `Declined · ${days} ${days === 1 ? "day" : "days"}`;
}

async function sendFriendRequest(event) {
  event.preventDefault();
  const input = $("friend-name");
  const username = input.value.trim();
  if (!username) {
    showError($("friend-error"), "Type a username first.");
    input.focus();
    return;
  }
  try {
    const res = await api("POST", "/api/friends/requests", { username });
    input.value = "";
    showError($("friend-error"), "");
    await loadFriends();
    toast(res.status === "friends"
      ? `${res.username} is on your list`
      : `Request sent to ${res.username}`);
  } catch (err) {
    showError($("friend-error"), err.message);
  }
}

async function removeFriend(f) {
  const ok = confirm(`Remove ${f.username} from your crew? They drop off your board.`);
  if (!ok) return;
  try {
    await api("DELETE", `/api/friends/${f.id}`);
    await loadFriends();
    toast(`${f.username} removed`);
  } catch (err) {
    showError($("friend-error"), err.message);
  }
}

/* ---------------------------------------------------------- realtime */

// The server sends invalidation hints, never data: "something you can see
// changed". The page answers by re-running the loaders it already has, so
// SQLite stays the only thing that knows anything. One stream per page, and
// EventSource owns its own reconnection.
const REFRESH_DEBOUNCE_MS = 250;
const FRESHNESS_MS = 60_000;
const RETURN_TO_TODAY_MS = 5 * 60_000;
let events = null;
let refreshTimer = 0;
let freshnessTimer = 0;
let inactiveAt = null;
let resetDayOnRefresh = false;
let refreshing = false;
let refreshPending = false;
// Set for as long as a commit is running its own loaders.
let committing = false;
// The loader batch a refresh has in flight, so a commit that starts while one
// is running can wait it out instead of racing it. It settles rather than
// rejects, so awaiting it cannot throw into the waiter.
let refreshRun = null;

function connectEvents() {
  if (events) return;
  events = new EventSource("/api/events");
  // Every open is a full refresh, not a resume: a reconnect may have missed
  // hints while it was down, and a restarted server remembers nothing.
  events.addEventListener("open", scheduleRefresh);
  events.addEventListener("invalidate", scheduleRefresh);
  events.addEventListener("error", () => {
    // A dropped connection is retried by EventSource itself. A rejected one —
    // a session that expired elsewhere — is not, so let go of the corpse and
    // let the next foreground try again.
    if (events && events.readyState === EventSource.CLOSED) disconnectEvents();
  });
}

function disconnectEvents() {
  if (events) events.close();
  events = null;
  clearTimeout(refreshTimer);
  refreshTimer = 0;
  clearTimeout(freshnessTimer);
}

// The server owns the calendar date (and its timezone). Follow today across
// midnight, while leaving deliberate history browsing alone during live use.
function adoptToday(today, reset = false) {
  const previous = state.today;
  const following = !state.selectedDay || state.selectedDay === previous;
  const oldMonth = previous.slice(0, 7);
  const viewingCurrent = `${state.cal.year}-${pad2(state.cal.month)}` === oldMonth;
  state.today = today;
  if (reset || following) state.selectedDay = today;
  if (reset || (today !== previous && viewingCurrent)) {
    const date = parseDay(today);
    state.cal = { year: date.getFullYear(), month: date.getMonth() + 1 };
  }
}

function armFreshness(rolloverMS = FRESHNESS_MS) {
  clearTimeout(freshnessTimer);
  // Use a server-relative delay so timezone, DST and a skewed device clock
  // cannot postpone midnight. The minute cap also repairs a silent SSE gap.
  const delay = Math.min(FRESHNESS_MS, Math.max(100, rolloverMS + 50));
  freshnessTimer = setTimeout(() => {
    if (!document.hidden && state.csrf) refreshAll();
  }, delay);
}

function markInactive() {
  if (inactiveAt === null) inactiveAt = Date.now();
}

function resumeApp() {
  if (document.hidden || !state.csrf) return;
  if (inactiveAt !== null && Date.now() - inactiveAt >= RETURN_TO_TODAY_MS) {
    resetDayOnRefresh = true;
  }
  inactiveAt = null;
  connectEvents();
  scheduleRefresh();
}

// A burst — a friend logging three sets in a row — is one refresh.
function scheduleRefresh() {
  clearTimeout(refreshTimer);
  refreshTimer = setTimeout(refreshAll, REFRESH_DEBOUNCE_MS);
}

// Re-runs the ordinary loaders for what is already on screen. It deliberately
// takes no focus and keeps drafts intact. Date synchronization precedes the
// loaders so the selected day, month, stats and board agree after midnight.
async function refreshAll() {
  if (!state.csrf) return;
  // A hint that lands while a refresh is in flight is remembered, not dropped:
  // dropping it left the page wrong until a reconnect, with nothing to correct
  // it. However many arrive, one follow-up refresh answers all of them.
  // A commit runs the same five loaders, and a refresh overlapping it takes the
  // generation race — dropping the commit's own responses, so the day cell it
  // just pulsed still reads the old total and this refresh pulses it again.
  // The commit is a refresh; wait for it rather than race it.
  if (refreshing || committing) { refreshPending = true; return; }

  refreshing = true;
  try {
    // Settled, not Promise.all: all() rejects on the first loader to fail,
    // which would start the follow-up while the other four are still in
    // flight, and a straggler landing after it overwrites newer state.
    refreshRun = (async () => {
      try {
        const session = await api("GET", "/api/session");
        if (!state.csrf) return;
        if (!session.authenticated) { disconnectEvents(); return; }
        adoptToday(session.today, resetDayOnRefresh);
        resetDayOnRefresh = false;
        armFreshness(session.day_rollover_ms);
        dropStreamIfSignedOut(await Promise.allSettled([
          loadCalendar(), loadDay(state.selectedDay), loadStats(), loadTypes(), loadFriends(),
        ]));
      } catch {
        // Offline: retain the visible data and retry, even without an SSE hint.
        if (state.csrf) armFreshness();
      }
    })();
    await refreshRun;
  } finally {
    refreshing = false;
    refreshRun = null;
    drainPending();
  }
}

// A loader answering 401 means the session ended somewhere else. The stream is
// authenticated only when it opens, so it would otherwise stay connected on a
// session that is already gone; let go of it and let the next sign-in reopen.
// Offline is a different thing entirely: the next hint or foreground retries.
function dropStreamIfSignedOut(settled) {
  if (settled.some((r) => r.status === "rejected" && r.reason && r.reason.status === 401)) {
    disconnectEvents();
  }
}

// Answers whatever arrived while a refresh or a commit had the loaders.
function drainPending() {
  if (!refreshPending) return;
  refreshPending = false;
  if (state.csrf && !document.hidden) refreshAll();
}

/* ---------------------------------------------------------- boot */

async function enterApp() {
  $("gate").hidden = true;
  $("app").hidden = false;
  $("who").textContent = state.username;

  const t = parseDay(state.today);
  state.cal = { year: t.getFullYear(), month: t.getMonth() + 1 };

  setView(location.hash === "#crew" ? "crew" : "log");
  await loadTypes();
  await Promise.all([loadCalendar(), loadDay(state.today), loadStats(), loadFriends()]);
  move(document.querySelector(".stats"), [
    { transform: "translateY(12px)", opacity: .2 },
    { transform: "none", opacity: 1 },
  ], 480);
  connectEvents();
  armFreshness();
  // Only when the bar is docked: on a phone the sheet is closed and focusing
  // it would throw up the soft keyboard over the calendar.
  if (docked() && window.matchMedia("(pointer: fine)").matches) {
    $("reps").focus({ preventScroll: true });
  }
}

function wire() {
  trackKeyboardInset();
  trackChromeHeight();
  reduceMotion.addEventListener("change", () => {
    if (!reduceMotion.matches) return;
    for (const animation of motions.values()) animation.cancel();
    motions.clear();
  });
  // Font loading, a request badge, and responsive layouts change tab widths.
  if (typeof ResizeObserver === "function") {
    const rails = new ResizeObserver(syncTabRails);
    document.querySelectorAll(".vtab, .ptab").forEach((tab) => rails.observe(tab));
  }
  for (const tab of document.querySelectorAll(".tab")) {
    tab.addEventListener("click", () => setAuthMode(tab.dataset.mode));
  }
  $("auth-form").addEventListener("submit", submitAuth);
  $("logout").addEventListener("click", logout);
  $("type-form").addEventListener("submit", createType);
  $("log").addEventListener("submit", submitLog);
  $("cal-prev").addEventListener("click", () => shiftMonth(-1));
  $("cal-next").addEventListener("click", () => shiftMonth(1));
  $("day-today").addEventListener("click", () => loadDay(state.today).catch((e) => toast(e.message)));

  wireTablist(".vtab", (tab) => setView(tab.dataset.view));
  wireTablist(".ptab", (tab) => {
    state.period = tab.dataset.period;
    for (const other of document.querySelectorAll(".ptab")) {
      const on = other === tab;
      other.classList.toggle("is-on", on);
      other.setAttribute("aria-selected", String(on));
      other.tabIndex = on ? 0 : -1;
    }
    moveTabRail(tab);
    renderBoard();
  });
  $("friend-form").addEventListener("submit", sendFriendRequest);
  $("friend-name").addEventListener("input", () => showError($("friend-error"), ""));
  // Coming back to the tab is the moment stale data is most obvious, and iOS
  // may have suspended the stream while the page was away.
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) {
      markInactive();
      clearTimeout(freshnessTimer);
    } else resumeApp();
  });
  window.addEventListener("blur", markInactive);
  window.addEventListener("focus", resumeApp);
  window.addEventListener("pageshow", resumeApp);
  window.addEventListener("online", resumeApp);

  $("fab").addEventListener("click", openSheet);
  $("log-close").addEventListener("click", () => closeSheet());
  // The scrim deliberately does NOT close the sheet: a stray tap while
  // choosing a variation must not throw away a half-entered set. The X and
  // Escape are the only ways out.
  $("scrim").addEventListener("click", (event) => event.stopPropagation());

  $("type-trigger").addEventListener("click", () => {
    if (pickerIsOpen()) closePicker();
    else openPicker();
  });
  $("type-trigger").addEventListener("keydown", pickerKeydown);
  $("picker-close").addEventListener("click", closePicker);
  // Tapping the picker's own backdrop only dismisses the picker.
  $("type-picker").addEventListener("click", (event) => {
    if (event.target === $("type-picker")) closePicker();
  });
  window.addEventListener("resize", () => {
    if (pickerIsOpen()) positionPicker();
  });

  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      // Innermost layer first: the picker, then the sheet.
      if (pickerIsOpen()) {
        event.preventDefault();
        closePicker();
        return;
      }
      if (sheetIsOpen()) {
        event.preventDefault();
        closeSheet();
        return;
      }
    }
    if (!pickerIsOpen()) trapFocus(event);
  });
  // Rotating into the docked layout must not leave a half-open sheet behind.
  window.matchMedia("(min-width: 48rem)").addEventListener("change", (e) => {
    if (!e.matches) return;
    closePicker();
    $("log").classList.remove("is-open");
    $("scrim").classList.remove("is-open");
    $("scrim").hidden = true;
    $("fab").classList.remove("is-hidden");
    $("fab").setAttribute("aria-expanded", "false");
    $("app").querySelector(".sheet").inert = false;
    document.querySelector(".chrome").inert = false;
    document.body.style.overflow = "";
  });

  const reps = $("reps");
  reps.addEventListener("focus", () => reps.select());
  reps.addEventListener("input", () => {
    const cleaned = reps.value.replace(/\D+/g, "").slice(0, 4);
    if (cleaned !== reps.value) reps.value = cleaned;
  });

  for (const key of document.querySelectorAll(".qk")) {
    // A button takes focus when tapped, which closes the on-screen keyboard
    // and reflows the sheet out from under the user's thumb mid-tap. Cancelling
    // the pointer/mouse press keeps focus on the number field, so the keyboard
    // stays up and the keys hold still for repeated taps. The click still
    // fires, so keyboard activation (Enter/Space) is unaffected.
    const holdFocus = (event) => event.preventDefault();
    key.addEventListener("pointerdown", (event) => {
      holdFocus(event);
      // Cancelling the press also suppresses :active, so drive the pressed
      // state directly; repeated tapping needs the feedback.
      key.classList.add("is-press");
    });
    key.addEventListener("mousedown", holdFocus);
    for (const done of ["pointerup", "pointercancel", "pointerleave"]) {
      key.addEventListener(done, () => key.classList.remove("is-press"));
    }

    key.addEventListener("click", () => {
      buzz(12);
      if (key.dataset.clear) {
        reps.value = "";
      } else {
        const add = parseInt(key.dataset.add, 10);
        const current = parseInt(reps.value, 10);
        reps.value = String(Math.min(9999, (Number.isFinite(current) ? current : 0) + add));
      }
      // Leave the new total selected so the next keystroke replaces it outright
      // instead of appending to it.
      keepEditing(reps);
    });
  }
}

// Returns focus to the field if something took it anyway, then selects the
// whole value. Called from a click handler, so it counts as user activation
// and the keyboard is allowed to reopen.
function keepEditing(input) {
  if (document.activeElement !== input) input.focus({ preventScroll: true });
  input.select();
}

async function boot() {
  wire();
  try {
    const res = await api("GET", "/api/session");
    state.today = res.today;
    if (res.authenticated) {
      state.csrf = res.csrf_token;
      state.username = res.username;
      await enterApp();
    } else {
      $("gate").hidden = false;
      setAuthMode("login");
    }
  } catch {
    $("gate").hidden = false;
    setAuthMode("login");
    showError($("auth-error"), "Cannot reach the server. Check that it is running, then reload.");
  }
}

if ("serviceWorker" in navigator) {
  // The shell is served cache-first, so a new build only reaches an open tab
  // if the waiting worker is told to take over. This app holds no unsaved
  // state, so it activates immediately and reloads once.
  let reloading = false;
  navigator.serviceWorker.addEventListener("controllerchange", () => {
    if (reloading) return;
    reloading = true;
    location.reload();
  });

  const takeOver = (worker) => {
    if (worker && navigator.serviceWorker.controller) worker.postMessage({ type: "SKIP_WAITING" });
  };

  window.addEventListener("load", () => {
    navigator.serviceWorker
      .register("/sw.js", { scope: "/", updateViaCache: "none" })
      .then((reg) => {
        takeOver(reg.waiting);
        reg.addEventListener("updatefound", () => {
          const installing = reg.installing;
          if (!installing) return;
          installing.addEventListener("statechange", () => {
            if (installing.state === "installed") takeOver(reg.waiting || installing);
          });
        });
      })
      .catch(() => {
        // A service worker needs a secure context; plain LAN HTTP degrades to
        // an ordinary web app, which is fine.
      });
  });
}

boot();
