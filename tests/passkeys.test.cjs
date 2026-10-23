const test = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');

function load({ credential, native = false, document: customDocument, api: customAPI } = {}) {
  const calls = [];
  let received;
  const PublicKeyCredential = function () {};
  if (native) PublicKeyCredential.parseRequestOptionsFromJSON = options => ({ parsed: options });
  const sandbox = {
    window: { isSecureContext: true, PublicKeyCredential }, PublicKeyCredential,
    document: customDocument || { readyState: 'loading', addEventListener() {}, getElementById() { return null; } },
    navigator: { credentials: { async get(options) { received = options; return credential; } } },
    atob: value => Buffer.from(value, 'base64').toString('binary'),
    btoa: value => Buffer.from(value, 'binary').toString('base64'),
    DOMException, Uint8Array,
    api: customAPI || (async (method, route, body) => {
      calls.push({ method, route, body });
      return route.endsWith('/begin') ? { publicKey: { challenge: 'AAH-_w', rpId: 'pushups.example', userVerification: 'required', allowCredentials: [{ id: 'AgM', type: 'public-key' }] } } : { authenticated: true, user_id: 7 };
    }),
  };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../web/passkeys.js'), 'utf8'), sandbox);
  return { passkeys: sandbox.window.Passkeys, calls, received: () => received };
}

test('passkey sign-in decodes options and serializes binary assertion without losing bytes', async () => {
  const bytes = (...values) => new Uint8Array(values).buffer;
  const context = load({ credential: {
    id: 'AgM', rawId: bytes(2, 3), type: 'public-key', authenticatorAttachment: 'platform',
    response: { clientDataJSON: bytes(0, 255), authenticatorData: bytes(254, 1), signature: bytes(253, 2), userHandle: bytes(252, 3) },
    getClientExtensionResults() { return {}; },
  } });
  const result = await context.passkeys.signIn();
  assert.equal(result.authenticated, true);
  assert.deepEqual([...context.received().publicKey.challenge], [0, 1, 254, 255]);
  assert.deepEqual([...context.received().publicKey.allowCredentials[0].id], [2, 3]);
  assert.equal(context.received().publicKey.userVerification, 'required');
  const finish = context.calls[1];
  assert.equal(finish.route, '/api/passkeys/login/finish');
  assert.equal(finish.body.response.clientDataJSON, 'AP8');
  assert.equal(finish.body.response.signature, '_QI');
  assert.equal(finish.body.response.userHandle, '_AM');
  assert.equal(finish.body.rawId, 'AgM');
});

test('passkey sign-in supports browser native JSON APIs', async () => {
  const serialized = { id: 'native', type: 'public-key', response: { signature: 'native-signature' } };
  const context = load({ native: true, credential: { toJSON() { return serialized; } } });
  await context.passkeys.signIn();
  assert.equal(context.received().publicKey.parsed.rpId, 'pushups.example');
  assert.equal(context.calls[1].body, serialized);
});

test('cancelled passkey request does not send an assertion', async () => {
  const context = load({ credential: null });
  await assert.rejects(context.passkeys.signIn(), { name: 'NotAllowedError' });
  assert.equal(context.calls.length, 1);
});

test('account changes discard late passkey responses and clear private settings fields', async () => {
  const makeNode = () => ({
    value: '', textContent: '', hidden: false, children: [],
    addEventListener() {}, setAttribute() {}, querySelectorAll() { return []; },
    replaceChildren() { this.children = []; }, append(...nodes) { this.children.push(...nodes); },
  });
  const nodes = new Map();
  for (const id of ['account-passkeys', 'passkey-form', 'passkey-name', 'passkey-password', 'passkey-list', 'passkey-availability', 'passkey-error', 'passkey-status']) nodes.set(id, makeNode());
  const pending = [];
  const context = load({
    document: { readyState: 'loading', addEventListener() {}, getElementById(id) { return nodes.get(id); }, createElement: makeNode },
    api: () => new Promise(resolve => pending.push(resolve)),
  });
  const stale = context.passkeys.refresh();
  nodes.get('passkey-password').value = 'private-password';
  nodes.get('passkey-name').value = 'Private laptop';
  context.passkeys.clear();
  assert.equal(nodes.get('passkey-password').value, '');
  assert.equal(nodes.get('passkey-name').value, '');
  const current = context.passkeys.refresh();
  pending[1]({ available: true, passkeys: [{ id: 'new', name: 'New account key' }] });
  await current;
  pending[0]({ available: true, passkeys: [{ id: 'old', name: 'Previous account key' }] });
  await stale;
  assert.equal(nodes.get('passkey-list').children.length, 1);
  assert.equal(nodes.get('passkey-list').children[0].children[0].textContent, 'New account key');
});
