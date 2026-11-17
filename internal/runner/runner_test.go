package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"eucli-deep-wiki/internal/config"
	"eucli-deep-wiki/internal/gitops"
	"eucli-deep-wiki/internal/repoindex"
	"eucli-deep-wiki/internal/state"
	"eucli-deep-wiki/internal/status"
)

type stubGit struct {
	result    gitops.Result
	size      int64
	sizeAfter int64
	gcCalls   int
	syncCalls int
}

func (s *stubGit) Sync(_ context.Context, repo gitops.Repository) gitops.Result {
	s.syncCalls++
	result := s.result
	result.Name = repo.Name
	result.URL = repo.URL
	return result
}

func (s *stubGit) GarbageCollect(_ context.Context, _ string) error {
	s.gcCalls++
	if s.sizeAfter > 0 {
		s.size = s.sizeAfter
	}
	return nil
}

func (s *stubGit) GitDirectorySize(_ string) (int64, error) {
	return s.size, nil
}

func newTestRunner(t *testing.T, git *stubGit, repos []repoindex.Repository) (*Runner, *state.Store) {
	t.Helper()

	dataDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"dataRoot": "` + filepath.ToSlash(dataDir) + `",
		"apiKeys": ["0123456789abcdef"],
		"gcThresholdMB": 10,
		"gcGrowthMB": 5,
		"sizeLimitMB": 100
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}

	guard, err := state.Open(filepath.Join(dataDir, state.FileName))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}

	return New(cfg, repos, git, status.NewStore(), guard), guard
}

func TestSyncOneSkipsBlockedRepository(t *testing.T) {
	t.Parallel()

	git := &stubGit{result: gitops.Result{Action: "skipped"}}
	target, guard := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	if err := guard.Put("example", state.RepoState{Blocked: true, BlockedSize: 1 << 30}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if err := target.SyncOnce(context.Background()); err == nil {
		t.Fatal("SyncOnce() error = nil, want blocked error")
	}
	if git.syncCalls != 0 {
		t.Fatalf("syncCalls = %d, want 0", git.syncCalls)
	}
}

func TestSyncOneRunsGarbageCollectAboveThreshold(t *testing.T) {
	t.Parallel()

	git := &stubGit{
		result:    gitops.Result{Action: "skipped"},
		size:      20 << 20,
		sizeAfter: 8 << 20,
	}
	target, guard := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	if err := target.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce() error = %v", err)
	}
	if git.gcCalls != 1 {
		t.Fatalf("gcCalls = %d, want 1", git.gcCalls)
	}
	if got := guard.Get("example").LastGCSize; got != 8<<20 {
		t.Fatalf("LastGCSize = %d, want %d", got, 8<<20)
	}
}

func TestSyncOneSkipsGarbageCollectWithoutGrowth(t *testing.T) {
	t.Parallel()

	git := &stubGit{result: gitops.Result{Action: "skipped"}, size: 12 << 20}
	target, guard := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	if err := guard.Put("example", state.RepoState{LastGCSize: 10 << 20}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if err := target.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce() error = %v", err)
	}
	if git.gcCalls != 0 {
		t.Fatalf("gcCalls = %d, want 0", git.gcCalls)
	}
}

func TestSyncOneBlocksWhenSizeLimitExceeded(t *testing.T) {
	t.Parallel()

	git := &stubGit{result: gitops.Result{Action: "skipped"}, size: 200 << 20}
	target, guard := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	if err := target.SyncOnce(context.Background()); err == nil {
		t.Fatal("SyncOnce() error = nil, want size limit error")
	}
	if !guard.Get("example").Blocked {
		t.Fatal("仓库未标记为停止同步")
	}
}

func TestSyncOneRejectsUnknownRepository(t *testing.T) {
	t.Parallel()

	git := &stubGit{result: gitops.Result{Action: "skipped"}}
	target, _ := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	if _, err := target.SyncOne(context.Background(), "nope"); !errors.Is(err, ErrRepoNotManaged) {
		t.Fatalf("SyncOne() error = %v, want ErrRepoNotManaged", err)
	}
}

func TestSyncOneReturnsResult(t *testing.T) {
	t.Parallel()

	git := &stubGit{result: gitops.Result{Action: "updated", Branch: "main"}}
	target, _ := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	result, err := target.SyncOne(context.Background(), "example")
	if err != nil {
		t.Fatalf("SyncOne() error = %v", err)
	}
	if result.Action != "updated" || result.Name != "example" {
		t.Fatalf("result = %#v", result)
	}
	if git.syncCalls != 1 {
		t.Fatalf("syncCalls = %d, want 1", git.syncCalls)
	}
}

func TestSyncOneRecordsSingleResult(t *testing.T) {
	t.Parallel()

	git := &stubGit{result: gitops.Result{Action: "skipped"}}
	target, _ := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	if _, err := target.SyncOne(context.Background(), "example"); err != nil {
		t.Fatalf("SyncOne() error = %v", err)
	}

	snapshot := target.store.Snapshot()
	if len(snapshot.SingleResults) != 1 || snapshot.SingleResults[0].Name != "example" {
		t.Fatalf("SingleResults = %#v", snapshot.SingleResults)
	}
}
