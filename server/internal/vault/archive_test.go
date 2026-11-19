package vault

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestWriteArchivePacksSelectedPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := t.TempDir()
	_ = dir
	writeRepoFile(t, root, "example/docs/a.md", "alpha\n")
	writeRepoFile(t, root, "example/docs/b.md", "beta\n")
	writeRepoFile(t, root, "example/.git/config", "should be skipped\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	buffer := &bytes.Buffer{}
	if err := v.WriteArchive([]ArchiveTarget{{Repo: "example", Path: "docs"}}, FormatZip, buffer); err != nil {
		t.Fatalf("WriteArchive() error = %v", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}

	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
		if strings.Contains(file.Name, ".git") {
			t.Fatalf("归档里出现了 .git: %v", names)
		}
	}
	if len(names) != 2 {
		t.Fatalf("entries = %v", names)
	}
	if names[0] != "example/docs/a.md" || names[1] != "example/docs/b.md" {
		t.Fatalf("entries = %v", names)
	}
}

func TestWriteArchivePacksWholeRepository(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, root, "example/README.md", "hello\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	buffer := &bytes.Buffer{}
	if err := v.WriteArchive([]ArchiveTarget{{Repo: "example"}}, FormatTarGz, buffer); err != nil {
		t.Fatalf("WriteArchive() error = %v", err)
	}
	if buffer.Len() == 0 {
		t.Fatal("归档为空")
	}
}

func TestWriteArchiveRejectsUnknownRepository(t *testing.T) {
	t.Parallel()

	v, err := New(t.TempDir(), nil, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = v.WriteArchive([]ArchiveTarget{{Repo: "missing"}}, FormatZip, io.Discard)
	if !errors.Is(err, ErrRepoNotFound) {
		t.Fatalf("WriteArchive() error = %v, want ErrRepoNotFound", err)
	}
}
