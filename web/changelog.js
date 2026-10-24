/* Notes are embedded in this page, so they always describe the running build. */
"use strict";
(() => {
  const notes = document.getElementById("whats-new");
  const latest = notes.querySelector("[data-release]")?.dataset.release;
  if (!latest) return;
  const key = "pushup-counter:release-read";
  let seen;
  try { seen = localStorage.getItem(key); } catch { /* Works for this visit. */ }
  function update() {
    const unread = seen !== latest;
    document.getElementById("release-unread").hidden = !unread;
    for (const button of document.querySelectorAll("[data-open-settings]")) {
      button.toggleAttribute("data-unread", unread);
      button.setAttribute("aria-label", unread ? "Settings — new update notes" : "Settings");
    }
  }
  notes.addEventListener("toggle", () => {
    if (!notes.open) return;
    seen = latest;
    try { localStorage.setItem(key, latest); } catch { /* Do not block reading. */ }
    update();
  });
  window.addEventListener("storage", (event) => {
    if (event.key !== key && event.key !== null) return;
    seen = event.newValue;
    update();
  });
  update();
})();
