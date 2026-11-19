package vault

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestTreeExpandsRequestedDepth(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	writeRepoFile(t, dir, "top.txt", "x")
	writeRepoFile(t, dir, "sub/b.txt", "x")
	writeRepoFile(t, dir, "sub/deep/c.txt", "x")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tree, err := v.Tree("example", TreeOptions{Depth: 2})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if tree.Depth != 2 {
		t.Fatalf("Depth = %d, want 2", tree.Depth)
	}

	got := make([]string, 0, len(tree.Entries))
	for _, entry := range tree.Entries {
		got = append(got, entry.Path)
	}
	// 深度优先：父目录后面紧跟它的子项。
	want := []string{"sub", "sub/deep", "sub/b.txt", "top.txt"}
	if len(got) != len(want) {
		t.Fatalf("entries = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("entries = %#v, want %#v", got, want)
		}
	}

	for _, entry := range tree.Entries {
		wantDepth := 1
		if entry.Path == "sub/deep" || entry.Path == "sub/b.txt" {
			wantDepth = 2
		}
		if entry.Depth != wantDepth {
			t.Fatalf("%s depth = %d, want %d", entry.Path, entry.Depth, wantDepth)
		}
	}
}

func TestTreeDefaultDepthIsOneLevel(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	writeRepoFile(t, dir, "sub/b.txt", "x")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tree, err := v.Tree("example", TreeOptions{})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if tree.Depth != DefaultTreeDepth {
		t.Fatalf("Depth = %d, want %d", tree.Depth, DefaultTreeDepth)
	}
	if len(tree.Entries) != 1 || tree.Entries[0].Path != "sub" {
		t.Fatalf("entries = %#v", tree.Entries)
	}
}

func TestTreeDirsOnlySkipsFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	writeRepoFile(t, dir, "top.txt", "x")
	writeRepoFile(t, dir, "sub/b.txt", "x")
	writeRepoFile(t, dir, "sub/deep/c.txt", "x")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tree, err := v.Tree("example", TreeOptions{Depth: 3, DirsOnly: true})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}

	got := make([]string, 0, len(tree.Entries))
	for _, entry := range tree.Entries {
		if entry.Type != "dir" {
			t.Fatalf("非目录混进结果: %#v", entry)
		}
		got = append(got, entry.Path)
	}
	if len(got) != 2 || got[0] != "sub" || got[1] != "sub/deep" {
		t.Fatalf("entries = %#v", got)
	}
}

func TestTreeReportsTruncation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	writeRepoFile(t, dir, "a.txt", "x")
	writeRepoFile(t, dir, "b.txt", "x")
	writeRepoFile(t, dir, "c.txt", "x")

	limits := DefaultLimits()
	limits.MaxTreeEntries = 2
	v, err := New(root, []string{"example"}, limits)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tree, err := v.Tree("example", TreeOptions{})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if !tree.Truncated {
		t.Fatal("超出条目上限时应标记截断")
	}
	if len(tree.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(tree.Entries))
	}
}

func TestTreeRejectsFileTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, "example"), "a.txt", "x")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := v.Tree("example", TreeOptions{Path: "a.txt"}); !errors.Is(err, ErrNotRegular) {
		t.Fatalf("Tree(file) error = %v, want ErrNotRegular", err)
	}
}
