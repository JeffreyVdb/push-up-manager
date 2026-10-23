/* Optional account recovery. Tokens stay in memory, never in browser storage. */
"use strict";
window.AppAccount = (() => {
  let account = null;
  let generation = 0;
  let recoveryMode = "";
  let recoveryToken = "";
  let busy = false;
  const el = (id) => document.getElementById(id);
  const message = (id, value) => {
    el(id).textContent = value;
    el(id).hidden = !value;
  };

  function render() {
    if (!account) return;
    const pending = account.pending_email;
    let status = account.email_verified ? `Verified: ${account.email}. You can reset your password by email.` : "No verified email. Add one to recover your account if you forget your password.";
    if (pending) status += ` Check ${pending} for a confirmation link.`;
    if (!account.recovery_available) status += " Email delivery is not configured on this server yet.";
    message("account-email-status", status);
    if (!el("account-email-form").contains(document.activeElement)) {
      el("account-email").value = pending || account.email || "";
    }
    el("account-email-resend").hidden = !pending;
    el("account-email-remove").hidden = !pending && !account.email;
    el("email-prompt").hidden = !!(account.email || pending || account.email_prompt_dismissed);
  }

  async function refresh() {
    const requestGeneration = generation;
    try {
      const result = await api("GET", "/api/account");
      if (requestGeneration !== generation || !state.csrf) return;
      account = result;
      render();
    } catch (err) {
      if (requestGeneration !== generation) return;
      message("account-email-status", "Connect to the server to manage your recovery email.");
    }
  }

  async function changeEmail(action) {
    if (busy) return;
    const password = el("account-password").value;
    if (!password) {
      message("account-error", "Enter your current password to change your recovery email.");
      el("account-password").focus();
      return;
    }
    if (action === "remove" && !window.confirm("Remove your recovery email? You will not be able to reset your password by email.")) return;
    busy = true;
    const controls = [...el("account-email-form").querySelectorAll("button")];
    controls.forEach(button => { button.disabled = true; });
    message("account-error", "");
    message("account-status", "");
    try {
      if (action === "save") {
        await api("POST", "/api/account/email", { email: el("account-email").value.trim(), password });
      } else if (action === "resend") {
        await api("POST", "/api/account/email/resend", { password });
      } else {
        await api("DELETE", "/api/account/email", { password });
      }
      el("account-password").value = "";
      message("account-status", action === "remove" ? "Recovery email removed." : "Confirmation sent. Open the link in your email to verify the address.");
      await refresh();
    } catch (err) {
      message("account-error", err.message);
      await refresh();
    } finally {
      busy = false;
      controls.forEach(button => { button.disabled = false; });
    }
  }

  async function dismissPrompt() {
    const button = el("email-prompt-dismiss");
    button.disabled = true;
    message("email-prompt-error", "");
    try {
      await api("POST", "/api/account/email-prompt/dismiss", {});
      if (account) account.email_prompt_dismissed = true;
      el("email-prompt").hidden = true;
    } catch (err) {
      message("email-prompt-error", "Connect to the server to save your dismissal. Then we won’t ask again.");
    } finally { button.disabled = false; }
  }

  async function submitRecovery(event) {
    event?.preventDefault();
    if (busy) return;
    message("recovery-error", "");
    message("recovery-status", "");
    if (recoveryMode === "reset-password" && el("recovery-password").value !== el("recovery-repeat").value) {
      message("recovery-error", "The passwords do not match. Enter the same new password in both fields.");
      el("recovery-repeat").focus();
      return;
    }
    const button = el("recovery-submit");
    const label = button.textContent;
    button.disabled = true;
    button.textContent = "PLEASE WAIT…";
    busy = true;
    try {
      if (recoveryMode === "forgot-password") {
        await api("POST", "/api/forgot-password", { email: el("recovery-email").value.trim() });
        message("recovery-status", "If this address is verified on an account, a reset link is on its way. Check your inbox and spam folder.");
      } else if (recoveryMode === "reset-password") {
        await api("POST", "/api/reset-password", { token: recoveryToken, password: el("recovery-password").value });
        recoveryToken = "";
        el("recovery-password").value = "";
        el("recovery-repeat").value = "";
        el("recovery-password-field").hidden = true;
        el("recovery-repeat-field").hidden = true;
        button.hidden = true;
        message("recovery-status", "Password changed. Sign in with your new password. Your other sessions have been signed out.");
      } else {
        await api("POST", "/api/account/email/confirm", { token: recoveryToken });
        recoveryToken = "";
        button.hidden = true;
        message("recovery-status", "Email confirmed. You can now use this address to reset your password.");
      }
    } catch (err) {
      message("recovery-error", err.message);
    } finally {
      busy = false;
      button.disabled = false;
      button.textContent = label;
    }
  }

  function boot() {
    const route = location.pathname.replace(/\/$/, "").slice(1);
    if (!["forgot-password", "reset-password", "confirm-email"].includes(route)) return false;
    recoveryMode = route;
    recoveryToken = new URLSearchParams(location.hash.slice(1)).get("token") || "";
    // Remove the bearer token from the visible address and browser history.
    if (location.hash) history.replaceState(null, "", location.pathname);
    el("gate").hidden = true;
    el("app").hidden = true;
    el("recovery").hidden = false;
    el("recovery-email-field").hidden = route !== "forgot-password";
    el("recovery-email").required = route === "forgot-password";
    const resetting = route === "reset-password";
    el("recovery-password-field").hidden = !resetting;
    el("recovery-repeat-field").hidden = !resetting;
    el("recovery-password").required = resetting;
    el("recovery-repeat").required = resetting;
    const confirming = route === "confirm-email";
    el("recovery-title").textContent = confirming ? "Confirm your email" : "Reset your password";
    el("recovery-intro").textContent = confirming ? "Verify your recovery address." : resetting ? "Choose a new password for your account." : "Enter the email address you verified in Settings. We’ll send you a link to choose a new password.";
    el("recovery-submit").textContent = confirming ? "CONFIRM EMAIL" : resetting ? "SET NEW PASSWORD" : "SEND RESET LINK";
    if (route !== "forgot-password" && !recoveryToken) {
      message("recovery-error", "This link is missing its token. Open the complete link from your email, or request a new one.");
      el("recovery-submit").hidden = true;
    } else if (confirming) {
      submitRecovery();
    }
    return true;
  }

  function wire() {
    window.Passkeys?.mount();
    el("account-email-form").addEventListener("submit", event => { event.preventDefault(); changeEmail("save"); });
    el("account-email-resend").addEventListener("click", () => changeEmail("resend"));
    el("account-email-remove").addEventListener("click", () => changeEmail("remove"));
    el("email-prompt-dismiss").addEventListener("click", dismissPrompt);
    el("email-prompt-settings").addEventListener("click", () => {
      el("settings").showModal();
      el("account-email").focus();
    });
    el("recovery-form").addEventListener("submit", submitRecovery);
    for (const button of document.querySelectorAll("[data-open-settings]")) {
      button.addEventListener("click", () => {
        if (!state.csrf) return;
        refresh();
        window.Passkeys?.refresh();
      });
    }
    el("settings").addEventListener("close", () => {
      el("account-password").value = "";
      el("account-passkeys").querySelectorAll('input[type="password"]').forEach(input => { input.value = ""; });
    });
  }

  function enter() {
    generation++;
    account = null;
    window.Passkeys?.clear();
    el("account-settings").hidden = false;
    refresh();
    window.Passkeys?.refresh();
  }

  function leave() {
    generation++;
    account = null;
    window.Passkeys?.clear();
    el("account-settings").hidden = true;
    el("email-prompt").hidden = true;
    el("account-email-form").reset();
    message("account-error", "");
    message("account-status", "");
  }

  async function signedIn(res) {
    await adoptSession(res);
    await enterApp();
  }
  return { wire, boot, enter, leave, refresh, signedIn };
})();
