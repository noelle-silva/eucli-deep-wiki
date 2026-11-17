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

	result, err := v.Search("example", "needle", 0)
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

	if _, err := v.Search("example", "  ", 0); !errors.Is(err, ErrInvalidQuery) {
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
