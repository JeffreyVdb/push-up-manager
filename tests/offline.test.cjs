const assert = require('node:assert/strict');
const { test } = require('node:test');
const { OfflineLog } = require('../web/offline.js');
const clone = (value) => value === undefined ? undefined : structuredClone(value);
class MemoryStore {
  constructor() { this.data = { meta: new Map(), accounts: new Map(), outbox: new Map() }; }
  async get(name, key) { return clone(this.data[name].get(key)); }
  async put(name, key, value) {
    if (this.failWrites) throw new Error('Device storage is full');
    this.data[name].set(key, clone(value));
  }
  async remove(name, key) { this.data[name].delete(key); }
  async all(name) { return [...this.data[name].values()].map(clone); }
  async acknowledge(userID, key, entry) {
    if (this.failWrites) throw new Error('Device storage is full');
    const account = await this.get('accounts', userID);
    if (!account.snapshot.entries.some((e) => e.id === entry.id || e.client_id === key)) account.snapshot.entries.unshift(entry);
    await this.put('accounts', userID, account);
    await this.remove('outbox', key);
  }
}
function setup() {
  const storage = new MemoryStore();
  const session = { authenticated: true, user_id: 1, username: 'alice', csrf_token: 'csrf', today: '2026-10-23', day_rollover_ms: 3600000 };
  const server = { session, online: true, entries: [], receipts: new Map(), attempts: [], types: [{ id: 2, name: 'Standard' }] };
  let uuid = 0;
  const request = async (method, path, body) => {
    if (!server.online) throw new Error('Offline');
    if (path === '/api/session') return clone(server.session);
    if (path === '/api/offline') return { user_id: server.session.user_id, entries: clone(server.entries), types: clone(server.types) };
    if (path === '/api/friends') return { friends: [], incoming: [], outgoing: [], leaderboard: [] };
    if (path === '/api/reps') {
      server.attempts.push(clone(body));
      if (server.reject) throw Object.assign(new Error(server.reject.message), { status: server.reject.status });
      if (!server.receipts.has(body.client_id)) {
        const entry = { ...body, id: server.entries.length + 1, type_name: 'Standard', created_at: '2026-10-23T11:00:00Z' };
        server.entries.push(entry);
        server.receipts.set(body.client_id, entry);
      }
      if (server.loseResponse) { server.loseResponse = false; throw new Error('Connection dropped after commit'); }
      return clone(server.receipts.get(body.client_id));
    }
    throw new Error(`Unexpected ${method} ${path}`);
  };
  const make = () => new OfflineLog({ request, storage, uuid: () => `operation-${++uuid}-unique` });
  return { storage, session, server, make };
}
async function prepared() {
  const app = setup();
  app.log = app.make();
  await app.log.accept(app.session);
  await app.log.refresh();
  return app;
}

test('offline logging survives reload and sync preserves the selected day', async () => {
  const { log, server, make } = await prepared();
  server.online = false;
  await log.enqueue({ type_id: 2, count: 17, day: '2026-10-22' }, 'Standard');
  await log.refresh();
  const reloaded = make();
  assert.equal((await reloaded.restore()).username, 'alice');
  assert.equal((await reloaded.read('/api/day/2026-10-22')).total, 17);
  assert.equal((await reloaded.read('/api/overview')).all_time, 17);
  server.online = true;
  server.session.today = '2026-10-24';
  await reloaded.refresh();
  assert.equal(server.entries.length, 1);
  assert.equal(server.entries[0].day, '2026-10-22');
  assert.equal((await reloaded.pending()).length, 0);
});

test('a lost response retries the same operation without duplicate totals', async () => {
  const { log, server } = await prepared();
  await log.enqueue({ type_id: 2, count: 23, day: '2026-10-23' }, 'Standard');
  server.loseResponse = true;
  await log.refresh();
  assert.equal((await log.pending()).length, 1);
  assert.equal((await log.read('/api/overview')).all_time, 23);
  await log.refresh();
  assert.equal(server.attempts.length, 2);
  assert.deepEqual(server.attempts[0], server.attempts[1]);
  assert.equal(server.entries.length, 1);
  assert.equal((await log.read('/api/overview')).all_time, 23);
});

test('account switch and logout isolate cached data and queued operations', async () => {
  const { log, server, session, make, storage } = await prepared();
  await log.enqueue({ type_id: 2, count: 31, day: '2026-10-23' }, 'Standard');
  server.session = { ...session, user_id: 9, username: 'bob' };
  await log.refresh();
  assert.equal(log.needsAuth, true);
  assert.equal(server.attempts.length, 0);
  await log.accept(server.session);
  await log.refresh();
  assert.equal((await log.pending()).length, 0);
  assert.equal((await log.read('/api/overview')).all_time, 0);
  assert.equal((await storage.all('outbox')).length, 1);
  await log.leave();
  assert.equal(await make().restore(), null);
  server.session = session;
  await log.accept(session);
  await log.refresh();
  assert.equal(server.entries[0].user_id, 1);
});

test('deleted variations keep failed sets and can be reassigned explicitly', async () => {
  const { log, server } = await prepared();
  const entry = await log.enqueue({ type_id: 2, count: 11, day: '2026-10-21' }, 'Standard');
  server.reject = { status: 404, message: 'Variation no longer exists' };
  await log.refresh();
  assert.equal((await log.pending())[0].error_status, 404);
  assert.equal((await log.read('/api/day/2026-10-21')).total, 11);
  server.reject = null;
  await log.reassign(entry.client_id, 8, 'Wide');
  await log.refresh();
  assert.equal(server.entries[0].type_id, 8);
  assert.equal(server.entries[0].day, '2026-10-21');
});

test('quota errors never acknowledge an unsaved local set', async () => {
  const { log, storage } = await prepared();
  storage.failWrites = true;
  await assert.rejects(log.enqueue({ type_id: 2, count: 40, day: '2026-10-23' }, 'Standard'), /storage is full/);
  assert.equal((await log.pending()).length, 0);
});

test('server snapshot with a still-pending client ID does not double count', async () => {
  const { log, storage } = await prepared();
  const entry = await log.enqueue({ type_id: 2, count: 19, day: '2026-10-23' }, 'Standard');
  const account = await storage.get('accounts', 1);
  account.snapshot.entries.push({ ...entry, id: 45, pending: false });
  await storage.put('accounts', 1, account);
  assert.equal((await log.read('/api/day/2026-10-23')).total, 19);
  assert.equal((await log.read('/api/calendar?year=2026&month=10')).month_total, 19);
  assert.equal((await log.read('/api/types')).types[0].total, 19);
});

test('transient errors and expired sessions retain retryable sets', async () => {
  const { log, server } = await prepared();
  await log.enqueue({ type_id: 2, count: 7, day: '2026-10-23' }, 'Standard');
  for (const status of [500, 429, 403]) {
    server.reject = { status, message: 'Try later' };
    await log.refresh();
    assert.equal((await log.pending())[0].error, undefined);
  }
  server.reject = null;
  await log.refresh();
  assert.equal((await log.pending()).length, 0);
});

test('two open tabs can replay the same durable queue without duplicate entries', async () => {
  const { log, server, make } = await prepared();
  await log.enqueue({ type_id: 2, count: 14, day: '2026-10-23' }, 'Standard');
  const otherTab = make();
  await otherTab.restore();
  await Promise.all([log.refresh(), otherTab.refresh()]);
  assert.equal(server.entries.length, 1);
  assert.equal((await log.pending()).length, 0);
  assert.equal((await otherTab.read('/api/overview')).all_time, 14);
});

test('a late session response cannot undo explicit offline sign-out', async () => {
  const { log, storage } = await prepared();
  const original = log.request;
  let release;
  log.request = async (method, path, body) => {
    if (path === '/api/session') await new Promise((resolve) => { release = resolve; });
    return original(method, path, body);
  };
  const refresh = log.refresh();
  // Let the initial active-account read reach the suspended session request.
  while (!release) await Promise.resolve();
  await log.leave();
  release();
  await refresh;
  assert.equal(await storage.get('meta', 'active'), 0);
  assert.equal(log.userID, 0);
});

test('dated goals survive offline reload, include queued reps and preserve earlier targets', async () => {
  const { log, storage, server, make } = await prepared();
  const account = await log.account();
  account.snapshot.goals = [{effective_day:'2026-09-05',target:100},{effective_day:'2026-09-10',target:150}];
  await storage.put('accounts',1,account);
  server.online = false;
  await log.enqueue({type_id:2,count:100,day:'2026-09-05'},'Standard');
  const restored = make(); await restored.restore();
  assert.equal((await restored.read('/api/day/2026-09-04')).goal,null);
  assert.equal((await restored.read('/api/day/2026-09-05')).goal.target,100);
  assert.equal((await restored.read('/api/day/2026-09-05')).total,100);
  assert.equal((await restored.read('/api/day/2026-09-09')).goal.target,100);
  assert.equal((await restored.read('/api/day/2026-09-10')).goal.target,150);
  assert.equal((await restored.read('/api/calendar?year=2026&month=9')).goals.length,2);
  assert.equal((await restored.read('/api/goals')).goals.length,2);
});

test('cached restoration is not evidence of a failed connection and preserves its server clock', async () => {
  const { server, make, storage } = await prepared();
  const before = await storage.get('accounts',1);
  const restored = make();
  const session = await restored.restore();
  assert.equal(restored.online,null,'connection has not been checked yet');
  assert.equal(session.cached,true);
  assert.equal(session.offline,false);
  assert.equal((await storage.get('accounts',1)).clock,before.clock);
  server.online = false;
  await restored.refresh();
  assert.equal(restored.online,false,'failed request confirms offline');
  server.online = true;
  await restored.refresh();
  assert.equal(restored.online,true,'successful refresh confirms connected');
});
