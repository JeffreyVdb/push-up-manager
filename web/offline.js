/* Account-scoped offline data and a durable, idempotent rep outbox. */
"use strict";
(function (root) {
  const copy = (value) => value === undefined ? undefined : JSON.parse(JSON.stringify(value));
  const failure = (message, status) => Object.assign(new Error(message), { status });
  const storageError = () => failure("This browser cannot save sets on this device. Free some storage or allow site storage, then try again. Your set has not been cleared.");
  const addDays = (day, count) => {
    const date = new Date(`${day}T12:00:00Z`);
    date.setUTCDate(date.getUTCDate() + count);
    return date.toISOString().slice(0, 10);
  };

  class BrowserStore {
    async open() {
      if (this.db) return this.db;
      if (!root.indexedDB) throw storageError();
      this.db = await new Promise((resolve, reject) => {
        const request = root.indexedDB.open("pushup-offline", 1);
        request.onupgradeneeded = () => {
          for (const name of ["meta", "accounts", "outbox"]) request.result.createObjectStore(name);
        };
        request.onsuccess = () => resolve(request.result);
        request.onerror = request.onblocked = () => reject(storageError());
      });
      this.db.onversionchange = () => { this.db.close(); this.db = null; };
      return this.db;
    }
    async transaction(names, mode, action) {
      const db = await this.open();
      return new Promise((resolve, reject) => {
        let result;
        const tx = db.transaction(names, mode);
        tx.oncomplete = () => resolve(result);
        tx.onerror = tx.onabort = () => reject(storageError());
        try { action(tx, (value) => { result = value; }); }
        catch { tx.abort(); }
      });
    }
    get(name, key) {
      return this.transaction([name], "readonly", (tx, done) => {
        tx.objectStore(name).get(key).onsuccess = (event) => done(event.target.result);
      });
    }
    put(name, key, value) {
      return this.transaction([name], "readwrite", (tx) => tx.objectStore(name).put(value, key));
    }
    remove(name, key) {
      return this.transaction([name], "readwrite", (tx) => tx.objectStore(name).delete(key));
    }
    all(name) {
      return this.transaction([name], "readonly", (tx, done) => {
        tx.objectStore(name).getAll().onsuccess = (event) => done(event.target.result);
      });
    }
    // Merge acknowledgement and delete the outbox item atomically. A crash or
    // quota failure cannot drop the sole durable copy of a set.
    acknowledge(userID, key, entry) {
      return this.transaction(["accounts", "outbox"], "readwrite", (tx) => {
        tx.objectStore("accounts").get(userID).onsuccess = (event) => {
          const account = event.target.result;
          if (!account?.snapshot) { tx.abort(); return; }
          if (!account.snapshot.entries.some((e) => e.id === entry.id || e.client_id === key)) account.snapshot.entries.unshift(entry);
          tx.objectStore("accounts").put(account, userID);
          tx.objectStore("outbox").delete(key);
        };
      });
    }
  }

  class OfflineLog {
    constructor({ request, changed = () => {}, sessionChanged = () => {}, broadcast = () => {}, storage = new BrowserStore(), uuid } = {}) {
      this.request = request;
      this.changed = changed;
      this.broadcast = broadcast;
      this.sessionChanged = sessionChanged;
      this.storage = storage;
      this.uuid = uuid || (() => {
        if (root.crypto?.randomUUID) return root.crypto.randomUUID();
        const bytes = new Uint8Array(16);
        root.crypto.getRandomValues(bytes);
        return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("");
      });
      this.userID = 0;
      this.online = true;
      this.needsAuth = false;
      this.syncing = null;
      this.problem = "";
    }
    async active() { return this.storage.get("meta", "active"); }
    async account() { return this.storage.get("accounts", this.userID); }
    async pending() { return (await this.storage.all("outbox")).filter((e) => e.user_id === this.userID); }
    async accept(session) {
      if (!session.authenticated || !session.user_id) throw failure("Sign in again to prepare offline logging.");
      const oldActive = await this.active();
      this.userID = session.user_id;
      this.needsAuth = false;
      const previous = await this.account() || {};
      await this.storage.put("accounts", this.userID, { ...previous, session, clock: Date.now() });
      await this.storage.put("meta", "active", this.userID);
      this.sessionChanged(session);
      if (oldActive !== this.userID) this.broadcast("account");
    }
    async restore() {
      const id = await this.active();
      if (!id) return null;
      this.userID = id;
      const account = await this.account();
      if (!account?.snapshot) return null;
      // Restoring data says nothing about reachability. Only a browser offline
      // signal or a failed request establishes that; null means not checked.
      this.online = root.navigator?.onLine === false ? false : null;
      const session = { ...this.localSession(account), cached: true };
      this.sessionChanged(session);
      return session;
    }
    async leave() {
      // A tombstone also prevents an unexpired HttpOnly cookie automatically
      // undoing an offline sign-out when the network returns.
      await this.storage.put("meta", "active", 0);
      this.userID = 0;
      this.needsAuth = false;
      this.broadcast("account");
    }
    localSession(account) {
      const session = copy(account.session);
      // Advance at the last known server midnight. Subsequent offline days
      // are estimates; queued sets always retain the explicit selected date.
      const elapsed = Math.max(0, Date.now() - account.clock);
      const rollover = session.day_rollover_ms || 86400000;
      if (elapsed >= rollover) session.today = addDays(session.today, 1 + Math.floor((elapsed - rollover) / 86400000));
      session.offline = this.online === false;
      return session;
    }
    async session() {
      const account = await this.account();
      return account ? this.localSession(account) : null;
    }
    async enqueue(body, typeName) {
      const account = await this.account();
      if (!account?.snapshot || !this.userID) throw failure("Connect once before logging offline on this device.");
      const id = this.uuid();
      const entry = { ...body, id: `pending-${id}`, client_id: id, user_id: this.userID, type_name: typeName, created_at: new Date().toISOString(), pending: true };
      await this.storage.put("outbox", id, entry);
      this.broadcast("queue");
      this.changed();
      return entry;
    }
    async discard(id) {
      const entry = await this.storage.get("outbox", id);
      if (entry?.user_id === this.userID) await this.storage.remove("outbox", id);
      this.broadcast("queue");
      this.changed();
    }
    async reassign(id, typeID, typeName) {
      const entry = await this.storage.get("outbox", id);
      if (!entry || entry.user_id !== this.userID || entry.error_status !== 404) return;
      // A 404 guarantees this operation did not commit. Keep its original
      // date/count while making the user's chosen variation explicit.
      await this.storage.put("outbox", id, { ...entry, type_id: typeID, type_name: typeName, error: "", error_status: 0 });
      this.broadcast("queue");
      this.changed();
    }
    async changeDay(id, day) {
      const entry = await this.storage.get("outbox", id);
      if (!entry || entry.user_id !== this.userID || entry.error_status !== 400) return;
      await this.storage.put("outbox", id, { ...entry, day, error: "", error_status: 0 });
      this.broadcast("queue");
      this.changed();
    }
    async refresh() {
      if (!this.userID) return;
      if (this.syncing) return this.syncing;
      const userID = this.userID;
      this.syncing = this.refreshAccount(userID).finally(() => { this.syncing = null; this.changed(); });
      this.changed();
      return this.syncing;
    }
    async refreshAccount(userID) {
      try {
        if (await this.active() !== userID) throw failure("This account was signed out in another tab. Sign in to sync its saved sets.", 401);
        const session = await this.request("GET", "/api/session");
        if (!session.authenticated || session.user_id !== userID) throw failure("Sign in to this account again to sync its saved sets.", 401);
        if (this.userID !== userID || await this.active() !== userID) return;
        const previous = await this.storage.get("accounts", userID) || {};
        await this.storage.put("accounts", userID, { ...previous, session, clock: Date.now() });
        if (this.userID !== userID || await this.active() !== userID) return;
        this.sessionChanged(session);
        for (const queued of await this.pending()) {
          if (queued.error) continue;
          if (this.userID !== userID || await this.active() !== userID) return;
          try {
            const entry = await this.request("POST", "/api/reps", { user_id: userID, client_id: queued.client_id, type_id: queued.type_id, count: queued.count, day: queued.day });
            await this.storage.acknowledge(userID, queued.client_id, entry);
            this.broadcast("queue");
          } catch (err) {
            if (!err.status || err.status >= 500 || [401, 403, 408, 429].includes(err.status)) throw err;
            await this.storage.put("outbox", queued.client_id, { ...queued, error: err.message, error_status: err.status });
          }
        }
        const snapshot = await this.request("GET", "/api/offline");
        if (snapshot.user_id !== userID) throw failure("The account changed in another tab. Sign in again to sync.", 401);
        if (this.userID !== userID || await this.active() !== userID) return;
        const account = await this.account();
        await this.storage.put("accounts", userID, { ...account, snapshot });
        // Crew data is useful offline but never blocks logging or syncing.
        try {
          const friends = await this.request("GET", "/api/friends");
          if (this.userID === userID && await this.active() === userID) {
            const latest = await this.account();
            await this.storage.put("accounts", userID, { ...latest, friends });
          }
        } catch { /* last known crew is labelled by offline status */ }
        this.online = true;
        this.needsAuth = false;
        this.problem = "";
      } catch (err) {
        this.online = false;
        this.needsAuth = [401, 403].includes(err.status);
        this.problem = this.needsAuth ? err.message : "";
        if (!(await this.account())?.snapshot) throw err;
      }
    }
    async read(path) {
      const account = await this.account();
      if (!account?.snapshot) return undefined;
      const snapshot = account.snapshot;
      const seen = new Set(snapshot.entries.map((e) => e.client_id).filter(Boolean));
      const pending = (await this.pending()).filter((e) => !seen.has(e.client_id));
      const entries = [...pending.slice().reverse(), ...snapshot.entries];
      const today = this.localSession(account).today;
      const sum = (rows) => rows.reduce((n, e) => n + e.count, 0);
      const totals = new Map();
      for (const entry of entries) totals.set(entry.day, (totals.get(entry.day) || 0) + entry.count);
      if (path === "/api/goals") return { goals: snapshot.goals || [], today };
      if (path === "/api/types") return { types: snapshot.types.map((t) => ({ ...t, total: sum(entries.filter((e) => e.type_id === t.id)) })), last_used_type_id: entries[0]?.type_id || 0 };
      if (path.startsWith("/api/day/")) {
        const day = path.slice(9);
        const rows = entries.filter((e) => e.day === day);
        return { day, entries: rows, total: sum(rows), goal: [...(snapshot.goals || [])].reverse().find((g) => g.effective_day <= day) || null };
      }
      if (path.startsWith("/api/calendar?")) {
        const params = new URLSearchParams(path.split("?")[1]);
        const year = Number(params.get("year"));
        const month = Number(params.get("month"));
        const prefix = `${year}-${String(month).padStart(2, "0")}`;
        const rows = [...totals].filter(([day]) => day.startsWith(prefix)).map(([day, total]) => ({ day, total }));
        return { year, month, today, goals: snapshot.goals || [], days: new Date(Date.UTC(year, month, 0)).getUTCDate(), lead_blank: (new Date(Date.UTC(year, month - 1, 1)).getUTCDay() + 6) % 7, totals: rows, month_total: rows.reduce((n, r) => n + r.total, 0) };
      }
      if (path === "/api/overview") {
        let cursor = totals.has(today) ? today : addDays(today, -1);
        let streak = 0;
        while (totals.has(cursor)) { streak++; cursor = addDays(cursor, -1); }
        return { today: totals.get(today) || 0, week: sum(entries.filter((e) => e.day >= addDays(today, -6) && e.day <= today)), month: sum(entries.filter((e) => e.day.slice(0, 7) === today.slice(0, 7) && e.day <= today)), all_time: sum(entries), best_day: Math.max(0, ...totals.values()), streak };
      }
      if (path === "/api/friends") return account.friends || { friends: [], incoming: [], outgoing: [], leaderboard: [] };
      return undefined;
    }
  }
  root.OfflineLog = OfflineLog;
  if (typeof module !== "undefined") module.exports = { OfflineLog, addDays };
})(typeof window === "undefined" ? globalThis : window);
