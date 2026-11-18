package vault

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSlicesLineRange(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, "example"), "doc.txt", "one\ntwo\nthree\nfour\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	file, err := v.File("example", FileOptions{Path: "doc.txt", Lines: LineRange{Start: 2, End: 3}})
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}
	if string(file.Content) != "two\nthree\n" {
		t.Fatalf("Content = %q", string(file.Content))
	}
	if file.TotalLines != 4 || file.FirstLine != 2 || file.LastLine != 3 {
		t.Fatalf("行信息 = %d / %d-%d", file.TotalLines, file.FirstLine, file.LastLine)
	}
	if file.Truncated != "" {
		t.Fatalf("Truncated = %q, want empty", file.Truncated)
	}
}

func TestFileWithoutRangeReturnsEverything(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, "example"), "doc.txt", "one\ntwo\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	file, err := v.File("example", FileOptions{Path: "doc.txt"})
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}
	if string(file.Content) != "one\ntwo\n" {
		t.Fatalf("Content = %q", string(file.Content))
	}
	if file.FirstLine != 1 || file.LastLine != 2 || file.TotalLines != 2 {
		t.Fatalf("行信息 = %d / %d-%d", file.TotalLines, file.FirstLine, file.LastLine)
	}
}

func TestFileClampsByMaxBytes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, "example"), "doc.txt", "aaaa\nbbbb\ncccc\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	file, err := v.File("example", FileOptions{Path: "doc.txt", MaxBytes: 12})
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}
	if string(file.Content) != "aaaa\nbbbb\n" {
		t.Fatalf("Content = %q", string(file.Content))
	}
	if file.LastLine != 2 {
		t.Fatalf("LastLine = %d, want 2", file.LastLine)
	}
	if file.Truncated != "bytes" {
		t.Fatalf("Truncated = %q, want bytes", file.Truncated)
	}
}

func TestFileReportsRangePastEnd(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, "example"), "doc.txt", "one\ntwo\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	file, err := v.File("example", FileOptions{Path: "doc.txt", Lines: LineRange{Start: 9}})
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}
	if len(file.Content) != 0 {
		t.Fatalf("Content = %q, want empty", string(file.Content))
	}
	if file.FirstLine != 3 || file.LastLine != 2 {
		t.Fatalf("行信息 = %d-%d, want 3-2", file.FirstLine, file.LastLine)
	}
}

func TestFileKeepsETagAcrossRanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, "example"), "doc.txt", "one\ntwo\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	whole, err := v.File("example", FileOptions{Path: "doc.txt"})
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}
	part, err := v.File("example", FileOptions{Path: "doc.txt", Lines: LineRange{Start: 2, End: 2}})
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}
	if whole.ETag == "" || whole.ETag != part.ETag {
		t.Fatalf("ETag 应描述整份文件：whole=%q part=%q", whole.ETag, part.ETag)
	}
}

func TestFileReadsRangeFromLargeFile(t *testing.T) {
	t.Parallel()

	// 32 行 × 64 KiB ≈ 2 MiB：文件本身很大，但要的只是其中两行。
	line := strings.Repeat("x", 64<<10)
	content := strings.Repeat(line+"\n", 32)

	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, "example"), "big.txt", content)

	limits := DefaultLimits()
	limits.MaxFileBytes = 4 << 20
	v, err := New(root, []string{"example"}, limits)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	file, err := v.File("example", FileOptions{Path: "big.txt", Lines: LineRange{Start: 2, End: 2}})
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}
	if file.TotalLines != 32 || file.FirstLine != 2 || file.LastLine != 2 {
		t.Fatalf("行信息 = %d / %d-%d", file.TotalLines, file.FirstLine, file.LastLine)
	}
	if string(file.Content) != line+"\n" {
		t.Fatalf("Content 长度 = %d, want %d", len(file.Content), len(line)+1)
	}
}
