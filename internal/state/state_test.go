package state

import (
	"path/filepath"
	"testing"
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
