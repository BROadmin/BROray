package servers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// A private in-memory object API, not evidence of a public cloud account.
// S3 verifies the transmitted SigV4 signature; template requires its token.
func cloudStorageFixture(t *testing.T, kind string) string {
	t.Helper()
	var mu sync.Mutex
	objects := map[string][]byte{}
	counts := map[string]int{}
	retryFault, authFault, tokens := false, false, 0
	baseKind := strings.Split(kind, "-")[0]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			http.Error(w, "read", 400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if kind == "template-recovery" && r.URL.Path == "/token" {
			if r.Method != "POST" || string(body) != "refresh_token=synthetic-private-refresh" {
				http.Error(w, "form", 400)
				return
			}
			tokens++
			json.NewEncoder(w).Encode(map[string]any{"access_token": fmt.Sprintf("synthetic-token-%d", tokens), "expires_in": 3600})
			return
		}
		if baseKind == "s3" {
			if !verifyFixtureS3(r, body) {
				http.Error(w, "signature", 403)
				return
			}
		} else if kind == "template-recovery" {
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer synthetic-token-") {
				http.Error(w, "token", 401)
				return
			}
			if !authFault {
				authFault = true
				http.Error(w, "expired fixture token", 401)
				return
			}
		} else if r.Header.Get("Authorization") != "Bearer synthetic-private-token" {
			http.Error(w, "token", 401)
			return
		}
		if strings.Contains(kind, "-") && r.Method == "PUT" && !retryFault {
			retryFault = true
			w.Header().Set("Retry-After", "1")
			http.Error(w, "temporary fixture limit", 429)
			return
		}
		counts[r.Method]++
		key := strings.TrimPrefix(r.URL.Path, "/synthetic-bucket/")
		if baseKind == "template" {
			key = strings.TrimPrefix(r.URL.Path, "/objects/")
		}
		list := r.URL.Query().Get("list-type") == "2" || r.URL.Path == "/list"
		if list {
			prefix := r.URL.Query().Get("prefix")
			if baseKind == "template" {
				prefix = "profiles/test/" + strings.TrimSuffix(prefix, "/") + "/"
			}
			var names []string
			for k := range objects {
				if strings.HasPrefix(k, prefix) {
					names = append(names, k)
				}
			}
			sort.Strings(names)
			if baseKind == "s3" {
				w.Header().Set("Content-Type", "application/xml")
				io.WriteString(w, "<ListBucketResult><IsTruncated>false</IsTruncated>")
				for _, k := range names {
					io.WriteString(w, "<Contents><Key>")
					xml.EscapeText(w, []byte(k))
					io.WriteString(w, "</Key></Contents>")
				}
				io.WriteString(w, "</ListBucketResult>")
			} else {
				seen, entries := map[string]bool{}, []map[string]string{}
				for _, k := range names {
					name, _, _ := strings.Cut(strings.TrimPrefix(k, prefix), "/")
					if name != "" && !seen[name] {
						seen[name] = true
						entries = append(entries, map[string]string{"name": name})
					}
				}
				json.NewEncoder(w).Encode(entries)
			}
			return
		}
		switch r.Method {
		case http.MethodPut:
			objects[key] = bytes.Clone(body)
			w.WriteHeader(200)
		case http.MethodGet:
			if b, ok := objects[key]; ok {
				w.Write(b)
			} else {
				w.WriteHeader(404)
				io.WriteString(w, "<Error><Code>NoSuchKey</Code></Error>")
			}
		case http.MethodDelete:
			delete(objects, key)
			w.WriteHeader(204)
		default:
			w.WriteHeader(405)
		}
	}))
	t.Cleanup(func() {
		server.Close()
		mu.Lock()
		defer mu.Unlock()
		if counts["PUT"] == 0 || counts["GET"] == 0 {
			t.Error("no actual object transfer")
		}
		if strings.Contains(kind, "-") && !retryFault {
			t.Error("rate-limit path not exercised")
		}
		if kind == "template-recovery" && (!authFault || tokens < 3) {
			t.Errorf("token refresh not exercised: issued=%d", tokens)
		}
		t.Logf("%s private API requests: PUT=%d GET=%d DELETE=%d", kind, counts["PUT"], counts["GET"], counts["DELETE"])
		if kind == "template-recovery" {
			t.Logf("oauth token responses=%d, rejected token=%t, rate-limit=%t", tokens, authFault, retryFault)
		}
	})
	return server.URL
}

func verifyFixtureS3(r *http.Request, body []byte) bool {
	auth := r.Header.Get("Authorization")
	var credential, signed, signature string
	if _, err := fmt.Sscanf(auth, "AWS4-HMAC-SHA256 Credential=%s SignedHeaders=%s Signature=%s", &credential, &signed, &signature); err != nil {
		return false
	}
	credential, signed = strings.TrimSuffix(credential, ","), strings.TrimSuffix(signed, ",")
	parts := strings.Split(credential, "/")
	if len(parts) != 5 || parts[0] != "synthetic-private-access" || parts[2] != "us-east-1" || parts[3] != "s3" || parts[4] != "aws4_request" {
		return false
	}
	if r.Header.Get("X-Amz-Security-Token") != "synthetic-private-token" {
		return false
	}
	hash := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	if r.Header.Get("X-Amz-Content-Sha256") != hash(body) {
		return false
	}
	var headers strings.Builder
	for _, name := range strings.Split(signed, ";") {
		value := r.Header.Get(name)
		if name == "host" {
			value = r.Host
		}
		headers.WriteString(name + ":" + strings.Join(strings.Fields(value), " ") + "\n")
	}
	canonical := r.Method + "\n" + r.URL.EscapedPath() + "\n" + r.URL.RawQuery + "\n" + headers.String() + "\n" + signed + "\n" + hash(body)
	scope := strings.Join(parts[1:], "/")
	toSign := "AWS4-HMAC-SHA256\n" + r.Header.Get("X-Amz-Date") + "\n" + scope + "\n" + hash([]byte(canonical))
	mac := func(key []byte, value string) []byte {
		h := hmac.New(sha256.New, key)
		h.Write([]byte(value))
		return h.Sum(nil)
	}
	key := []byte("AWS4synthetic-private-secret")
	for _, v := range parts[1:] {
		key = mac(key, v)
	}
	return hmac.Equal([]byte(signature), []byte(hex.EncodeToString(mac(key, toSign))))
}
