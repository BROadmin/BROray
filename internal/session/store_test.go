package session

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"
)

func TestSESSION003TTL1800(t *testing.T) {
	s := NewStore()
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	token, v, e := s.Create("SyntheticUser")
	if e != nil || v.Expires.Sub(now) != 1800*time.Second {
		t.Fatal("TTL")
	}
	b, e := hex.DecodeString(token)
	if e != nil || len(b) < 32 {
		t.Fatal("token entropy size")
	}
	if len(v.CSRF) != 64 || v.CSRF == token {
		t.Fatal("independent CSRF")
	}
}
func TestSESSION004SlidingExpiry(t *testing.T) {
	s := NewStore()
	now := time.Unix(1000, 0)
	s.now = func() time.Time { return now }
	token, _, _ := s.Create("SyntheticUser")
	now = now.Add(1700 * time.Second)
	v, ok := s.Lookup(token)
	if !ok || v.Expires != now.Add(TTL) {
		t.Fatal("sliding")
	}
	now = now.Add(TTL)
	if _, ok = s.Lookup(token); ok {
		t.Fatal("expiry boundary resurrected")
	}
}
func TestSESSION005Logout(t *testing.T) {
	s := NewStore()
	token, _, _ := s.Create("SyntheticUser")
	s.Delete(token)
	if _, ok := s.Lookup(token); ok {
		t.Fatal("session survived logout")
	}
}
func TestSESSION006RAMOnly(t *testing.T) {
	s := NewStore()
	token, _, _ := s.Create("SyntheticUser")
	other := NewStore()
	if len(s.entries) != 1 {
		t.Fatal("not held in RAM")
	}
	if _, ok := other.Lookup(token); ok {
		t.Fatal("global or persisted session")
	}
}
func TestSESSION007Restart(t *testing.T) {
	s := NewStore()
	token, _, _ := s.Create("SyntheticUser")
	s = NewStore()
	if _, ok := s.Lookup(token); ok {
		t.Fatal("restart retained session")
	}
}
func TestCapacityAndRandomFailure(t *testing.T) {
	s := NewStore()
	s.capacity = 1
	_, _, _ = s.Create("test")
	if _, _, e := s.Create("test"); e != ErrCapacity {
		t.Fatal("unbounded store")
	}
	s = NewStore()
	s.random = bytes.NewReader(nil)
	if _, _, e := s.Create("test"); e == nil || len(s.entries) != 0 {
		t.Fatal("random failure admitted")
	}
}
func TestCSRFAndInvalidTokens(t *testing.T) {
	s := NewStore()
	token, v, _ := s.Create("test")
	if !ValidCSRF(v, v.CSRF) || ValidCSRF(v, token) || ValidCSRF(v, "") {
		t.Fatal("CSRF")
	}
	for _, bad := range []string{"", token[:63], "not-hex"} {
		if _, ok := s.Lookup(bad); ok {
			t.Fatal("invalid token")
		}
	}
}
