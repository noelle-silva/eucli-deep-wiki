package ops

import (
	"errors"
	"testing"

	"eucli-deep-wiki-server/internal/repoindex"
)

func TestListReportsIndexAndDiskFacts(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
		{Name: "history", URL: "https://github.com/example/history.git", History: repoindex.HistoryFull},
		{Name: "pending", URL: "https://github.com/example/pending.git"},
	}, repoindex.HistoryShallow)
	fixture.markOnDisk(t, "example", "history")

	listed := fixture.manager(&stubGit{}, nil).List(ListOptions{})

	if len(listed) != 3 {
		t.Fatalf("listed = %#v", listed)
	}
	example := listed[0]
	if example.Name != "example" || example.URL != "https://github.com/example/example.git" {
		t.Fatalf("example = %#v", example)
	}
	if example.History != "" || example.EffectiveHistory != repoindex.HistoryShallow {
		t.Fatalf("example history = %q / %q", example.History, example.EffectiveHistory)
	}
	if !example.OnDisk {
		t.Fatalf("example 应已落盘: %#v", example)
	}
	if example.MeasuredBytes != nil {
		t.Fatalf("没要求测量时不该给体积: %#v", example.MeasuredBytes)
	}

	if listed[1].EffectiveHistory != repoindex.HistoryFull {
		t.Fatalf("history 仓库的生效档位 = %q", listed[1].EffectiveHistory)
	}
	if listed[2].OnDisk {
		t.Fatalf("pending 不该被判为已落盘: %#v", listed[2])
	}
}

// 顺序只按名字定：同一份名单，任何时候看到的都是同一个顺序。
func TestListOrdersByName(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "zulu", URL: "https://github.com/example/zulu.git"},
		{Name: "alpha", URL: "https://github.com/example/alpha.git"},
		{Name: "mike", URL: "https://github.com/example/mike.git"},
	}, repoindex.HistoryShallow)

	listed := fixture.manager(&stubGit{}, nil).List(ListOptions{})

	names := make([]string, len(listed))
	for index, item := range listed {
		names[index] = item.Name
	}
	if len(names) != 3 || names[0] != "alpha" || names[1] != "mike" || names[2] != "zulu" {
		t.Fatalf("顺序 = %v", names)
	}
}

func TestListMeasuresOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
		{Name: "pending", URL: "https://github.com/example/pending.git"},
	}, repoindex.HistoryShallow)
	fixture.markOnDisk(t, "example")

	git := &stubGit{size: 4096}
	listed := fixture.manager(git, nil).List(ListOptions{Measure: true})

	if len(listed) != 2 || listed[0].MeasuredBytes == nil {
		t.Fatalf("listed = %#v", listed)
	}
	if *listed[0].MeasuredBytes != 4096 {
		t.Fatalf("MeasuredBytes = %d", *listed[0].MeasuredBytes)
	}
	// 没落盘的仓库没有体积可测，就不给这个字段。
	if listed[1].MeasuredBytes != nil {
		t.Fatalf("未落盘的仓库不该给体积: %#v", listed[1])
	}

	// 测量失败时如实不给数字，而不是给一个 0 让人误以为仓库是空的。
	listed = fixture.manager(&stubGit{sizeErr: errors.New("boom")}, nil).List(ListOptions{Measure: true})
	if listed[0].MeasuredBytes != nil {
		t.Fatalf("测量失败不该给出体积: %#v", listed[0].MeasuredBytes)
	}
}
