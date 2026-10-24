/* Apply appearance before styles load, including on the sign-in screen. */
"use strict";
(() => {
  const themeKey = "pushup-counter:theme";
  const modeKey = "pushup-counter:mode";
  const normalizeTheme = (value) => value === "classic" ? "classic" : "plant";
  const normalizeMode = (value) => ["light", "dark"].includes(value) ? value : "system";
  const system = window.matchMedia("(prefers-color-scheme: dark)");
  function read(key) {
    try { return localStorage.getItem(key); } catch { return null; }
  }
  let theme = normalizeTheme(read(themeKey));
  let preference = normalizeMode(read(modeKey));
  function apply() {
    const mode = preference === "system" ? (system.matches ? "dark" : "light") : preference;
    document.documentElement.dataset.theme = theme;
    document.documentElement.dataset.mode = mode;
    document.querySelector('meta[name="color-scheme"]').content = mode;
    const colors = { plant: { light: "#edf3e7", dark: "#111b15" }, classic: { light: "#f3f1ea", dark: "#070807" } };
    document.querySelector('meta[name="theme-color"]').content = colors[theme][mode];
    const icon = document.querySelector('link[rel="icon"]');
    if (icon) icon.href = theme === "classic" ? "/icons/icon-192.png" : "/icons/plant-icon-192.png";
    const toggle = document.getElementById("classic-theme");
    if (toggle) toggle.checked = theme === "classic";
    const select = document.getElementById("color-mode");
    if (select) select.value = preference;
    const note = document.getElementById("mode-note");
    if (note) note.textContent = preference === "system"
      ? `Following your device’s appearance. Currently ${mode}.`
      : `Always ${mode}, regardless of your device’s appearance.`;
  }
  function save(key, value) {
    apply();
    try { localStorage.setItem(key, value); return true; } catch { return false; }
  }
  apply();
  window.PushupTheme = {
    set(value) { theme = normalizeTheme(value); return save(themeKey, theme); },
    setMode(value) { preference = normalizeMode(value); return save(modeKey, preference); },
  };
  window.addEventListener("storage", (event) => {
    if (event.key === themeKey) theme = normalizeTheme(event.newValue);
    else if (event.key === modeKey) preference = normalizeMode(event.newValue);
    else if (event.key === null) { theme = "plant"; preference = "system"; }
    else return;
    apply();
  });
  system.addEventListener("change", apply);
  // Controls and icon links follow this blocking script in the document.
  document.addEventListener("DOMContentLoaded", apply);
})();
