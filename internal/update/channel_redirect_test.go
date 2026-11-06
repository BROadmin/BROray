package update

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestFetcherRejectsHTTPRedirectBeforeRequest(t *testing.T) {
	var httpHits atomic.Int32
	insecure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer insecure.Close()

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, insecure.URL+"/private", http.StatusFound)
	}))
	defer secure.Close()
	u, err := url.Parse(secure.URL)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		injected bool
	}{
		{name: "production-client"},
		{name: "injected-client", injected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := NewFetcher()
			if tc.injected {
				f.Client = secure.Client()
			} else {
				f.Client.Transport = secure.Client().Transport
			}
			body, _, err := f.open(context.Background(), u)
			if body != nil {
				body.Close()
			}
			if err == nil {
				t.Fatal("HTTP redirect was accepted")
			}
			if got := httpHits.Load(); got != 0 {
				t.Fatalf("HTTP redirect sent %d requests before refusal", got)
			}
		})
	}
}

func TestFetcherFollowsHTTPSRedirect(t *testing.T) {
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/ok", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	defer secure.Close()
	u, err := url.Parse(secure.URL + "/redirect")
	if err != nil {
		t.Fatal(err)
	}
	f := NewFetcher()
	f.Client.Transport = secure.Client().Transport
	body, _, err := f.open(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil || string(got) != "ok" {
		t.Fatalf("HTTPS redirect body = %q, %v", got, err)
	}
}
