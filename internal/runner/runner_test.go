package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	probeErr  error
	probeURLs []string
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

func (s *stubGit) ProbeRemote(_ context.Context, url string) error {
	s.probeURLs = append(s.probeURLs, url)
	return s.probeErr
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

func TestSyncOneCountsConsecutiveFailures(t *testing.T) {
	t.Parallel()

	git := &stubGit{result: gitops.Result{Action: "fetch failed", Error: "网络中断"}}
	target, guard := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	for round := 1; round <= remoteMissingThreshold; round++ {
		if err := target.SyncOnce(context.Background()); err == nil {
			t.Fatalf("第 %d 轮 SyncOnce() error = nil, want failure", round)
		}
		if got := guard.Get("example"); got.ConsecutiveFailures != round || got.LastError != "网络中断" {
			t.Fatalf("第 %d 轮 state = %#v", round, got)
		}
	}

	// 普通失败不是「远端消失」，不能把仓库判死。
	if guard.Get("example").RemoteMissing {
		t.Fatal("普通失败不应标记远端消失")
	}
}

func TestSyncOneMarksRemoteMissingAfterThreshold(t *testing.T) {
	t.Parallel()

	git := &stubGit{result: gitops.Result{
		Action:            "remote check failed",
		Error:             "remote repository unavailable: Repository not found",
		RemoteUnavailable: true,
	}}
	target, guard := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	for round := 1; round < remoteMissingThreshold; round++ {
		if err := target.SyncOnce(context.Background()); err == nil {
			t.Fatalf("第 %d 轮 SyncOnce() error = nil, want failure", round)
		}
		if guard.Get("example").RemoteMissing {
			t.Fatalf("第 %d 轮就标记了远端消失，阈值是 %d", round, remoteMissingThreshold)
		}
	}

	if err := target.SyncOnce(context.Background()); err == nil {
		t.Fatal("最后一轮 SyncOnce() error = nil, want failure")
	}
	stateOfRepo := guard.Get("example")
	if !stateOfRepo.RemoteMissing {
		t.Fatalf("state = %#v, want RemoteMissing", stateOfRepo)
	}
	if stateOfRepo.RemoteMissingAt.IsZero() {
		t.Fatal("RemoteMissingAt 未记录")
	}
	if stateOfRepo.ConsecutiveFailures != remoteMissingThreshold {
		t.Fatalf("ConsecutiveFailures = %d, want %d", stateOfRepo.ConsecutiveFailures, remoteMissingThreshold)
	}
}

func TestSyncOneSkipsRemoteMissingRepository(t *testing.T) {
	t.Parallel()

	git := &stubGit{result: gitops.Result{Action: "updated"}}
	target, guard := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	if err := guard.Put("example", state.RepoState{
		ConsecutiveFailures: remoteMissingThreshold,
		LastError:           "Repository not found",
		RemoteMissing:       true,
		RemoteMissingAt:     time.Now(),
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	result, err := target.SyncOne(context.Background(), "example")
	if err != nil {
		t.Fatalf("SyncOne() error = %v", err)
	}
	if result.Action != "missing" {
		t.Fatalf("Action = %q, want missing", result.Action)
	}
	if result.Error == "" {
		t.Fatal("missing 结果必须说明原因")
	}
	if git.syncCalls != 0 {
		t.Fatalf("syncCalls = %d, want 0", git.syncCalls)
	}
}

func TestSyncOneClearsFailureStateOnSuccess(t *testing.T) {
	t.Parallel()

	git := &stubGit{result: gitops.Result{Action: "skipped"}}
	target, guard := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	if err := guard.Put("example", state.RepoState{ConsecutiveFailures: 2, LastError: "网络中断"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if err := target.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce() error = %v", err)
	}

	stateOfRepo := guard.Get("example")
	if stateOfRepo.ConsecutiveFailures != 0 || stateOfRepo.LastError != "" {
		t.Fatalf("state = %#v, want cleared", stateOfRepo)
	}
	if stateOfRepo.RemoteMissing || !stateOfRepo.RemoteMissingAt.IsZero() {
		t.Fatalf("state = %#v, want RemoteMissing cleared", stateOfRepo)
	}
}

func TestSyncOnePicksUpUnblockedStateFromDisk(t *testing.T) {
	t.Parallel()

	git := &stubGit{result: gitops.Result{Action: "skipped"}}
	target, guard := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	if err := guard.Put("example", state.RepoState{
		ConsecutiveFailures: remoteMissingThreshold,
		RemoteMissing:       true,
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	// repo unblock 由另一个进程直接改盘，服务重新加载后才会继续同步。
	statePath := filepath.Join(target.cfg.DataRoot, state.FileName)
	if err := os.WriteFile(statePath, []byte(`{"repositories":{}}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	if err := target.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce() error = %v", err)
	}
	if git.syncCalls != 1 {
		t.Fatalf("syncCalls = %d, want 1（解除后应重新同步）", git.syncCalls)
	}
}

func TestCheckRemoteReportsReachability(t *testing.T) {
	t.Parallel()

	git := &stubGit{}
	target, _ := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	status, err := target.CheckRemote(context.Background(), "example")
	if err != nil {
		t.Fatalf("CheckRemote() error = %v", err)
	}
	if !status.Reachable || status.Repo != "example" || status.Error != "" {
		t.Fatalf("status = %#v", status)
	}
	if len(git.probeURLs) != 1 || git.probeURLs[0] != "https://example.com/a.git" {
		t.Fatalf("probeURLs = %#v", git.probeURLs)
	}
}

func TestCheckRemoteReportsUnavailableRemote(t *testing.T) {
	t.Parallel()

	probeErr := fmt.Errorf("%w: Repository not found", gitops.ErrRemoteUnavailable)
	git := &stubGit{probeErr: probeErr}
	target, _ := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	status, err := target.CheckRemote(context.Background(), "example")
	if err != nil {
		t.Fatalf("CheckRemote() error = %v", err)
	}
	if status.Reachable {
		t.Fatalf("status = %#v, want unreachable", status)
	}
	if !strings.Contains(status.Error, "Repository not found") {
		t.Fatalf("Error = %q", status.Error)
	}
}

func TestCheckRemoteRejectsUnknownRepository(t *testing.T) {
	t.Parallel()

	git := &stubGit{}
	target, _ := newTestRunner(t, git, []repoindex.Repository{{Name: "example", URL: "https://example.com/a.git"}})

	if _, err := target.CheckRemote(context.Background(), "nope"); !errors.Is(err, ErrRepoNotManaged) {
		t.Fatalf("CheckRemote() error = %v, want ErrRepoNotManaged", err)
	}
	if len(git.probeURLs) != 0 {
		t.Fatalf("probeURLs = %#v, want empty", git.probeURLs)
	}
}
