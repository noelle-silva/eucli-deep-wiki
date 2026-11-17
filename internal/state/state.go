// Package state 记录每个参考仓库的体积保护状态。
//
// 文件保存在数据目录下，与仓库索引同级；内容只用于判断「要不要压缩」
// 与「是否已停止同步」，删除后不影响仓库本身。
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

// RepoState 是单个仓库的体积记录。
type RepoState struct {
	LastGCSize  int64     `json:"lastGCSize,omitempty"`
	LastGCAt    time.Time `json:"lastGCAt,omitempty"`
	Blocked     bool      `json:"blocked,omitempty"`
	BlockedAt   time.Time `json:"blockedAt,omitempty"`
	BlockedSize int64     `json:"blockedSize,omitempty"`
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
	store := &Store{path: path, data: map[string]RepoState{}}

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}

	var payload fileFormat
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if payload.Repositories != nil {
		store.data = payload.Repositories
	}
	return store, nil
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
