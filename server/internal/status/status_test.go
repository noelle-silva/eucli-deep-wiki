package status

import (
	"testing"
	"time"

	"eucli-deep-wiki-server/internal/gitops"
)

func TestSnapshotSummarizesRunningResults(t *testing.T) {
	t.Parallel()

	store := NewStore()
	store.Begin(time.Now(), 3)
	store.AddResult(gitops.Result{Name: "one", Action: "skipped"})
	store.AddResult(gitops.Result{Name: "two", Action: "updated"})
	store.AddResult(gitops.Result{Name: "three", Action: "skipped"})

	running := store.Snapshot()
	if !running.Running {
		t.Fatal("运行中的快照应标记 running")
	}
	if len(running.Summary) != 2 || running.Summary["skipped"] != 2 || running.Summary["updated"] != 1 {
		t.Fatalf("运行中 summary = %#v", running.Summary)
	}

	store.Finish(time.Now())
	finished := store.Snapshot()
	if finished.Running {
		t.Fatal("结束后的快照不应标记 running")
	}
	if len(finished.Summary) != 2 || finished.Summary["skipped"] != 2 || finished.Summary["updated"] != 1 {
		t.Fatalf("结束后 summary = %#v", finished.Summary)
	}
}

func TestSnapshotReturnsEmptySummaryBeforeAnyResult(t *testing.T) {
	t.Parallel()

	store := NewStore()
	snapshot := store.Snapshot()
	if snapshot.Summary == nil {
		t.Fatal("summary 不应为 null")
	}
	if len(snapshot.Summary) != 0 {
		t.Fatalf("summary = %#v", snapshot.Summary)
	}
}
