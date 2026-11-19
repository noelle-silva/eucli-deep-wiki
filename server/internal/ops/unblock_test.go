package ops

import (
	"errors"
	"testing"
	"time"

	"eucli-deep-wiki-server/internal/repoindex"
	"eucli-deep-wiki-server/internal/state"
)

func TestUnblockClearsMarkersAndKeepsSyncFacts(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
	}, repoindex.HistoryShallow)

	lastSyncAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	blockedAt := time.Date(2026, 3, 5, 6, 7, 8, 0, time.UTC)
	remoteMissingAt := time.Date(2026, 3, 6, 7, 8, 9, 0, time.UTC)
	if err := fixture.states.Put("example", state.RepoState{
		Blocked:             true,
		BlockedAt:           blockedAt,
		BlockedSize:         3 << 30,
		RemoteMissing:       true,
		RemoteMissingAt:     remoteMissingAt,
		LastSyncAt:          lastSyncAt,
		GitBytes:            1024,
		ConsecutiveFailures: 3,
		LastError:           "Repository not found",
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	result, err := fixture.manager(&stubGit{}, nil).Unblock("example", UnblockOptions{})
	if err != nil {
		t.Fatalf("Unblock() error = %v", err)
	}
	if !result.Cleared {
		t.Fatal("Cleared 应为 true")
	}
	if !result.Previous.Blocked || !result.Previous.RemoteMissing {
		t.Fatalf("Previous 应带上变更前的事实: %#v", result.Previous)
	}
	if result.Previous.BlockedSize != 3<<30 || result.Previous.ConsecutiveFailures != 3 || result.Previous.LastError == "" {
		t.Fatalf("Previous 应原样给出拦截前的数字与原因: %#v", result.Previous)
	}

	got := fixture.states.Get("example")
	if got.Blocked || !got.BlockedAt.IsZero() || got.BlockedSize != 0 {
		t.Fatalf("体积保护标记应被清掉: %#v", got)
	}
	if got.RemoteMissing || !got.RemoteMissingAt.IsZero() {
		t.Fatalf("远端消失标记应被清掉: %#v", got)
	}
	if !got.LastSyncAt.Equal(lastSyncAt) || got.GitBytes != 1024 {
		t.Fatalf("同步成功的事实必须保留: %#v", got)
	}
	if got.ConsecutiveFailures != 3 || got.LastError == "" {
		t.Fatalf("没要求清失败记录时不该动它: %#v", got)
	}
}

func TestUnblockCanClearFailureRecord(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
	}, repoindex.HistoryShallow)
	if err := fixture.states.Put("example", state.RepoState{
		Blocked:             true,
		ConsecutiveFailures: 3,
		LastError:           "Repository not found",
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if _, err := fixture.manager(&stubGit{}, nil).Unblock("example", UnblockOptions{ClearFailures: true}); err != nil {
		t.Fatalf("Unblock() error = %v", err)
	}

	got := fixture.states.Get("example")
	if got.ConsecutiveFailures != 0 || got.LastError != "" {
		t.Fatalf("失败记录应被清零: %#v", got)
	}
}

// 本来就没被拦是成功，不是错误：调用方据此给出「未处于停止同步状态」。
func TestUnblockWithoutMarkersIsSuccess(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
	}, repoindex.HistoryShallow)

	result, err := fixture.manager(&stubGit{}, nil).Unblock("example", UnblockOptions{})
	if err != nil {
		t.Fatalf("Unblock() error = %v", err)
	}
	if result.Cleared {
		t.Fatal("没有标记时 Cleared 应为 false")
	}
}

// 解除之后，下一轮同步的写回不能把它变回去：
// 写回基于同一份状态（记录一次失败只会累加失败次数），已清掉的标记不会被复活。
func TestUnblockSurvivesNextStateWriteBack(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
	}, repoindex.HistoryShallow)
	if err := fixture.states.Put("example", state.RepoState{
		Blocked:         true,
		BlockedAt:       time.Now(),
		BlockedSize:     4 << 30,
		RemoteMissing:   true,
		RemoteMissingAt: time.Now(),
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	if _, err := fixture.manager(&stubGit{}, nil).Unblock("example", UnblockOptions{ClearFailures: true}); err != nil {
		t.Fatalf("Unblock() error = %v", err)
	}

	// 同步器记录一次失败时就是这么写的：基于当前状态累加，再落盘。
	if err := fixture.states.Update("example", func(current state.RepoState) state.RepoState {
		current.ConsecutiveFailures++
		current.LastError = "临时网络故障"
		return current
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got := fixture.states.Get("example")
	if got.Blocked || got.RemoteMissing || !got.BlockedAt.IsZero() || !got.RemoteMissingAt.IsZero() {
		t.Fatalf("写回不该复活已解除的标记: %#v", got)
	}
}

func TestUnblockRejectsRepositoryOutsideIndex(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, nil, repoindex.HistoryShallow)

	if _, err := fixture.manager(&stubGit{}, nil).Unblock("ghost", UnblockOptions{}); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("Unblock() error = %v, want ErrRepositoryNotFound", err)
	}
}
