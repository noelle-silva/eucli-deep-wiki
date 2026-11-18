package status

import (
	"sync"
	"time"

	"eucli-deep-wiki/internal/gitops"
)

type Snapshot struct {
	Running bool `json:"running"`
	Total   int  `json:"total"`
	// Summary 按动作统计结果条数，由 Snapshot() 从 Results 推导。
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

// Snapshot 返回状态副本；summary 由已记录的结果推导，
// 因此运行中也能看到已处理部分的汇总，结束后自然是全量汇总。
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	copied := s.snapshot
	copied.Results = append([]gitops.Result(nil), s.snapshot.Results...)
	copied.SingleResults = append([]gitops.Result(nil), s.snapshot.SingleResults...)
	copied.Summary = summarize(copied.Results)
	return copied
}

// summarize 按动作统计结果条数。
func summarize(results []gitops.Result) map[string]int {
	summary := make(map[string]int, len(results))
	for _, result := range results {
		summary[result.Action]++
	}
	return summary
}
