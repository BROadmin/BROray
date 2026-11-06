// Package session owns independent BROray sessions in process memory only.
package session

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"time"
)

const TTL = 1800 * time.Second
const CookieName = "BRORAY_SESSION"

var ErrCapacity = errors.New("SESSION_CAPACITY")

type entry struct {
	User    string
	CSRF    string
	Expires time.Time
}
type View struct {
	User    string
	CSRF    string
	Expires time.Time
}
type Store struct {
	mu       sync.Mutex
	entries  map[[32]byte]entry
	now      func() time.Time
	random   io.Reader
	capacity int
}

func NewStore() *Store {
	return &Store{entries: make(map[[32]byte]entry), now: time.Now, random: rand.Reader, capacity: 1024}
}
func (s *Store) prune(now time.Time) {
	for key, e := range s.entries {
		if !now.Before(e.Expires) {
			delete(s.entries, key)
		}
	}
}
func (s *Store) Create(user string) (string, View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.prune(now)
	if len(s.entries) >= s.capacity {
		return "", View{}, ErrCapacity
	}
	raw := make([]byte, 64)
	defer clear(raw)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return "", View{}, errors.New("SESSION_RANDOM_FAILED")
	}
	token := hex.EncodeToString(raw[:32])
	csrf := hex.EncodeToString(raw[32:])
	key := sha256.Sum256([]byte(token))
	if _, exists := s.entries[key]; exists {
		return "", View{}, errors.New("SESSION_RANDOM_COLLISION")
	}
	e := entry{User: user, CSRF: csrf, Expires: now.Add(TTL)}
	s.entries[key] = e
	return token, View(e), nil
}

// validToken: exactly 64 lowercase hex characters (lib/web-auth.sh:83-95).
func validToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	for i := 0; i < len(token); i++ {
		if c := token[i]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Lookup validates expiry before sliding. Expired entries never resurrect.
func (s *Store) Lookup(token string) (View, bool) {
	if !validToken(token) {
		return View{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := sha256.Sum256([]byte(token))
	e, ok := s.entries[key]
	now := s.now()
	if !ok {
		return View{}, false
	}
	if !now.Before(e.Expires) {
		delete(s.entries, key)
		return View{}, false
	}
	e.Expires = now.Add(TTL)
	s.entries[key] = e
	return View(e), true
}
func (s *Store) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, sha256.Sum256([]byte(token)))
}
func ValidCSRF(view View, value string) bool {
	return value != "" && subtle.ConstantTimeCompare([]byte(view.CSRF), []byte(value)) == 1
}
