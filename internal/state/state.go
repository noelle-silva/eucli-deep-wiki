// Package state 记录每个参考仓库的体积保护状态与远端可用性。
//
// 文件保存在数据目录下，与仓库索引同级；内容只用于判断「要不要压缩」、
// 「是否已停止同步」与「远端是否已经消失」，删除后不影响仓库本身。
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileName 是状态文件名。
const FileName = "sync-state.json"

// RepoState 是单个仓库的体积记录与远端状态。
type RepoState struct {
	LastGCSize  int64     `json:"lastGCSize,omitempty"`
	LastGCAt    time.Time `json:"lastGCAt,omitempty"`
	Blocked     bool      `json:"blocked,omitempty"`
	BlockedAt   time.Time `json:"blockedAt,omitempty"`
	BlockedSize int64     `json:"blockedSize,omitempty"`

	// LastSyncAt 与 GitBytes 是同步成功那一刻的事实：何时同步、当时 .git 多大。
	LastSyncAt time.Time `json:"lastSyncAt,omitempty"`
	GitBytes   int64     `json:"gitBytes,omitempty"`

	// 连续同步失败的次数，成功一次即清零。
	ConsecutiveFailures int    `json:"consecutiveFailures,omitempty"`
	LastError           string `json:"lastError,omitempty"`

	// RemoteMissing 表示远端仓库已被判定不可用，同步会直接跳过。
	RemoteMissing   bool      `json:"remoteMissing,omitempty"`
	RemoteMissingAt time.Time `json:"remoteMissingAt,omitempty"`
}

// Store 是状态文件的读写入口。
type Store struct {
	path string
	mu   sync.Mutex
	data map[string]RepoState
}

type fileFormat struct {
	Repositories map[string]RepoState `json:"repositories"`
}

// Open 读取状态文件；文件不存在时返回空状态。
func Open(path string) (*Store, error) {
	data, err := load(path)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, data: data}, nil
}

// load 读出整份状态；文件不存在视为空状态。
func load(path string) (map[string]RepoState, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]RepoState{}, nil
	}
	if err != nil {
		return nil, err
	}

	var payload fileFormat
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if payload.Repositories == nil {
		return map[string]RepoState{}, nil
	}
	return payload.Repositories, nil
}

// Reload 重新读取状态文件，用于接纳另一个进程的修改：
// repo unblock 这类人工操作由命令行直接写盘，服务不重新加载就看不见。
func (s *Store) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := load(s.path)
	if err != nil {
		return err
	}
	s.data = data
	return nil
}

// Get 返回某个仓库的状态副本。
func (s *Store) Get(name string) RepoState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[name]
}

// Put 覆盖某个仓库的状态并落盘。
func (s *Store) Put(name string, value RepoState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[name] = value
	return s.saveLocked()
}

// Update 以回调方式修改状态，便于同时读旧值与写新值。
func (s *Store) Update(name string, mutate func(current RepoState) RepoState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[name] = mutate(s.data[name])
	return s.saveLocked()
}

// Snapshot 返回全部状态的副本。
func (s *Store) Snapshot() map[string]RepoState {
	s.mu.Lock()
	defer s.mu.Unlock()

	copied := make(map[string]RepoState, len(s.data))
	for name, value := range s.data {
		copied[name] = value
	}
	return copied
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	payload := fileFormat{Repositories: s.data}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, s.path)
}
