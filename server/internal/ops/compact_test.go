package ops

import (
	"context"
	"errors"
	"testing"

	"eucli-deep-wiki-server/internal/gitops"
	"eucli-deep-wiki-server/internal/repoindex"
)

func compactFixture(t *testing.T, history string) *testFixture {
	t.Helper()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "example", URL: "https://github.com/example/example.git", History: history},
	}, repoindex.HistoryShallow)
	fixture.markOnDisk(t, "example")
	return fixture
}

func TestCompactConvertsCompleteRepository(t *testing.T) {
	t.Parallel()

	fixture := compactFixture(t, "")
	git := &stubGit{shallow: false, shallowAfter: true, size: 2048, sizeAfter: 512}

	result, err := fixture.manager(git, nil).Compact(context.Background(), "example", CompactOptions{})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	if result.Action != Compacted {
		t.Fatalf("Action = %q, want %q", result.Action, Compacted)
	}
	if result.Before != (RepositorySize{Shallow: false, GitBytes: 2048}) {
		t.Fatalf("Before = %#v", result.Before)
	}
	if result.After != (RepositorySize{Shallow: true, GitBytes: 512}) {
		t.Fatalf("After = %#v", result.After)
	}
	if len(git.compacted) != 1 {
		t.Fatalf("compacted = %#v", git.compacted)
	}
}

func TestCompactDryRunDoesNotTouchRepository(t *testing.T) {
	t.Parallel()

	fixture := compactFixture(t, "")
	git := &stubGit{shallow: false}

	result, err := fixture.manager(git, nil).Compact(context.Background(), "example", CompactOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if result.Action != WouldCompact {
		t.Fatalf("Action = %q, want %q", result.Action, WouldCompact)
	}
	if len(git.compacted) != 0 {
		t.Fatalf("预演不该动仓库: %#v", git.compacted)
	}
}

// 「本来就是浅克隆」和「名单要求完整历史」都是成功结论，不是错误。
func TestCompactReportsSkipOutcomes(t *testing.T) {
	t.Parallel()

	t.Run("本来就是浅克隆", func(t *testing.T) {
		t.Parallel()

		fixture := compactFixture(t, "")
		git := &stubGit{shallow: true}

		result, err := fixture.manager(git, nil).Compact(context.Background(), "example", CompactOptions{})
		if err != nil {
			t.Fatalf("Compact() error = %v", err)
		}
		if result.Action != AlreadyShallow {
			t.Fatalf("Action = %q", result.Action)
		}
		if len(git.compacted) != 0 {
			t.Fatal("浅克隆不该再转一次")
		}
	})

	t.Run("名单要求完整历史", func(t *testing.T) {
		t.Parallel()

		fixture := compactFixture(t, repoindex.HistoryFull)
		git := &stubGit{shallow: false}

		result, err := fixture.manager(git, nil).Compact(context.Background(), "example", CompactOptions{})
		if err != nil {
			t.Fatalf("Compact() error = %v", err)
		}
		if result.Action != SkippedFullHistory {
			t.Fatalf("Action = %q", result.Action)
		}
		if len(git.compacted) != 0 {
			t.Fatal("完整历史仓库不该被瘦身")
		}
	})
}

func TestCompactRejectsBusyAndUnknownTargets(t *testing.T) {
	t.Parallel()

	t.Run("同步正忙", func(t *testing.T) {
		t.Parallel()

		fixture := compactFixture(t, "")
		git := &stubGit{shallow: false}
		busy := func(func()) bool { return false }

		_, err := fixture.manager(git, busy).Compact(context.Background(), "example", CompactOptions{})
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("Compact() error = %v, want ErrBusy", err)
		}
		if len(git.compacted) != 0 {
			t.Fatal("忙的时候不该动仓库")
		}
	})

	t.Run("不在名单", func(t *testing.T) {
		t.Parallel()

		fixture := compactFixture(t, "")

		_, err := fixture.manager(&stubGit{}, nil).Compact(context.Background(), "ghost", CompactOptions{})
		if !errors.Is(err, ErrRepositoryNotFound) {
			t.Fatalf("Compact() error = %v, want ErrRepositoryNotFound", err)
		}
	})

	t.Run("磁盘上没有副本", func(t *testing.T) {
		t.Parallel()

		fixture := newFixture(t, []repoindex.Repository{
			{Name: "example", URL: "https://github.com/example/example.git"},
		}, repoindex.HistoryShallow)

		_, err := fixture.manager(&stubGit{}, nil).Compact(context.Background(), "example", CompactOptions{})
		if !errors.Is(err, ErrRepositoryNotOnDisk) {
			t.Fatalf("Compact() error = %v, want ErrRepositoryNotOnDisk", err)
		}
	})
}

// 三种「做不下去」各有自己的标识：调用方据此区分是状态不干净、远端不在、还是命令失败。
func TestCompactReportsGitFailuresWithIdentity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		git      *stubGit
		expected error
	}{
		{
			name:     "游离头指针",
			git:      &stubGit{shallow: false, compactErr: gitops.ErrDetachedHead},
			expected: gitops.ErrDetachedHead,
		},
		{
			name:     "远端不可达",
			git:      &stubGit{shallow: false, compactErr: gitops.ErrRemoteUnavailable},
			expected: gitops.ErrRemoteUnavailable,
		},
		{
			name:     "版本管理命令失败",
			git:      &stubGit{shallowErr: errors.New("git rev-parse: boom")},
			expected: nil,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fixture := compactFixture(t, "")

			_, err := fixture.manager(testCase.git, nil).Compact(context.Background(), "example", CompactOptions{})
			if err == nil {
				t.Fatal("失败必须报错")
			}
			if testCase.expected != nil && !errors.Is(err, testCase.expected) {
				t.Fatalf("Compact() error = %v, want %v", err, testCase.expected)
			}
		})
	}
}

func TestCompactReportsBranch(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "example", URL: "https://github.com/example/example.git", Branch: "main"},
	}, repoindex.HistoryShallow)
	fixture.markOnDisk(t, "example")
	git := &stubGit{shallow: true}

	// 磁盘上没有 HEAD 时退回名单里的声明：分支是名单的意图，不是猜出来的。
	result, err := fixture.manager(git, nil).Compact(context.Background(), "example", CompactOptions{})
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if result.Branch != "main" {
		t.Fatalf("Branch = %q, want main", result.Branch)
	}
}
