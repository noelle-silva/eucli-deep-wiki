package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMetaReadsBranchAndRevision(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	writeRepoFile(t, dir, ".git/HEAD", "ref: refs/heads/main\n")
	writeRepoFile(t, dir, ".git/refs/heads/main", "0123456789abcdef0123456789abcdef01234567\n")
	writeRepoFile(t, dir, "README.md", "# hello\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	meta, err := v.Meta("example")
	if err != nil {
		t.Fatalf("Meta() error = %v", err)
	}
	if meta.Branch != "main" {
		t.Fatalf("Branch = %q", meta.Branch)
	}
	if meta.Revision != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("Revision = %q", meta.Revision)
	}
}

func TestMetaReadsPackedReference(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	writeRepoFile(t, dir, ".git/HEAD", "ref: refs/heads/dev\n")
	writeRepoFile(t, dir, ".git/packed-refs", "# pack-refs with: peeled\nfedcba9876543210fedcba9876543210fedcba98 refs/heads/dev\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	meta, err := v.Meta("example")
	if err != nil {
		t.Fatalf("Meta() error = %v", err)
	}
	if meta.Revision != "fedcba9876543210fedcba9876543210fedcba98" {
		t.Fatalf("Revision = %q", meta.Revision)
	}
}

func TestMetaRejectsUnknownRepository(t *testing.T) {
	t.Parallel()

	v, err := New(t.TempDir(), nil, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := v.Meta("missing"); !errors.Is(err, ErrRepoNotFound) {
		t.Fatalf("Meta() error = %v, want ErrRepoNotFound", err)
	}
}

func TestFileReadsTextAndBuildsETag(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, "example"), "docs/guide.md", "line one\nline two\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	file, err := v.File("example", "docs/guide.md")
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}
	if string(file.Content) != "line one\nline two\n" {
		t.Fatalf("Content = %q", string(file.Content))
	}
	if !strings.HasPrefix(file.ETag, `"`) || !strings.HasSuffix(file.ETag, `"`) {
		t.Fatalf("ETag = %q", file.ETag)
	}
}

func TestFileRejectsEscapingPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, "example"), "README.md", "hello\n")
	writeRepoFile(t, root, "secret.txt", "secret\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for _, requested := range []string{"../secret.txt", "/etc/passwd", `..\secret.txt`, "docs/../../secret.txt"} {
		if _, err := v.File("example", requested); !errors.Is(err, ErrInvalidPath) && !errors.Is(err, ErrNotFound) {
			t.Fatalf("File(%q) error = %v, want invalid path or not found", requested, err)
		}
	}
}

func TestFileRejectsBinaryAndOversized(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	writeRepoBytes(t, dir, "blob.bin", []byte{0x00, 0x01, 0x02})
	writeRepoFile(t, dir, "big.txt", strings.Repeat("a", 2048))

	limits := DefaultLimits()
	limits.MaxFileBytes = 1024
	v, err := New(root, []string{"example"}, limits)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := v.File("example", "blob.bin"); !errors.Is(err, ErrBinary) {
		t.Fatalf("File(blob) error = %v, want ErrBinary", err)
	}
	if _, err := v.File("example", "big.txt"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("File(big) error = %v, want ErrTooLarge", err)
	}
}

func TestTreeListsDirectoriesFirst(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	writeRepoFile(t, dir, "b.txt", "b\n")
	writeRepoFile(t, dir, "a/x.txt", "x\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tree, err := v.Tree("example", "")
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if len(tree.Entries) != 2 {
		t.Fatalf("entries = %d, want 2: %#v", len(tree.Entries), tree.Entries)
	}
	if tree.Entries[0].Type != "dir" || tree.Entries[0].Name != "a" {
		t.Fatalf("first entry = %#v", tree.Entries[0])
	}
	if tree.Entries[1].Path != "b.txt" {
		t.Fatalf("second entry = %#v", tree.Entries[1])
	}
}

func TestSearchSkipsGitDirectoryAndFindsLines(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	writeRepoFile(t, dir, "docs/a.md", "alpha\nneedle here\n")
	writeRepoFile(t, dir, ".git/config", "needle in git\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := v.Search("example", SearchOptions{Query: "needle"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("matches = %#v", result.Matches)
	}
	if result.Matches[0].Path != "docs/a.md" || result.Matches[0].Line != 2 {
		t.Fatalf("match = %#v", result.Matches[0])
	}
}

func TestSearchRejectsEmptyQuery(t *testing.T) {
	t.Parallel()

	v, err := New(t.TempDir(), []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := v.Search("example", SearchOptions{Query: "  "}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("Search() error = %v, want ErrInvalidQuery", err)
	}
}

func writeRepoFile(t *testing.T, dir string, name string, content string) {
	t.Helper()
	writeRepoBytes(t, dir, name, []byte(content))
}

func writeRepoBytes(t *testing.T, dir string, name string, content []byte) {
	t.Helper()

	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("make dir: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestSearchSupportsPathAndOffset(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, root, "example/docs/a.md", "needle one\nneedle two\nneedle three\n")
	writeRepoFile(t, root, "example/src/b.md", "needle outside\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	first, err := v.Search("example", SearchOptions{Query: "needle", Path: "docs", Limit: 2})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(first.Matches) != 2 || !first.Truncated {
		t.Fatalf("first = %#v", first)
	}
	for _, match := range first.Matches {
		if !strings.HasPrefix(match.Path, "docs/") {
			t.Fatalf("命中了目录外的文件: %#v", match)
		}
	}

	second, err := v.Search("example", SearchOptions{Query: "needle", Path: "docs", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("Search(offset) error = %v", err)
	}
	if len(second.Matches) != 1 || second.Matches[0].Line != 3 {
		t.Fatalf("second = %#v", second)
	}
	if second.Truncated {
		t.Fatal("最后一页不应标记截断")
	}
}

func TestFindMatchesGlobAndSubstring(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, root, "example/docs/guide.md", "x")
	writeRepoFile(t, root, "example/docs/deep/notes.md", "x")
	writeRepoFile(t, root, "example/src/main.go", "x")
	writeRepoFile(t, root, "example/.git/config", "x")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	globbed, err := v.Find("example", FindOptions{Pattern: "*.md"})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if len(globbed.Entries) != 2 {
		t.Fatalf("glob entries = %#v", globbed.Entries)
	}
	for _, entry := range globbed.Entries {
		if !strings.HasSuffix(entry.Path, ".md") {
			t.Fatalf("命中了非目标文件: %#v", entry)
		}
	}

	substring, err := v.Find("example", FindOptions{Pattern: "MAIN"})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if len(substring.Entries) != 1 || substring.Entries[0].Name != "main.go" {
		t.Fatalf("substring entries = %#v", substring.Entries)
	}

	scoped, err := v.Find("example", FindOptions{Pattern: "*.md", Path: "docs/deep"})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if len(scoped.Entries) != 1 || scoped.Entries[0].Path != "docs/deep/notes.md" {
		t.Fatalf("scoped entries = %#v", scoped.Entries)
	}
}

func TestFindRejectsEmptyPattern(t *testing.T) {
	t.Parallel()

	v, err := New(t.TempDir(), []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := v.Find("example", FindOptions{Pattern: "  "}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("Find() error = %v, want ErrInvalidName", err)
	}
}

func TestSearchReportsUnknownRepositoryAndMissingPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, root, "example/README.md", "needle\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := v.Search("missing", SearchOptions{Query: "needle"}); !errors.Is(err, ErrRepoNotFound) {
		t.Fatalf("Search(unknown repo) error = %v, want ErrRepoNotFound", err)
	}
	if _, err := v.Search("example", SearchOptions{Query: "needle", Path: "nodir"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Search(unknown path) error = %v, want ErrNotFound", err)
	}
}

func TestSearchSkipsOversizedAndBinaryFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	writeRepoFile(t, dir, "docs/ok.md", "needle here\n")
	writeRepoFile(t, dir, "docs/big.md", strings.Repeat("needle ", 400))
	writeRepoBytes(t, dir, "docs/blob.bin", []byte("needle\x00binary"))

	limits := DefaultLimits()
	limits.MaxFileBytes = 512
	v, err := New(root, []string{"example"}, limits)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := v.Search("example", SearchOptions{Query: "needle"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Matches) != 1 || result.Matches[0].Path != "docs/ok.md" {
		t.Fatalf("matches = %#v", result.Matches)
	}
	if result.Scanned != 1 {
		t.Fatalf("Scanned = %d, want 1", result.Scanned)
	}
}

func TestSearchStopsAtByteBudget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	line := "needle" + strings.Repeat("x", 93) + "\n"
	for index := range 5 {
		writeRepoFile(t, dir, fmt.Sprintf("docs/%d.md", index), line)
	}

	limits := DefaultLimits()
	limits.MaxSearchBytes = 250
	v, err := New(root, []string{"example"}, limits)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := v.Search("example", SearchOptions{Query: "needle"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if !result.Truncated {
		t.Fatal("超过字节预算后应标记截断")
	}
	// 每个文件 100 字节：读到第 3 个文件时累计 300 字节，越过 250 字节的预算。
	if result.Scanned != 3 {
		t.Fatalf("Scanned = %d, want 3", result.Scanned)
	}
	if want := int64(3 * len(line)); result.ScannedBytes != want {
		t.Fatalf("ScannedBytes = %d, want %d", result.ScannedBytes, want)
	}
}

func TestSearchStopsAtFileBudget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	for index := range 4 {
		writeRepoFile(t, dir, fmt.Sprintf("docs/%d.md", index), "needle\n")
	}

	limits := DefaultLimits()
	limits.MaxSearchFiles = 2
	v, err := New(root, []string{"example"}, limits)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := v.Search("example", SearchOptions{Query: "needle"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if !result.Truncated {
		t.Fatal("达到文件数上限后应标记截断")
	}
	if result.Scanned != 2 {
		t.Fatalf("Scanned = %d, want 2", result.Scanned)
	}
}

func TestSearchIsDeterministicAndSorted(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	for index := range 20 {
		writeRepoFile(t, dir, fmt.Sprintf("docs/%02d.md", index), "needle one\nfiller\nneedle two\n")
	}
	writeRepoFile(t, dir, "docs.md", "needle outside directory\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	options := SearchOptions{Query: "needle", Limit: 5}
	first, err := v.Search("example", options)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(first.Matches) != 5 || !first.Truncated {
		t.Fatalf("first = %#v", first)
	}
	assertSortedMatches(t, first.Matches)

	for range 10 {
		again, err := v.Search("example", options)
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if !reflect.DeepEqual(first.Matches, again.Matches) {
			t.Fatalf("两次检索结果不一致:\n%#v\n%#v", first.Matches, again.Matches)
		}
	}

	// 翻页时残存的命中同样稳定：同一次查询多次执行完全一致。
	page := SearchOptions{Query: "needle", Limit: 5, Offset: 3}
	want, err := v.Search("example", page)
	if err != nil {
		t.Fatalf("Search(offset) error = %v", err)
	}
	for range 10 {
		again, err := v.Search("example", page)
		if err != nil {
			t.Fatalf("Search(offset) error = %v", err)
		}
		if !reflect.DeepEqual(want.Matches, again.Matches) {
			t.Fatalf("两次翻页结果不一致:\n%#v\n%#v", want.Matches, again.Matches)
		}
	}
	assertSortedMatches(t, want.Matches)
}

func TestSearchKeepsTextOnlyForRequestedWindow(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	var body strings.Builder
	for index := 1; index <= 10; index++ {
		fmt.Fprintf(&body, "needle %d\n", index)
	}
	writeRepoFile(t, dir, "docs/list.md", body.String())

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := v.Search("example", SearchOptions{Query: "needle", Limit: 3, Offset: 6})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Matches) != 3 {
		t.Fatalf("matches = %#v", result.Matches)
	}
	for index, match := range result.Matches {
		wantLine := 7 + index
		if match.Line != wantLine || match.Text != fmt.Sprintf("needle %d", wantLine) {
			t.Fatalf("match[%d] = %#v, want line %d", index, match, wantLine)
		}
	}
	if !result.Truncated {
		t.Fatal("后面还有命中时应标记截断")
	}
}

func TestSearchReturnsEmptyPageBeyondLastMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, root, "example/docs/a.md", "needle\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := v.Search("example", SearchOptions{Query: "needle", Offset: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Matches) != 0 {
		t.Fatalf("matches = %#v, want empty", result.Matches)
	}
	if result.Truncated {
		t.Fatal("越过最后一条命中时不应标记截断")
	}
}

func assertSortedMatches(t *testing.T, matches []Match) {
	t.Helper()

	for index := 1; index < len(matches); index++ {
		previous := matches[index-1]
		current := matches[index]
		if previous.Path > current.Path {
			t.Fatalf("命中未按路径升序: %q 在 %q 之前", previous.Path, current.Path)
		}
		if previous.Path == current.Path && previous.Line > current.Line {
			t.Fatalf("同文件命中未按行号升序: %d 在 %d 之前", previous.Line, current.Line)
		}
	}
}

func TestListingKeepsUnsynchronizedRepository(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	writeRepoFile(t, dir, ".git/HEAD", "ref: refs/heads/main\n")
	writeRepoFile(t, dir, ".git/refs/heads/main", "0123456789abcdef0123456789abcdef01234567\n")

	v, err := New(root, []string{"example", "fresh"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	listing := v.Listing()
	if len(listing) != 2 {
		t.Fatalf("Listing() = %#v, want 2 条", listing)
	}
	if listing[0].Name != "example" || listing[0].Branch != "main" {
		t.Fatalf("listing[0] = %#v", listing[0])
	}
	// fresh 只是登记了，还没同步到本地：仍然出现在清单里，只是没有分支与提交号。
	if listing[1].Name != "fresh" || listing[1].Branch != "" || listing[1].Revision != "" {
		t.Fatalf("listing[1] = %#v", listing[1])
	}
}
