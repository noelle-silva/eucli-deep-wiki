package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenMissingFileStartsEmpty(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if len(store.Snapshot()) != 0 {
		t.Fatalf("Snapshot() = %#v", store.Snapshot())
	}
}

func TestPutPersistsAcrossReopen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), FileName)
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := store.Put("example", RepoState{LastGCSize: 1024, Blocked: true, BlockedSize: 2048}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	got := reopened.Get("example")
	if got.LastGCSize != 1024 || !got.Blocked || got.BlockedSize != 2048 {
		t.Fatalf("Get() = %#v", got)
	}
}

func TestReloadPicksUpExternalChange(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), FileName)
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// 模拟另一个进程（repo unblock）直接改盘。
	external := `{"repositories":{"example":{"consecutiveFailures":3,"remoteMissing":true}}}`
	if err := os.WriteFile(path, []byte(external), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	if err := store.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if got := store.Get("example"); !got.RemoteMissing {
		t.Fatalf("Get() = %#v, want refreshed state", got)
	}

	if err := os.WriteFile(path, []byte(`not json`), 0o600); err != nil {
		t.Fatalf("write broken state: %v", err)
	}
	if err := store.Reload(); err == nil {
		t.Fatal("Reload() error = nil, want parse error")
	}
	// 读坏了也不能把内存里的状态丢掉。
	if got := store.Get("example"); !got.RemoteMissing {
		t.Fatalf("Get() = %#v, want previous state kept", got)
	}
}

func TestUpdateReadsPreviousValue(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := store.Put("example", RepoState{LastGCSize: 500}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if err := store.Update("example", func(current RepoState) RepoState {
		current.LastGCSize += 100
		return current
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if got := store.Get("example").LastGCSize; got != 600 {
		t.Fatalf("LastGCSize = %d, want 600", got)
	}
}

func TestPutPersistsRemoteStatus(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), FileName)
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	missingAt := time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := store.Put("example", RepoState{
		ConsecutiveFailures: 3,
		LastError:           "remote repository unavailable: Repository not found",
		RemoteMissing:       true,
		RemoteMissingAt:     missingAt,
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}

	got := reopened.Get("example")
	if got.ConsecutiveFailures != 3 {
		t.Fatalf("ConsecutiveFailures = %d, want 3", got.ConsecutiveFailures)
	}
	if !strings.Contains(got.LastError, "Repository not found") {
		t.Fatalf("LastError = %q", got.LastError)
	}
	if !got.RemoteMissing || !got.RemoteMissingAt.Equal(missingAt) {
		t.Fatalf("Get() = %#v", got)
	}
}
