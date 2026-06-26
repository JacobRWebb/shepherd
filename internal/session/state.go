package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

// File is the on-disk session registry (.shepherd/sessions.json).
type File struct {
	Version   int             `json:"version"`
	Backend   Backend         `json:"backend,omitempty"`
	UpdatedAt time.Time       `json:"updated_at"`
	Sessions  map[string]Info `json:"sessions"`
}

// Store is a file-locked JSON session registry shared across processes.
type Store struct {
	path string
	lock *flock.Flock
	mu   sync.Mutex
}

// OpenStore prepares a Store at sessionsFile, creating its directory.
func OpenStore(sessionsFile string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(sessionsFile), 0o755); err != nil {
		return nil, err
	}
	return &Store{path: sessionsFile, lock: flock.New(sessionsFile + ".lock")}, nil
}

func (s *Store) load() (File, error) {
	f := File{Version: 1, Sessions: map[string]Info{}}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil
		}
		return f, err
	}
	if len(b) == 0 {
		return f, nil
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, err
	}
	if f.Sessions == nil {
		f.Sessions = map[string]Info{}
	}
	return f, nil
}

func (s *Store) save(f File) error {
	f.UpdatedAt = time.Now().UTC()
	if f.Version == 0 {
		f.Version = 1
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) withLock(fn func(*File) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock.Lock(); err != nil {
		return err
	}
	defer func() { _ = s.lock.Unlock() }()
	f, err := s.load()
	if err != nil {
		return err
	}
	if err := fn(&f); err != nil {
		return err
	}
	return s.save(f)
}

// Load returns the whole registry under a shared lock.
func (s *Store) Load() (File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.lock.RLock(); err != nil {
		return File{}, err
	}
	defer func() { _ = s.lock.Unlock() }()
	return s.load()
}

// Upsert inserts or replaces a session.
func (s *Store) Upsert(info Info) error {
	return s.withLock(func(f *File) error {
		info.UpdatedAt = time.Now().UTC()
		f.Sessions[info.Name] = info
		return nil
	})
}

// Patch mutates an existing session in place.
func (s *Store) Patch(name string, mut func(*Info)) error {
	return s.withLock(func(f *File) error {
		info, ok := f.Sessions[name]
		if !ok {
			return ErrNotFound
		}
		mut(&info)
		info.UpdatedAt = time.Now().UTC()
		f.Sessions[name] = info
		return nil
	})
}

// Delete removes a session.
func (s *Store) Delete(name string) error {
	return s.withLock(func(f *File) error {
		delete(f.Sessions, name)
		return nil
	})
}

// All returns every session.
func (s *Store) All() ([]Info, error) {
	f, err := s.Load()
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(f.Sessions))
	for _, i := range f.Sessions {
		out = append(out, i)
	}
	return out, nil
}

// Get returns one session or ErrNotFound.
func (s *Store) Get(name string) (Info, error) {
	f, err := s.Load()
	if err != nil {
		return Info{}, err
	}
	i, ok := f.Sessions[name]
	if !ok {
		return Info{}, ErrNotFound
	}
	return i, nil
}
