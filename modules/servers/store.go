package servers

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BROadmin/broray/internal/platform"
)

var ErrNotFound = errors.New("SERVER_NOT_FOUND")
var ErrExists = errors.New("SERVER_EXISTS")

// Store keeps servers as data/servers/<id>.json (mode 0600) and the active
// server id in data/servers/active-server.
type Store struct {
	mu  sync.Mutex
	dir string
}

func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".json") }
func (s *Store) activePath() string    { return filepath.Join(s.dir, "active-server") }

func (s *Store) List() ([]Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []Server
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var srv Server
		if platform.ReadJSON(filepath.Join(s.dir, e.Name()), &srv) == nil && Validate(srv) == nil {
			out = append(out, srv)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *Store) Get(id string) (Server, error) {
	if !ValidID(id) {
		return Server{}, ErrNotFound
	}
	var srv Server
	if err := platform.ReadJSON(s.path(id), &srv); err != nil {
		return Server{}, ErrNotFound
	}
	if err := Validate(srv); err != nil {
		return Server{}, err
	}
	return srv, nil
}

// Add stores a new server. A server with the same URI is refused.
func (s *Store) Add(srv Server) (Server, error) {
	if srv.ID == "" {
		srv.ID = NewID()
	}
	if srv.CreatedAt == "" {
		srv.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if srv.Source.Type == "" {
		srv.Source.Type = "manual"
	}
	srv.SchemaVersion = 2
	if err := Validate(srv); err != nil {
		return srv, err
	}
	existing, _ := s.List()
	for _, e := range existing {
		if e.URI == srv.URI && srv.URI != "" {
			return e, ErrExists
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Lstat(s.path(srv.ID)); err == nil {
		return srv, ErrExists
	}
	return srv, platform.WriteJSONAtomic(s.path(srv.ID), srv, 0o600)
}

func (s *Store) Put(srv Server) error {
	if err := Validate(srv); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return platform.WriteJSONAtomic(s.path(srv.ID), srv, 0o600)
}

func (s *Store) Delete(id string) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path(id)); err != nil {
		return ErrNotFound
	}
	if a, _ := s.activeLocked(); a == id {
		_ = os.Remove(s.activePath())
	}
	return nil
}

// DeleteBySubscription removes all servers imported from a subscription.
func (s *Store) DeleteBySubscription(subID string) (int, error) {
	list, err := s.List()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, srv := range list {
		if srv.Source.Type == "subscription" && srv.Source.SubscriptionID == subID {
			if s.Delete(srv.ID) == nil {
				n++
			}
		}
	}
	return n, nil
}

func (s *Store) activeLocked() (string, error) {
	b, err := os.ReadFile(s.activePath())
	if err != nil {
		return "", nil
	}
	id := strings.TrimSpace(string(b))
	if !ValidID(id) {
		return "", nil
	}
	return id, nil
}

func (s *Store) Active() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, _ := s.activeLocked()
	return id
}

func (s *Store) SetActive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		err := os.Remove(s.activePath())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !ValidID(id) {
		return ErrNotFound
	}
	return platform.WriteFileAtomic(s.activePath(), []byte(id+"\n"), 0o600)
}
