package status

import (
	"sync"
	"time"

	"eucli-deep-wiki/internal/gitops"
)

type Snapshot struct {
	Running        bool            `json:"running"`
	Total          int             `json:"total"`
	Summary        map[string]int  `json:"summary"`
	LastStartedAt  time.Time       `json:"lastStartedAt,omitempty"`
	LastFinishedAt time.Time       `json:"lastFinishedAt,omitempty"`
	NextRunAt      time.Time       `json:"nextRunAt,omitempty"`
	Results        []gitops.Result `json:"results"`
	SingleResults  []gitops.Result `json:"singleResults"`
	LastSingleAt   time.Time       `json:"lastSingleAt,omitempty"`
}

type Store struct {
	mu        sync.RWMutex
	snapshot  Snapshot
	nextRunAt time.Time
}

func NewStore() *Store {
	return &Store{}
}

func (s *Store) Begin(startedAt time.Time, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Running = true
	s.snapshot.LastStartedAt = startedAt
	s.snapshot.Results = nil
	s.snapshot.Total = total
	s.snapshot.Summary = nil
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

	summary := make(map[string]int, len(s.snapshot.Results))
	for _, result := range s.snapshot.Results {
		summary[result.Action]++
	}
	s.snapshot.Summary = summary
}

// RecordSingle 记录一次按需同步，供 /status 反映「刚才那次到底成没成」。
func (s *Store) RecordSingle(result gitops.Result) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshot.LastSingleAt = result.FinishedAt
	s.snapshot.SingleResults = append(s.snapshot.SingleResults, result)
	const keep = 20
	if len(s.snapshot.SingleResults) > keep {
		s.snapshot.SingleResults = s.snapshot.SingleResults[len(s.snapshot.SingleResults)-keep:]
	}
}

// SetNextRun 记录下一轮定时同步的时间。
func (s *Store) SetNextRun(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextRunAt = at
	s.snapshot.NextRunAt = at
}

func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	copied := s.snapshot
	copied.Results = append([]gitops.Result(nil), s.snapshot.Results...)
	copied.SingleResults = append([]gitops.Result(nil), s.snapshot.SingleResults...)
	return copied
}
