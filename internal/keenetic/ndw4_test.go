package keenetic

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha3"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
)

const syntheticNDW4User = "synthetic-user"
const syntheticNDW4Password = "synthetic-password-only"
const syntheticNDW4Salt = "synthetic-salt16" // 16 bytes
const syntheticNDW4ServerNonce = "synthetic-server-nonce"

func independentlyExpectedNDW4(nonce string) (string, string, string) {
	key := argon2.IDKey([]byte(syntheticNDW4Password), []byte(syntheticNDW4Salt), 2, 8192, 1, 64)
	mac := func(key []byte, text string) []byte {
		h := hmac.New(func() hash.Hash { return sha3.New512() }, key)
		_, _ = h.Write([]byte(text))
		return h.Sum(nil)
	}
	client := mac(key, "NDW4 Interactive Client Key")
	stored := sha3.Sum512(client)
	server := mac(key, "NDW4 Interactive Server Key")
	transcript := "login1=" + syntheticNDW4User + ",nonce1=" + nonce +
		";iter2=2,memcost2=8192,nonce2=" + syntheticNDW4ServerNonce +
		",salt2=" + base64.StdEncoding.EncodeToString([]byte(syntheticNDW4Salt)) +
		";login3=" + syntheticNDW4User + ",nonce3=" + syntheticNDW4ServerNonce
	xor := func(v []byte) []byte {
		for i := range v {
			v[i] ^= client[i]
		}
		return v
	}
	proof := xor(mac(stored[:], transcript))
	signature := mac(server, transcript)
	final := xor(mac(stored[:], transcript+";signature4="+base64.StdEncoding.EncodeToString(signature)))
	return base64.StdEncoding.EncodeToString(proof), base64.StdEncoding.EncodeToString(signature), base64.StdEncoding.EncodeToString(final)
}

type ndw4Fake struct {
	mu        sync.Mutex
	scheme    string
	failure   string
	getCount  int
	postCount int
	nonce     string
	bad       bool
	rights    *rciFake
}

func (w *ndw4Fake) ServeHTTP(out http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.rights == nil {
		f := newRCIFake("synthetic-ndm", syntheticNDW4User, []string{"cli", "http"})
		w.rights = &f
	}
	if w.rights.serve(out, r) {
		return
	}
	if r.URL.Path != "/auth" {
		w.bad = true
		out.WriteHeader(404)
		return
	}
	if r.Method == http.MethodGet {
		w.getCount++
		if w.postCount == 3 {
			out.WriteHeader(200)
			return
		}
		if len(r.Cookies()) != 0 {
			w.bad = true
		}
		if w.scheme == "both" || w.scheme == "both-data" {
			out.Header().Add("WWW-Authenticate", `x-ndw2-interactive`)
			if w.scheme == "both-data" {
				out.Header().Add("WWW-Authenticate", `x-ndw4-interactive endpoint="/auth" data="e30="`)
			} else {
				out.Header().Add("WWW-Authenticate", `x-ndw4-interactive endpoint="/auth"`)
			}
		} else if w.scheme == "ndw4" {
			out.Header().Add("WWW-Authenticate", `x-ndw4-interactive endpoint="/auth"`)
		} else if w.scheme == "bootstrap" {
			out.Header().Add("WWW-Authenticate", `x-ndw2-interactive`)
			out.Header().Add("WWW-Authenticate", `x-ndw4-authenticate`)
		} else if w.scheme == "unknown" {
			out.Header().Add("WWW-Authenticate", "future-unknown-scheme")
		} else if w.scheme == "duplicate" {
			out.Header().Add("WWW-Authenticate", "x-ndw4-interactive")
			out.Header().Add("WWW-Authenticate", "x-ndw4-interactive")
		}
		out.Header().Set("X-NDM-Realm", "synthetic-realm")
		out.Header().Set("X-NDM-Challenge", "synthetic-challenge")
		http.SetCookie(out, &http.Cookie{Name: "synthetic-ndm", Value: "synthetic-cookie", Path: "/", HttpOnly: true})
		out.WriteHeader(401)
		return
	}
	if r.Method != http.MethodPost {
		w.bad = true
		out.WriteHeader(405)
		return
	}
	w.postCount++
	cookie, e := r.Cookie("synthetic-ndm")
	if e != nil || cookie.Value != "synthetic-cookie" {
		w.bad = true
	}
	var body map[string]string
	if r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&body) != nil ||
		body["login"] != syntheticNDW4User {
		w.bad = true
	}
	ndw4Header := func(value any) {
		b, _ := json.Marshal(value)
		out.Header().Add("X-NDM-Data", base64.StdEncoding.EncodeToString(b))
	}
	switch w.postCount {
	case 1:
		w.nonce = body["nonce"]
		raw, e := base64.StdEncoding.DecodeString(w.nonce)
		if e != nil || len(raw) != 16 || len(body) != 2 {
			w.bad = true
		}
		m := any(8192)
		if w.failure == "lowram" {
			m = 65536
		}
		if w.failure == "stringmem" {
			m = "8192"
		}
		if w.failure == "overbudget" {
			m = 65537
		}
		data := map[string]any{"salt": base64.StdEncoding.EncodeToString([]byte(syntheticNDW4Salt)), "nonce": syntheticNDW4ServerNonce, "iter": 2, "memcost": m}
		if w.failure == "bad-json" {
			out.Header().Set("X-NDM-Data", base64.StdEncoding.EncodeToString([]byte(`{"salt":"a","salt":"b"}`)))
		} else if w.failure == "missing-data" {
		} else {
			ndw4Header(data)
		}
		out.WriteHeader(401)
	case 2:
		proof, signature, _ := independentlyExpectedNDW4(w.nonce)
		if len(body) != 3 || body["nonce"] != syntheticNDW4ServerNonce || body["proof"] != proof {
			w.bad = true
		}
		if w.failure == "wrong-password" {
			out.WriteHeader(401)
			return
		}
		if w.failure == "bad-signature" {
			signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
		}
		ndw4Header(map[string]any{"signature": signature})
		out.WriteHeader(401)
	case 3:
		_, _, final := independentlyExpectedNDW4(w.nonce)
		if len(body) != 3 || body["nonce"] != syntheticNDW4ServerNonce || body["signature-proof"] != final {
			w.bad = true
		}
		if w.failure == "phase3-reject" {
			out.WriteHeader(401)
		} else {
			out.WriteHeader(200)
		}
	default:
		w.bad = true
		out.WriteHeader(400)
	}
}
func runNDW4Fake(t *testing.T, scheme, failure string, free uint64) (error, int, int, bool) {
	t.Helper()
	f := &ndw4Fake{scheme: scheme, failure: failure}
	svr := httptest.NewServer(f)
	defer svr.Close()
	uri, _ := url.Parse(svr.URL + "/auth")
	// The synthetic server performs password crypto under race instrumentation;
	// ARM64 QEMU can take longer than the production request budget here.
	v := &Verifier{endpoint: *uri, requestTimeout: 30 * time.Second, connectTimeout: time.Second,
		memoryAvailable: func() (uint64, error) { return free, nil }}
	err := v.Verify(context.Background(), syntheticNDW4User, syntheticNDW4Password)
	f.mu.Lock()
	defer f.mu.Unlock()
	return err, f.getCount, f.postCount, f.bad
}
func TestNDW4GoldenVectorIndependentBROrayOracle(t *testing.T) {
	proof, sig, final := independentlyExpectedNDW4("AAECAwQFBgcICQoLDA0ODw==")
	want := map[string]string{
		"proof": "fb441a60f43b9d6f9bb2cd739c967ff3bd52b1e4bbf78a48ff9bb8167288cea9",
		"sig":   "ebca02a091d8d35e696432c75d54a77aa5e2f487bd48abf3b78950e88d60c861",
		"final": "aaba1ee417fdd035b4f28bb169521dd8679dc4d855f2b5f39f91aa68ef90c3ec",
	}
	for name, value := range map[string]string{"proof": proof, "sig": sig, "final": final} {
		decoded, e := base64.StdEncoding.DecodeString(value)
		if e != nil {
			t.Fatal(e)
		}
		actual := sha256.Sum256(decoded)
		if hex.EncodeToString(actual[:]) != want[name] {
			t.Fatalf("reference proof mismatch %s", name)
		}
	}
}
func TestNDW4Physical5_2BothCapabilitiesPreferred(t *testing.T) {
	for _, scheme := range []string{"both", "both-data", "ndw4"} {
		t.Run(scheme, func(t *testing.T) {
			err, gets, posts, bad := runNDW4Fake(t, scheme, "", 512<<20)
			if err != nil || bad || gets != 2 || posts != 3 {
				t.Fatalf("ndw4 flow failed err=%v gets=%d posts=%d bad=%v", err, gets, posts, bad)
			}
		})
	}
}
func TestNDW4FailClosedNoLegacyDowngrade(t *testing.T) {
	cases := []struct {
		scheme, failure string
		want            error
		gets, posts     int
	}{
		{"both", "wrong-password", ErrInvalidCredentials, 1, 2},
		{"both", "bad-signature", ErrProtocol, 1, 2},
		{"both", "phase3-reject", ErrInvalidCredentials, 1, 3},
		{"both", "lowram", ErrResource, 1, 1},
		{"both", "bad-json", ErrProtocol, 1, 1},
		{"both", "missing-data", ErrProtocol, 1, 1},
		{"both", "stringmem", ErrProtocol, 1, 1},
		{"both", "overbudget", ErrProtocol, 1, 1},
		{"bootstrap", "", ErrUnsupported, 1, 0},
		{"unknown", "", ErrUnsupported, 1, 0},
		{"duplicate", "", ErrProtocol, 1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.scheme+"-"+tc.failure, func(t *testing.T) {
			free := uint64(512 << 20)
			if tc.failure == "lowram" {
				free = 40 << 20
			}
			err, gets, posts, bad := runNDW4Fake(t, tc.scheme, tc.failure, free)
			if !errors.Is(err, tc.want) || gets != tc.gets || posts != tc.posts || bad {
				t.Fatalf("unsafe fallback or wrong state err=%v want=%v GET=%d POST=%d bad=%v", err, tc.want, gets, posts, bad)
			}
		})
	}
}
func TestNDW4MemoryReserveAndMalformedCapabilities(t *testing.T) {
	for _, c := range []struct {
		memory uint32
		free   uint64
		want   error
	}{
		{8192, 64 << 20, nil}, {65536, 64 << 20, ErrResource}, {65536, 100 << 20, nil},
	} {
		err := checkNDW4Memory(c.memory, func() (uint64, error) { return c.free, nil })
		if !errors.Is(err, c.want) {
			t.Fatalf("memory=%d free=%d err=%v", c.memory, c.free, err)
		}
	}
	for _, c := range []string{"x-ndw4-interactive endpoint=\"/evil\"", "x-ndw4-authenticate", "future-scheme"} {
		h := http.Header{"Www-Authenticate": []string{c}}
		if _, err := selectAuthMode(h); err == nil {
			t.Fatalf("unsafe scheme accepted: %q", c)
		}
	}
	if got, err := selectAuthMode(http.Header{"Www-Authenticate": []string{"x-ndw2-interactive"}}); err != nil || got != authLegacy {
		t.Fatal("legacy scheme lost", err)
	}
}
