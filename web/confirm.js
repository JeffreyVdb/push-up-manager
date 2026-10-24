/* One explicit confirmation at a time. Native dialog supplies focus containment,
   Escape cancellation and focus return; CSS makes it a bottom sheet on phones. */
"use strict";
window.PushupConfirm = (() => {
  const dialog = document.getElementById("confirm-action");
  const sheet = window.PushupSheet.create(dialog, { dismissOnBackdrop: true });
  let pending = null;
  dialog.addEventListener("close", () => {
    const resolve = pending;
    pending = null;
    const confirmed = dialog.returnValue === "confirm";
    dialog.returnValue = "";
    resolve?.(confirmed);
  });
  return {
    ask({ title, message, action }) {
      // Do not replace a question somebody is already answering.
      if (pending) return Promise.resolve(false);
      document.getElementById("confirm-title").textContent = title;
      document.getElementById("confirm-message").textContent = message;
      document.getElementById("confirm-accept").textContent = action;
      dialog.returnValue = "";
      return new Promise((resolve) => {
        pending = resolve;
        sheet.open({ focus: document.getElementById("confirm-cancel") });
      });
    },
    cancel() { sheet.close(); },
  };
})();
