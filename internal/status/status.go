package status

import (
	"sync"
	"time"

	"eucli-deep-wiki/internal/gitops"
)

type Snapshot struct {
	Running        bool            `json:"running"`
	LastStartedAt  time.Time       `json:"lastStartedAt,omitempty"`
	LastFinishedAt time.Time       `json:"lastFinishedAt,omitempty"`
	Results        []gitops.Result `json:"results"`
}

type Store struct {
	mu       sync.RWMutex
	snapshot Snapshot
}

func NewStore() *Store {
	return &Store{}
}

func (s *Store) Begin(startedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Running = true
	s.snapshot.LastStartedAt = startedAt
	s.snapshot.Results = nil
}

func (s *Store) AddResult(result gitops.Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Results = append(s.snapshot.Results, result)
}

func (s *Store) Finish(finishedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Running = false
	s.snapshot.LastFinishedAt = finishedAt
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	copy := s.snapshot
	copy.Results = append([]gitops.Result(nil), s.snapshot.Results...)
	return copy
}
