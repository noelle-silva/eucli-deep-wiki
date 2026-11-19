package ops

import (
	"context"
	"testing"

	"eucli-deep-wiki-server/internal/repoindex"
)

func checkFixture(t *testing.T) *testFixture {
	t.Helper()

	return newFixture(t, []repoindex.Repository{
		{Name: "alive", URL: "https://github.com/example/alive.git"},
		{Name: "gone", URL: "https://github.com/example/gone.git"},
	}, repoindex.HistoryShallow)
}

func TestCheckProbesExplicitTargetsInOrder(t *testing.T) {
	t.Parallel()

	fixture := checkFixture(t)
	git := &stubGit{probeErr: map[string]error{
		"https://github.com/example/gone.git": errProbeFailed,
	}}

	result := fixture.manager(git, nil).Check(context.Background(), CheckOptions{Names: []string{"alive", "gone", "ghost"}})

	if len(result.Repositories) != 3 {
		t.Fatalf("Repositories = %#v", result.Repositories)
	}
	// 顺序与请求一致，与哪个探测先完成无关。
	if result.Repositories[0].Name != "alive" || result.Repositories[1].Name != "gone" || result.Repositories[2].Name != "ghost" {
		t.Fatalf("顺序 = %#v", result.Repositories)
	}
	if !result.Repositories[0].Reachable || result.Repositories[0].Error != "" {
		t.Fatalf("alive = %#v", result.Repositories[0])
	}
	if result.Repositories[1].Reachable || result.Repositories[1].Error == "" {
		t.Fatalf("gone = %#v", result.Repositories[1])
	}
	if !result.Repositories[2].Missing || result.Repositories[2].Reachable {
		t.Fatalf("不在名单里的仓库必须单独表达: %#v", result.Repositories[2])
	}

	if result.Reachable != 1 || result.Unreachable != 1 || result.Missing != 1 {
		t.Fatalf("统计 = %#v", result)
	}
	// 不在名单的仓库不该被拿去探测：没有地址可探。
	if len(git.probed) != 2 {
		t.Fatalf("probed = %#v", git.probed)
	}
}

func TestCheckCoversWholeIndexByDefault(t *testing.T) {
	t.Parallel()

	fixture := checkFixture(t)
	git := &stubGit{}

	result := fixture.manager(git, nil).Check(context.Background(), CheckOptions{})

	if len(result.Repositories) != 2 || result.Reachable != 2 {
		t.Fatalf("result = %#v", result)
	}
	// 名单顺序即结果顺序。
	if result.Repositories[0].Name != "alive" || result.Repositories[1].Name != "gone" {
		t.Fatalf("顺序 = %#v", result.Repositories)
	}
}

func TestCheckOnEmptyIndexChecksNothing(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, nil, repoindex.HistoryShallow)
	git := &stubGit{}

	result := fixture.manager(git, nil).Check(context.Background(), CheckOptions{})

	if len(result.Repositories) != 0 || len(git.probed) != 0 {
		t.Fatalf("result = %#v, probed = %#v", result, git.probed)
	}
}
