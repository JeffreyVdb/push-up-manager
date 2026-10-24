const {test} = require('node:test');
const assert = require('node:assert/strict');
const {create, originals, normalize} = require('../web/quotes.js');
function storage() { const data = new Map(); return {getItem:k=>data.get(k),setItem:(k,v)=>data.set(k,v)}; }
test('15 offline reflections stay stable per date and do not repeat on consecutive days', () => {
  const store = storage();
  assert.equal(originals.length, 15);
  let previous;
  for (let i = 1; i <= 30; i++) {
    const day = `2026-09-${String(i).padStart(2,'0')}`;
    const service = create(store);
    const q = service.forDay(day, 'alice');
    assert.notEqual(q.text, previous);
    assert.deepEqual(create(store).forDay(day, 'alice'), q);
    previous = q.text;
  }
});
test('provider duplicates of yesterday fall back to an original reflection', async () => {
  const service = create(storage());
  const yesterday = service.forDay('2026-09-05','alice');
  await service.refresh('2026-09-05','alice', async()=>({quotes:[{...yesterday,text:yesterday.text.toUpperCase(),source:'ZenQuotes',source_url:'https://zenquotes.io/'}]}));
  assert.deepEqual(service.forDay('2026-09-05','alice'), yesterday);
  assert.notEqual(normalize(service.forDay('2026-09-06','alice').text),normalize(yesterday.text));
});
test('working provider pool persists, is attributed, and refreshes once per day', async () => {
  const store = storage(); const service = create(store); let calls = 0;
  const q = {text:'Take a breath and notice the present moment.',author:'Test author',source:'ZenQuotes',source_url:'https://zenquotes.io/'};
  const request = async()=>{calls++;return {quotes:[q]}};
  await service.refresh('2026-09-05','alice',request);
  await create(store).refresh('2026-09-05','alice',request);
  assert.equal(calls,1);
  assert.deepEqual(create(store).forDay('2026-09-06','alice'),q);
  assert.equal(create(store).forDay('2026-09-06','bob').author,'Daily reflection');
});
test('denied/corrupt storage and provider failures keep offline reflections available', async () => {
  for (const store of [{getItem:()=>'{broken',setItem:()=>{}},{getItem:()=>{throw Error()},setItem:()=>{throw Error()}}]) {
    const service = create(store);
    await service.refresh('2026-09-05','alice',async()=>{throw Error('offline')});
    assert.equal(service.forDay('2026-09-05','alice').author,'Daily reflection');
  }
});
test('history browsing avoids already selected neighboring days', () => {
  const service = create(storage());
  const a = service.forDay('2026-09-06','alice');
  const b = service.forDay('2026-09-04','alice');
  const middle = service.forDay('2026-09-05','alice');
  assert.notEqual(middle.text,a.text); assert.notEqual(middle.text,b.text);
});
