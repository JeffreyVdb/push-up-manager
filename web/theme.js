/* Apply the saved theme before styles load, including on the sign-in screen. */
"use strict";
(() => {
  const key = "pushup-counter:theme";
  const normalize = (value) => value === "classic" ? "classic" : "plant";
  function apply(value) {
    const theme = normalize(value);
    document.documentElement.dataset.theme = theme;
    document.querySelector('meta[name="color-scheme"]').content = theme === "classic" ? "dark" : "light";
    document.querySelector('meta[name="theme-color"]').content = theme === "classic" ? "#070807" : "#edf3e7";
    const icon = document.querySelector('link[rel="icon"]');
    if (icon) icon.href = theme === "classic" ? "/icons/icon-192.png" : "/icons/plant-icon-192.png";
    const toggle = document.getElementById("classic-theme");
    if (toggle) toggle.checked = theme === "classic";
    return theme;
  }
  let saved;
  try { saved = localStorage.getItem(key); } catch { /* Storage may be disabled. */ }
  apply(saved);
  window.PushupTheme = {
    set(value) {
      const theme = apply(value);
      try { localStorage.setItem(key, theme); return true; } catch { return false; }
    },
  };
  window.addEventListener("storage", (event) => {
    if (event.key === key || event.key === null) apply(event.newValue);
  });
  // Icon links follow this blocking script in the head.
  document.addEventListener("DOMContentLoaded", () => apply(document.documentElement.dataset.theme));
})();
