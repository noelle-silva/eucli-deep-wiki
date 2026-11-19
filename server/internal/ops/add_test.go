package ops

import (
	"errors"
	"testing"

	"eucli-deep-wiki-server/internal/live"
	"eucli-deep-wiki-server/internal/repoindex"
)

func TestAddWritesWholeBatch(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "existing", URL: "https://github.com/example/existing.git"},
	}, repoindex.HistoryShallow)

	git := &stubGit{}
	result, err := fixture.manager(git, nil).Add(AddOptions{
		URLs: []string{
			"https://github.com/example/first.git",
			"https://github.com/example/second.git",
		},
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if result.Repositories != 3 {
		t.Fatalf("Repositories = %d, want 3", result.Repositories)
	}
	if len(result.Added) != 2 {
		t.Fatalf("Added = %#v", result.Added)
	}
	if result.Added[0].Name != "first" || result.Added[1].Name != "second" {
		t.Fatalf("Added = %#v", result.Added)
	}
	if result.Added[0].EffectiveHistory != repoindex.HistoryShallow {
		t.Fatalf("EffectiveHistory = %q", result.Added[0].EffectiveHistory)
	}
	if result.Reloaded != nil {
		t.Fatalf("没要求立即生效时不该报告重载结果: %#v", result.Reloaded)
	}
	if fixture.source.reloadCount() != 0 {
		t.Fatal("没要求立即生效时不该触发重载")
	}

	index := fixture.storedIndex(t)
	if len(index.Repositories) != 3 || index.Repositories[2].Name != "second" {
		t.Fatalf("落盘的名单 = %#v", index.Repositories)
	}
}

// 整批原子：任何一条不合法，整批都不生效，磁盘上的名单保持原样；
// 而且「哪里不合法」靠标识区分，不靠文字。
func TestAddRejectsWholeBatchAndKeepsIndex(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		options  AddOptions
		expected error
	}{
		{
			name: "地址不合法",
			options: AddOptions{URLs: []string{
				"https://github.com/example/fresh.git",
				"https://github.com/example/broken.git?ref=main",
			}},
			expected: repoindex.ErrInvalidURL,
		},
		{
			name:     "取不出仓库名",
			options:  AddOptions{URLs: []string{"https://github.com/example/.git"}},
			expected: repoindex.ErrInvalidURL,
		},
		{
			name:     "推导出的名字不合法",
			options:  AddOptions{URLs: []string{"https://github.com/example/.."}},
			expected: repoindex.ErrInvalidName,
		},
		{
			name:     "历史档位不合法",
			options:  AddOptions{URLs: []string{"https://github.com/example/fresh.git"}, History: "deep"},
			expected: repoindex.ErrInvalidHistory,
		},
		{
			name:     "分支带空白",
			options:  AddOptions{URLs: []string{"https://github.com/example/fresh.git"}, Branch: " main "},
			expected: repoindex.ErrInvalidBranch,
		},
		{
			name:     "与已有仓库重名",
			options:  AddOptions{URLs: []string{"https://github.com/other/existing.git"}},
			expected: repoindex.ErrDuplicateName,
		},
		{
			name:     "一个地址都没有",
			options:  AddOptions{URLs: []string{"  "}},
			expected: ErrNoURLs,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fixture := newFixture(t, []repoindex.Repository{
				{Name: "existing", URL: "https://github.com/example/existing.git"},
			}, repoindex.HistoryShallow)

			if _, err := fixture.manager(&stubGit{}, nil).Add(testCase.options); !errors.Is(err, testCase.expected) {
				t.Fatalf("Add() error = %v, want %v", err, testCase.expected)
			}

			index := fixture.storedIndex(t)
			if len(index.Repositories) != 1 || index.Repositories[0].Name != "existing" {
				t.Fatalf("失败的批次不该改动名单: %#v", index.Repositories)
			}
			if fixture.source.reloadCount() != 0 {
				t.Fatal("失败的批次不该触发重载")
			}
		})
	}
}

func TestAddAppliesReloadWhenAsked(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, nil, repoindex.HistoryShallow)
	fixture.source.reload = live.Result{ConfigReloaded: true, IndexReloaded: true, Repositories: 1}

	result, err := fixture.manager(&stubGit{}, nil).Add(AddOptions{
		URLs:  []string{"https://github.com/example/fresh.git"},
		Apply: true,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if result.Reloaded == nil || !result.Reloaded.IndexReloaded {
		t.Fatalf("Reloaded = %#v", result.Reloaded)
	}
	if fixture.source.reloadCount() != 1 {
		t.Fatalf("reload 次数 = %d", fixture.source.reloadCount())
	}
}

// 名单已经写进盘、但立即生效失败时，两件事都要如实报告：
// 名单是真的写进去了，生效是真的没成，原因原样带回。
func TestAddReportsWhyItDidNotApply(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, nil, repoindex.HistoryShallow)
	fixture.source.reload = live.Result{Error: "配置加载失败（config.json）: invalid character"}

	result, err := fixture.manager(&stubGit{}, nil).Add(AddOptions{
		URLs:  []string{"https://github.com/example/fresh.git"},
		Apply: true,
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if result.Reloaded == nil {
		t.Fatal("要求立即生效时必须带回重载结果")
	}
	if result.Reloaded.IndexReloaded {
		t.Fatal("IndexReloaded 应为 false")
	}
	if result.Reloaded.Error != "配置加载失败（config.json）: invalid character" {
		t.Fatalf("Error = %q", result.Reloaded.Error)
	}
	if len(fixture.storedIndex(t).Repositories) != 1 {
		t.Fatal("名单应已写入")
	}
}
