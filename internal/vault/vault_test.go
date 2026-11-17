package vault

import (
	"errors"
	"os"
	"path/filepath"
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
