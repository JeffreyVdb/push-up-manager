const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { test } = require('node:test');
const vm = require('node:vm');

// Exercise the shipped client's refresh orchestration with controlled server
// responses. Rendering is stubbed so date boundaries need no browser or clock.
function client() {
  const calls = [];
  const timers = new Map();
  let timerID = 0;
  const context = vm.createContext({
    console,
    window: { matchMedia: () => ({ matches: false }) },
    document: { hidden: false },
    navigator: {},
    setTimeout: (fn, delay) => { timers.set(++timerID, { fn, delay }); return timerID; },
    clearTimeout: (id) => timers.delete(id),
    calls,
    serverDay: '2026-09-01',
  });
  vm.runInContext(readFileSync('web/app.js', 'utf8').replace(/boot\(\);\s*$/, ''), context);
  vm.runInContext(`
    state.csrf = 'test';
    state.today = state.selectedDay = '2026-08-31';
    state.cal = { year: 2026, month: 8 };
    api = async () => ({ authenticated: true, today: serverDay, day_rollover_ms: 60000 });
    loadCalendar = async () => calls.push(['calendar', state.cal.year, state.cal.month]);
    loadDay = async (day) => { state.selectedDay = day; calls.push(['day', day]); };
    loadStats = loadTypes = loadFriends = async () => {};
  `, context);
  return { context, calls, timers, run: (code) => vm.runInContext(code, context) };
}

test('refresh follows today over midnight and into the next calendar month', async () => {
  const app = client();
  await app.run('refreshAll()');
  assert.equal(app.run('state.today'), '2026-09-01');
  assert.equal(app.run('state.selectedDay'), '2026-09-01');
  assert.equal(app.run('state.cal.month'), 9);
  assert.deepEqual(app.calls.map((row) => Array.from(row)), [['calendar', 2026, 9], ['day', '2026-09-01']]);
});

test('background refresh preserves deliberate history browsing', async () => {
  const app = client();
  app.run("state.selectedDay = '2026-07-12'; state.cal.month = 7;");
  await app.run('refreshAll()');
  assert.equal(app.run('state.today'), '2026-09-01');
  assert.equal(app.run('state.selectedDay'), '2026-07-12');
  assert.equal(app.run('state.cal.month'), 7);
});

test('returning after five minutes resets history browsing to today', async () => {
  const app = client();
  app.run(`
    connectEvents = () => {};
    state.selectedDay = '2026-07-12'; state.cal.month = 7;
    inactiveAt = Date.now() - RETURN_TO_TODAY_MS;
    resumeApp();
  `);
  await app.run('refreshAll()');
  assert.equal(app.run('state.selectedDay'), '2026-09-01');
  assert.equal(app.run('state.cal.month'), 9);
});

test('visible idle page refreshes at server midnight despite device timezone', async () => {
  const app = client();
  app.context.serverDay = '2026-08-31';
  app.run('api = async () => ({ authenticated: true, today: serverDay, day_rollover_ms: 2000 });');
  await app.run('refreshAll()');
  const timer = [...app.timers.values()].find(({ delay }) => delay === 2050);
  assert.ok(timer, 'schedule rollover from server-relative time');
  app.context.serverDay = '2026-09-01';
  timer.fn();
  await app.run('refreshRun');
  assert.equal(app.run('state.selectedDay'), '2026-09-01');
});

test('offline refresh retains the day and arms a retry', async () => {
  const app = client();
  app.run('api = async () => { throw new Error("offline"); };');
  await app.run('refreshAll()');
  assert.equal(app.run('state.selectedDay'), '2026-08-31');
  assert.ok([...app.timers.values()].some(({ delay }) => delay === 60000));
});

test('refresh queued during a commit is drained even if SSE is disconnected', async () => {
  const app = client();
  app.run('committing = true;');
  await app.run('refreshAll()');
  assert.equal(app.calls.length, 0);
  app.run('committing = false; drainPending();');
  await app.run('refreshRun');
  assert.equal(app.run('state.selectedDay'), '2026-09-01');
});

test('weekly scope names Monday through Sunday across months and years', () => {
  const app = client();
  assert.equal(app.run("weekLabel('2026-09-16')"), 'SEP 14 – 20');
  assert.equal(app.run("weekLabel('2026-09-06')"), 'AUG 31 – SEP 6');
  assert.equal(app.run("weekLabel('2027-01-01')"), 'DEC 28 – JAN 3');
});

test('close totals no longer collapse into one heat bucket', () => {
  const app = client();
  const colors = [100, 105, 107, 112, 121, 123, 125, 140].map((total) => app.run(`heatColor(${total}, 140)`));
  assert.equal(new Set(colors).size, colors.length);
  assert.equal(app.run('heatColor(0, 0)'), 'var(--heat-0)');
});
