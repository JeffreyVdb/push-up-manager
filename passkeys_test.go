package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

const testPasskeyOrigin = "https://pushups.example"

// virtualPasskey implements the actual WebAuthn wire format and signs with its
// own private P-256 key. These tests exercise the verifier, not a mocked result.
type virtualPasskey struct {
	key        *ecdsa.PrivateKey
	id, handle []byte
	count      uint32
}

func newVirtualPasskey(t *testing.T) *virtualPasskey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		t.Fatal(err)
	}
	return &virtualPasskey{key: key, id: id}
}
func keyB64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func keyJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func keyCBOR(t *testing.T, v any) []byte {
	t.Helper()
	b, err := cbor.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (v *virtualPasskey) clientData(t *testing.T, kind, challenge, origin string) []byte {
	return keyJSON(t, map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
}
func (v *virtualPasskey) authData(flags byte) []byte {
	hash := sha256.Sum256([]byte("pushups.example"))
	data := append(hash[:], flags)
	return binary.BigEndian.AppendUint32(data, v.count)
}
func (v *virtualPasskey) registration(t *testing.T, options map[string]any, origin string) map[string]any {
	t.Helper()
	pub := options["publicKey"].(map[string]any)
	v.handle, _ = base64.RawURLEncoding.DecodeString(pub["user"].(map[string]any)["id"].(string))
	data := v.authData(0x45)
	data = append(data, make([]byte, 16)...)
	data = binary.BigEndian.AppendUint16(data, uint16(len(v.id)))
	data = append(data, v.id...)
	data = append(data, keyCBOR(t, map[int]any{1: 2, 3: -7, -1: 1, -2: v.key.X.FillBytes(make([]byte, 32)), -3: v.key.Y.FillBytes(make([]byte, 32))})...)
	att := keyCBOR(t, map[string]any{"fmt": "none", "authData": data, "attStmt": map[string]any{}})
	return map[string]any{"id": keyB64(v.id), "rawId": keyB64(v.id), "type": "public-key", "clientExtensionResults": map[string]any{}, "authenticatorAttachment": "platform", "response": map[string]any{"clientDataJSON": keyB64(v.clientData(t, "webauthn.create", pub["challenge"].(string), origin)), "attestationObject": keyB64(att), "transports": []string{"internal"}}}
}
func (v *virtualPasskey) assertion(t *testing.T, options map[string]any, origin string, uv bool) map[string]any {
	t.Helper()
	pub := options["publicKey"].(map[string]any)
	v.count++
	flags := byte(0x01)
	if uv {
		flags |= 0x04
	}
	auth := v.authData(flags)
	client := v.clientData(t, "webauthn.get", pub["challenge"].(string), origin)
	clientHash := sha256.Sum256(client)
	signed := append(append([]byte{}, auth...), clientHash[:]...)
	digest := sha256.Sum256(signed)
	sig, err := ecdsa.SignASN1(rand.Reader, v.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"id": keyB64(v.id), "rawId": keyB64(v.id), "type": "public-key", "clientExtensionResults": map[string]any{}, "response": map[string]any{"clientDataJSON": keyB64(client), "authenticatorData": keyB64(auth), "signature": keyB64(sig), "userHandle": keyB64(v.handle)}}
}
func passkeyTestServer(t *testing.T) (*httptest.Server, *store) {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "passkeys.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.db.Exec(passkeySchema); err != nil {
		t.Fatal(err)
	}
	s := newServer(st)
	s.passkeys = newPasskeyService(st, testPasskeyOrigin)
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	return srv, st
}
func registerVirtualPasskey(t *testing.T, c *client) *virtualPasskey {
	t.Helper()
	status, options := c.do("POST", "/api/passkeys/register/begin", map[string]string{"password": "strong-password", "name": "Laptop"})
	if status != 200 {
		t.Fatalf("begin registration: %d %v", status, options)
	}
	v := newVirtualPasskey(t)
	status, body := c.do("POST", "/api/passkeys/register/finish", v.registration(t, options, testPasskeyOrigin))
	if status != 201 {
		t.Fatalf("finish registration: %d %v", status, body)
	}
	return v
}
func TestPasskeyRegistrationAndDiscoverableLogin(t *testing.T) {
	srv, st := passkeyTestServer(t)
	c := newClient(t, srv)
	c.register("passkey-user", "strong-password")
	v := registerVirtualPasskey(t, c)
	status, body := c.do("GET", "/api/passkeys", nil)
	if status != 200 || len(body["passkeys"].([]any)) != 1 {
		t.Fatalf("list: %d %v", status, body)
	}
	c.do("POST", "/api/logout", nil)
	c.csrf = ""
	status, options := c.do("POST", "/api/passkeys/login/begin", map[string]any{})
	if status != 200 {
		t.Fatal(status, options)
	}
	assertion := v.assertion(t, options, testPasskeyOrigin, true)
	status, body = c.do("POST", "/api/passkeys/login/finish", assertion)
	if status != 200 || body["username"] != "passkey-user" || body["csrf_token"] == "" || body["user_id"].(float64) <= 0 {
		t.Fatalf("login: %d %v", status, body)
	}
	c.csrf = body["csrf_token"].(string)
	if status, body := c.do("GET", "/api/types", nil); status != 200 {
		t.Fatal("passkey session unusable", status, body)
	}
	var raw string
	if err := st.db.QueryRow(`SELECT credential FROM passkeys`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	json.Unmarshal([]byte(raw), &record)
	if record["authenticator"].(map[string]any)["signCount"].(float64) != 1 {
		t.Fatalf("counter not persisted: %s", raw)
	}
	if status, body := c.do("POST", "/api/passkeys/login/finish", assertion); status == 200 {
		t.Fatal("replayed assertion accepted", body)
	}
}
func TestPasskeyRejectsOriginChallengeUVAndBrowserMismatch(t *testing.T) {
	for _, scenario := range []string{"origin", "challenge", "verification", "browser", "expired", "signature", "handle"} {
		t.Run(scenario, func(t *testing.T) {
			srv, st := passkeyTestServer(t)
			owner := newClient(t, srv)
			owner.register("passkey-user", "strong-password")
			v := registerVirtualPasskey(t, owner)
			c := newClient(t, srv)
			status, options := c.do("POST", "/api/passkeys/login/begin", nil)
			if status != 200 {
				t.Fatal(status, options)
			}
			origin := testPasskeyOrigin
			if scenario == "origin" {
				origin = "https://attacker.example"
			}
			if scenario == "challenge" {
				options["publicKey"].(map[string]any)["challenge"] = keyB64(make([]byte, 32))
			}
			if scenario == "handle" {
				v.handle = []byte("another account")
			}
			assertion := v.assertion(t, options, origin, scenario != "verification")
			if scenario == "signature" {
				assertion["response"].(map[string]any)["signature"] = keyB64(make([]byte, 64))
			}
			if scenario == "browser" {
				c = newClient(t, srv)
			}
			if scenario == "expired" {
				if _, err := st.db.Exec(`UPDATE passkey_ceremonies SET expires_at=?`, time.Now().Add(-time.Minute).Unix()); err != nil {
					t.Fatal(err)
				}
			}
			status, body := c.do("POST", "/api/passkeys/login/finish", assertion)
			if status == 200 {
				t.Fatalf("%s accepted: %v", scenario, body)
			}
			if status, body := c.do("GET", "/api/session", nil); status != 200 || body["authenticated"] != false {
				t.Fatalf("bad assertion created session: %d %v", status, body)
			}
		})
	}
}
func TestPasskeyChangesRequirePasswordCSRFAndOwnership(t *testing.T) {
	srv, _ := passkeyTestServer(t)
	owner := newClient(t, srv)
	owner.register("owner", "strong-password")
	status, _ := owner.do("POST", "/api/passkeys/register/begin", map[string]string{"password": "wrong-password"})
	if status != 401 {
		t.Fatalf("wrong password: %d", status)
	}
	owner.csrf = "wrong"
	status, _ = owner.do("POST", "/api/passkeys/register/begin", map[string]string{"password": "strong-password"})
	if status != 403 {
		t.Fatalf("bad CSRF: %d", status)
	}
	_, body := owner.do("GET", "/api/session", nil)
	owner.csrf = body["csrf_token"].(string)
	v := registerVirtualPasskey(t, owner)
	other := newClient(t, srv)
	other.register("other", "strong-password")
	status, _ = other.do("DELETE", "/api/passkeys/"+keyB64(v.id), map[string]string{"password": "strong-password"})
	if status != 404 {
		t.Fatalf("deleted another account credential: %d", status)
	}
	status, _ = owner.do("DELETE", "/api/passkeys/"+keyB64(v.id), map[string]string{"password": "wrong-password"})
	if status != 401 {
		t.Fatalf("delete wrong password: %d", status)
	}
	status, _ = owner.do("DELETE", "/api/passkeys/"+keyB64(v.id), map[string]string{"password": "strong-password"})
	if status != 200 {
		t.Fatalf("delete own key: %d", status)
	}
}
func TestPasskeyRegistrationRejectsOriginAndSessionSwap(t *testing.T) {
	for _, scenario := range []string{"origin", "session"} {
		t.Run(scenario, func(t *testing.T) {
			srv, _ := passkeyTestServer(t)
			c := newClient(t, srv)
			c.register("owner", "strong-password")
			status, options := c.do("POST", "/api/passkeys/register/begin", map[string]string{"password": "strong-password"})
			if status != 200 {
				t.Fatal(status, options)
			}
			origin := testPasskeyOrigin
			if scenario == "origin" {
				origin = "https://attacker.example"
			}
			if scenario == "session" {
				status, body := c.do("POST", "/api/login", map[string]string{"username": "owner", "password": "strong-password"})
				if status != 200 {
					t.Fatal(status, body)
				}
				c.csrf = body["csrf_token"].(string)
			}
			v := newVirtualPasskey(t)
			status, body := c.do("POST", "/api/passkeys/register/finish", v.registration(t, options, origin))
			if status == 201 {
				t.Fatalf("invalid registration accepted: %v", body)
			}
		})
	}
}
func TestPasskeyPublicOriginPolicy(t *testing.T) {
	cases := []struct {
		configured, host string
		accepted         bool
	}{{"", "evil.example", false}, {"", "localhost:8080", true}, {"", "127.0.0.1:8080", false}, {"", "[::1]:8080", false}, {"http://localhost:8080", "localhost:8080", true}, {"http://127.0.0.1:8080", "127.0.0.1:8080", false}, {"https://127.0.0.1", "127.0.0.1", false}, {"http://evil.example", "localhost:8080", false}, {"https://pushups.example", "evil.example", true}, {"https://pushups.example/path", "localhost:8080", false}}
	for _, tc := range cases {
		p := newPasskeyService(nil, tc.configured)
		req := httptest.NewRequest("POST", "http://"+tc.host+"/api/passkeys/login/begin", nil)
		_, err := p.relyingParty(req)
		if (err == nil) != tc.accepted {
			t.Errorf("configured=%q host=%q: %v", tc.configured, tc.host, err)
		}
	}
}

// localhost is a permitted development RP even though plain HTTP numeric
// loopback origins are secure contexts in browsers: RP IDs still need domains.
func TestPasskeyLocalhostDevelopment(t *testing.T) {
	p := newPasskeyService(nil, "")
	req := httptest.NewRequest("POST", "http://localhost:5554/api/passkeys/login/begin", nil)
	rp, err := p.relyingParty(req)
	if err != nil {
		t.Fatal(err)
	}
	options, data, err := rp.BeginDiscoverableLogin()
	if err != nil {
		t.Fatal(err)
	}
	if rp.Config.RPID != "localhost" || rp.Config.RPOrigins[0] != "http://localhost:5554" || options.Response.Challenge.String() == "" || data.Challenge == "" {
		t.Fatalf("invalid localhost RP: %#v %#v", rp.Config, data)
	}
}
