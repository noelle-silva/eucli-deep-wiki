package ops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"eucli-deep-wiki-server/internal/repoindex"
)

// 默认只摘名单：磁盘上的副本留着，调用方据此回显「数据保留」。
func TestRemoveKeepsDataByDefault(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
		{Name: "other", URL: "https://github.com/example/other.git"},
	}, repoindex.HistoryShallow)
	fixture.markOnDisk(t, "example", "other")

	result, err := fixture.manager(&stubGit{}, nil).Remove("example", RemoveOptions{KeepData: true})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if !result.Removed || !result.DataExisted || !result.DataKept {
		t.Fatalf("result = %#v", result)
	}

	index := fixture.storedIndex(t)
	if len(index.Repositories) != 1 || index.Repositories[0].Name != "other" {
		t.Fatalf("落盘的名单 = %#v", index.Repositories)
	}
	if _, err := os.Stat(filepath.Join(fixture.repositoryDir, "example")); err != nil {
		t.Fatalf("数据应保留: %v", err)
	}
}

func TestRemoveDeletesDataWhenAsked(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
	}, repoindex.HistoryShallow)
	fixture.markOnDisk(t, "example")

	result, err := fixture.manager(&stubGit{}, nil).Remove("example", RemoveOptions{})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if !result.Removed || !result.DataExisted || result.DataKept {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(fixture.repositoryDir, "example")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("数据应已删除: %v", err)
	}
	if len(fixture.storedIndex(t).Repositories) != 0 {
		t.Fatal("名单应已摘除")
	}
}

// 还没落盘的仓库也能摘：名单里有它，磁盘上没有副本，摘掉就是摘掉。
func TestRemoveWorksForRepositoryNotOnDisk(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
	}, repoindex.HistoryShallow)

	result, err := fixture.manager(&stubGit{}, nil).Remove("example", RemoveOptions{})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if !result.Removed || result.DataExisted || result.DataKept {
		t.Fatalf("result = %#v", result)
	}
	if len(fixture.storedIndex(t).Repositories) != 0 {
		t.Fatal("名单应已摘除")
	}
}

func TestRemoveRejectsRepositoryOutsideIndex(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
	}, repoindex.HistoryShallow)

	if _, err := fixture.manager(&stubGit{}, nil).Remove("ghost", RemoveOptions{}); !errors.Is(err, ErrRepositoryNotFound) {
		t.Fatalf("Remove() error = %v, want ErrRepositoryNotFound", err)
	}
	if len(fixture.storedIndex(t).Repositories) != 1 {
		t.Fatal("失败的摘除不该改动名单")
	}
}

// 名单写盘失败时如实报告，而且它不是「用法错误」：接口层据此给出「写盘失败」。
func TestRemoveReportsIndexWriteFailure(t *testing.T) {
	t.Parallel()

	fixture := newFixture(t, []repoindex.Repository{
		{Name: "example", URL: "https://github.com/example/example.git"},
	}, repoindex.HistoryShallow)
	fixture.markOnDisk(t, "example")

	// 占住写盘用的临时文件名，让落盘失败；读盘不受影响。
	if err := os.MkdirAll(fixture.indexPath+".tmp", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := fixture.manager(&stubGit{}, nil).Remove("example", RemoveOptions{KeepData: true})
	if err == nil {
		t.Fatal("写盘失败必须报错")
	}
	for _, validation := range []error{
		repoindex.ErrInvalidURL,
		repoindex.ErrInvalidName,
		repoindex.ErrInvalidBranch,
		repoindex.ErrInvalidHistory,
		repoindex.ErrDuplicateName,
	} {
		if errors.Is(err, validation) {
			t.Fatalf("Remove() error = %v，写盘失败不该被当成校验失败", err)
		}
	}
}
