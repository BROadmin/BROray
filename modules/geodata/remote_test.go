package geodata

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func remoteFixture(fn roundTripFunc) *Remote {
	remote := NewRemote()
	remote.client.Transport = fn
	return remote
}
func TestRemotePinnedSources(t *testing.T) {
	rev := strings.Repeat("a", 40)
	remote := remoteFixture(func(req *http.Request) (*http.Response, error) {
		var data string
		switch req.URL.String() {
		case "https://api.github.com/repos/v2fly/geoip/commits/release":
			data = `{"sha":"` + rev + `"}`
		case "https://raw.githubusercontent.com/v2fly/geoip/" + rev + "/dat/fi.dat":
			data = "native-bytes"
		default:
			t.Fatalf("unexpected request %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(data)), Header: make(http.Header)}, nil
	})
	got, err := remote.Resolve(context.Background(), GeoIP)
	if err != nil || got != rev {
		t.Fatalf("revision %q %v", got, err)
	}
	plan, err := SourcePlan(testSelection, testRevisions)
	if err != nil {
		t.Fatal(err)
	}
	data, err := remote.Fetch(context.Background(), plan[0])
	if err != nil || string(data) != "native-bytes" {
		t.Fatalf("fetch %q %v", data, err)
	}
	plan[0].URL = "https://evil.invalid/data"
	if _, err := remote.Fetch(context.Background(), plan[0]); err == nil {
		t.Fatal("noncanonical source fetched")
	}
}

func TestRemoteRejectsErrorsBoundsAndMutableRevision(t *testing.T) {
	plan, _ := SourcePlan(testSelection, testRevisions)
	for _, tc := range []struct {
		name   string
		status int
		size   int64
		body   string
	}{
		{"server", 503, 0, "error"},
		{"oversize", 200, MaxAssetBytes + 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remote := remoteFixture(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, ContentLength: tc.size, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			if _, err := remote.Fetch(context.Background(), plan[0]); err == nil {
				t.Fatal("invalid download accepted")
			}
		})
	}
	remote := remoteFixture(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"sha":"release"}`)), Header: make(http.Header)}, nil
	})
	if _, err := remote.Resolve(context.Background(), GeoIP); err == nil {
		t.Fatal("mutable revision accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := remote.Fetch(ctx, plan[0]); err != context.Canceled {
		t.Fatalf("canceled fetch: %v", err)
	}
}
