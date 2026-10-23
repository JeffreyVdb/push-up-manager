/* Passkeys stay on the server; this module only transports WebAuthn ceremonies. */
(() => {
  "use strict";

  const supported = () => window.isSecureContext && !!window.PublicKeyCredential && !!navigator.credentials;
  const byID = id => document.getElementById(id);
  let mounted = false;
  let busy = false;
  let available = false;
  let accountGeneration = 0;
  let refreshGeneration = 0;
  let ceremonyController = null;

  function bytes(value) {
    const raw = atob(value.replace(/-/g, "+").replace(/_/g, "/"));
    return Uint8Array.from(raw, char => char.charCodeAt(0));
  }

  function base64(value) {
    let raw = "";
    for (const byte of new Uint8Array(value)) raw += String.fromCharCode(byte);
    return btoa(raw).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  }

  function optionsFromJSON(options, create) {
    const method = create ? "parseCreationOptionsFromJSON" : "parseRequestOptionsFromJSON";
    if (typeof PublicKeyCredential[method] === "function") return PublicKeyCredential[method](options);
    const result = { ...options, challenge: bytes(options.challenge) };
    if (create) result.user = { ...options.user, id: bytes(options.user.id) };
    for (const name of ["allowCredentials", "excludeCredentials"]) {
      if (options[name]) result[name] = options[name].map(credential => ({ ...credential, id: bytes(credential.id) }));
    }
    return result;
  }

  function credentialJSON(credential) {
    if (typeof credential.toJSON === "function") return credential.toJSON();
    const response = {};
    for (const field of ["clientDataJSON", "attestationObject", "authenticatorData", "signature", "userHandle"]) {
      if (credential.response[field] != null) response[field] = base64(credential.response[field]);
    }
    if (credential.response.getTransports) response.transports = credential.response.getTransports();
    return {
      id: credential.id, rawId: base64(credential.rawId), type: credential.type,
      authenticatorAttachment: credential.authenticatorAttachment,
      clientExtensionResults: credential.getClientExtensionResults(), response,
    };
  }

  function message(text, error = false) {
    const target = byID(error ? "passkey-error" : "passkey-status");
    const other = byID(error ? "passkey-status" : "passkey-error");
    if (other) { other.textContent = ""; other.hidden = true; }
    if (target) { target.textContent = text; target.hidden = !text; }
  }

  function errorText(error) {
    if (error.name === "NotAllowedError" || error.name === "AbortError") return "Passkey request cancelled or timed out. Try again when ready.";
    if (error.name === "InvalidStateError") return "That passkey is already registered. Choose another device or authenticator.";
    return error.message || "Passkey request failed. Try again.";
  }

  function setBusy(value) {
    busy = value;
    const root = byID("account-passkeys");
    if (root) {
      root.setAttribute("aria-busy", String(value));
      root.querySelectorAll("button").forEach(button => { button.disabled = value || !available; });
    }
  }

  function mount() {
    const root = byID("account-passkeys");
    if (!root || mounted) return;
    mounted = true;
    root.innerHTML = `
      <h3 class="band-title">Passkeys</h3>
      <p class="hint">Use your fingerprint, face or device PIN to sign in. Keep your password for account changes.</p>
      <p id="passkey-availability" class="hint"></p>
      <ul id="passkey-list" class="passkey-list" aria-label="Your passkeys"></ul>
      <form id="passkey-form" class="stack">
        <label class="field"><span class="field-label">Passkey name</span><input id="passkey-name" name="name" type="text" maxlength="80" autocomplete="off" placeholder="e.g. My phone"></label>
        <label class="field"><span class="field-label">Current password</span><input id="passkey-password" name="current-password" type="password" autocomplete="current-password" maxlength="256" required aria-describedby="passkey-password-hint"></label>
        <p id="passkey-password-hint" class="hint">Enter your password to add or remove a passkey.</p>
        <div class="account-actions"><button class="btn btn-outline" type="submit">ADD PASSKEY</button></div>
      </form>
      <p id="passkey-error" class="alert" role="alert" hidden></p>
      <p id="passkey-status" class="hint" role="status" hidden></p>`;
    byID("passkey-form").addEventListener("submit", async event => {
      event.preventDefault();
      if (busy || !available) return;
      const generation = accountGeneration;
      ceremonyController = new AbortController();
      const controller = ceremonyController;
      setBusy(true); message("Waiting for your device…");
      const password = byID("passkey-password").value;
      byID("passkey-password").value = "";
      try {
        const options = await api("POST", "/api/passkeys/register/begin", { password, name: byID("passkey-name").value.trim() });
        if (generation !== accountGeneration) return;
        const credential = await navigator.credentials.create({ publicKey: optionsFromJSON(options.publicKey, true), signal: controller.signal });
        if (generation !== accountGeneration) return;
        if (!credential) throw new DOMException("Cancelled", "NotAllowedError");
        await api("POST", "/api/passkeys/register/finish", credentialJSON(credential));
        if (generation !== accountGeneration) return;
        byID("passkey-name").value = "";
        await refresh();
        if (generation === accountGeneration) message("Passkey added. You can use it next time you sign in.");
      } catch (error) { if (generation === accountGeneration) message(errorText(error), true); }
      finally { if (generation === accountGeneration) { ceremonyController = null; setBusy(false); } }
    });
  }

  async function remove(key) {
    if (busy || !available) return;
    const input = byID("passkey-password");
    if (!input.value) { message("Enter your current password before removing a passkey.", true); input.focus(); return; }
    if (!window.confirm(`Remove “${key.name}”? You will no longer be able to sign in with this passkey.`)) return;
    const password = input.value; input.value = "";
    const generation = accountGeneration;
    setBusy(true);
    try {
      await api("DELETE", `/api/passkeys/${encodeURIComponent(key.id)}`, { password });
      if (generation !== accountGeneration) return;
      await refresh();
      if (generation === accountGeneration) message("Passkey removed.");
    } catch (error) { if (generation === accountGeneration) message(errorText(error), true); }
    finally { if (generation === accountGeneration) setBusy(false); }
  }

  async function refresh() {
    mount();
    if (!byID("account-passkeys")) return;
    const generation = ++refreshGeneration;
    try {
      const data = await api("GET", "/api/passkeys");
      if (generation !== refreshGeneration) return;
      available = supported() && data.available && navigator.onLine !== false;
      byID("passkey-availability").textContent = !supported() ? "Passkeys need a supported browser and HTTPS (or localhost)." : !data.available ? "Passkeys need an HTTPS public address configured on the server." : navigator.onLine === false ? "Connect to the server to manage passkeys." : "";
      const list = byID("passkey-list"); list.replaceChildren();
      for (const key of data.passkeys) {
        const row = document.createElement("li");
        const name = document.createElement("span"); name.textContent = key.name;
        const removeButton = document.createElement("button"); removeButton.type = "button"; removeButton.className = "btn btn-ghost"; removeButton.textContent = "REMOVE";
        removeButton.setAttribute("aria-label", `Remove passkey ${key.name}`);
        removeButton.addEventListener("click", () => remove(key)); row.append(name, removeButton); list.append(row);
      }
      if (!data.passkeys.length) { const row = document.createElement("li"); row.className = "hint"; row.textContent = "No passkeys yet."; list.append(row); }
      setBusy(busy);
    } catch (error) {
      if (generation !== refreshGeneration) return;
      available = false; setBusy(false); message(errorText(error), true);
    }
  }

  function clear() {
    accountGeneration++;
    refreshGeneration++;
    ceremonyController?.abort();
    ceremonyController = null;
    available = false;
    if (!mounted) return;
    byID("passkey-list").replaceChildren();
    byID("passkey-name").value = "";
    byID("passkey-password").value = "";
    byID("passkey-availability").textContent = "";
    message("");
    setBusy(false);
  }

  async function signIn() {
    if (!supported()) throw new Error("Passkeys need a supported browser and HTTPS (or localhost).");
    const options = await api("POST", "/api/passkeys/login/begin", {});
    const credential = await navigator.credentials.get({ publicKey: optionsFromJSON(options.publicKey, false) });
    if (!credential) throw new DOMException("Cancelled", "NotAllowedError");
    return api("POST", "/api/passkeys/login/finish", credentialJSON(credential));
  }

  function bind() {
    mount();
    const button = byID("passkey-login");
    if (!button) return;
    button.hidden = !supported();
    button.addEventListener("click", async () => {
      button.disabled = true;
      const errorNode = byID("auth-error"); if (errorNode) { errorNode.textContent = ""; errorNode.hidden = true; }
      try { const data = await signIn(); await window.AppAccount.signedIn(data); }
      catch (error) { if (errorNode) { errorNode.textContent = errorText(error); errorNode.hidden = false; } }
      finally { button.disabled = false; }
    });
  }

  window.Passkeys = { mount, refresh, clear, signIn };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", bind, { once: true }); else bind();
})();
