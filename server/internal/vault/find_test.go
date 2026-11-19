package vault

import (
	"errors"
	"testing"
)

func TestNormalizeFindLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input int
		want  int
	}{
		{name: "未指定", input: 0, want: DefaultFindLimit},
		{name: "负数", input: -1, want: DefaultFindLimit},
		{name: "正常值", input: 500, want: 500},
		{name: "超过上限", input: MaxFindLimit + 1, want: MaxFindLimit},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := NormalizeFindLimit(testCase.input); got != testCase.want {
				t.Fatalf("NormalizeFindLimit(%d) = %d, want %d", testCase.input, got, testCase.want)
			}
		})
	}
}

func TestFindHonoursLimitAndReportsTruncation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, root, "example/a.md", "x")
	writeRepoFile(t, root, "example/b.md", "x")
	writeRepoFile(t, root, "example/c.md", "x")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := v.Find("example", FindOptions{Pattern: "*.md", Limit: 2})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("entries = %#v", result.Entries)
	}
	if !result.Truncated {
		t.Fatal("还有未返回的命中时应标记截断")
	}
}

func TestFindDefaultsToExactName(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, root, "example/main.go", "x")
	writeRepoFile(t, root, "example/main_test.go", "x")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	exact, err := v.Find("example", FindOptions{Pattern: "MAIN.GO"})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if len(exact.Entries) != 1 || exact.Entries[0].Name != "main.go" {
		t.Fatalf("精确匹配 entries = %#v", exact.Entries)
	}

	substring, err := v.Find("example", FindOptions{Pattern: "main", Match: NameMatchSubstring})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if len(substring.Entries) != 2 {
		t.Fatalf("子串匹配 entries = %#v", substring.Entries)
	}
}

func TestFindRejectsBrokenGlob(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, root, "example/a.md", "x")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := v.Find("example", FindOptions{Pattern: "[abc"}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("Find(坏通配) error = %v, want ErrInvalidName", err)
	}
}

func TestFindPathIsSearchScope(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, root, "example/docs/a.md", "x")
	writeRepoFile(t, root, "example/docs/deep/b.md", "x")
	writeRepoFile(t, root, "example/src/a.md", "x")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := v.Find("example", FindOptions{Pattern: "*.md", Path: "docs/deep"})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if result.Path != "docs/deep" {
		t.Fatalf("Path = %q, want docs/deep", result.Path)
	}
	if len(result.Entries) != 1 || result.Entries[0].Path != "docs/deep/b.md" {
		t.Fatalf("entries = %#v", result.Entries)
	}
}
