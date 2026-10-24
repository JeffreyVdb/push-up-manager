/* Shared overlay behavior for the mobile logging form and responsive dialogs.
   Native dialogs own modality; the dockable form uses its existing scrim and
   background regions. Callers only open/close and supply their initial focus. */
"use strict";
window.PushupSheet = (() => {
  let locks = 0;
  let previousOverflow = "";
  return {
    create(panel, { scrim = null, background = [], dismissOnBackdrop = false } = {}) {
      const native = panel.tagName === "DIALOG";
      let opened = false;
      let returnFocus = null;
      let restoreFocus = true;
      let previousInert = [];
      function finish() {
        if (!opened) return;
        opened = false;
        panel.classList.remove("is-open");
        if (scrim) {
          scrim.classList.remove("is-open");
          setTimeout(() => { if (!opened) scrim.hidden = true; }, 340);
        }
        previousInert.forEach(([element, inert]) => { element.inert = inert; });
        previousInert = [];
        if (!native) { panel.removeAttribute("role"); panel.removeAttribute("aria-modal"); }
        if (--locks === 0) document.body.style.overflow = previousOverflow;
        if (restoreFocus && returnFocus?.isConnected) returnFocus.focus({ preventScroll: true });
      }
      function close({ restoreFocus: restore = true, value = "cancel" } = {}) {
        if (!opened) return;
        restoreFocus = restore;
        if (native) panel.close(value);
        else { finish(); panel.dispatchEvent(new Event("close")); }
      }
      if (native) panel.addEventListener("close", finish);
      panel.addEventListener("keydown", (event) => {
        if (native || !opened) return;
        if (event.key === "Escape") {
          event.preventDefault();
          event.stopPropagation();
          if (panel.dispatchEvent(new Event("cancel", { cancelable: true }))) close();
        } else if (event.key === "Tab") {
          const items = [...panel.querySelectorAll('button:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])')].filter(el => el.getClientRects().length);
          const first = items[0], last = items[items.length - 1];
          if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
          else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
        }
      });
      const backdrop = scrim || panel;
      backdrop.addEventListener("click", (event) => {
        if (!dismissOnBackdrop || event.target !== backdrop) return;
        const rect = panel.getBoundingClientRect();
        if (scrim || event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) close();
      });
      return {
        get isOpen() { return opened; },
        open({ focus = null, returnTo = document.activeElement } = {}) {
          if (opened) return;
          returnFocus = returnTo;
          restoreFocus = true;
          if (native) panel.showModal();
          opened = true;
          if (locks++ === 0) { previousOverflow = document.body.style.overflow; document.body.style.overflow = "hidden"; }
          if (!native) {
            panel.setAttribute("role", "dialog");
            panel.setAttribute("aria-modal", "true");
            previousInert = background.map(el => [el, el.inert]);
            background.forEach(el => { el.inert = true; });
          }
          panel.classList.add("is-open");
          if (scrim) {
            scrim.hidden = false;
            void scrim.offsetWidth;
            scrim.classList.add("is-open");
          }
          focus?.focus({ preventScroll: true });
        },
        close,
      };
    },
  };
})();
