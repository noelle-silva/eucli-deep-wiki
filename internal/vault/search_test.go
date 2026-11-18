package vault

import (
	"path/filepath"
	"testing"
)

func TestSearchAddsContextLines(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, "example"), "doc.txt", "one\ntwo\nneedle\nfour\nfive\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := v.Search("example", SearchOptions{Query: "needle", Context: 1})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("matches = %#v", result.Matches)
	}

	match := result.Matches[0]
	if len(match.Before) != 1 || match.Before[0].Line != 2 || match.Before[0].Text != "two" {
		t.Fatalf("before = %#v", match.Before)
	}
	if len(match.After) != 1 || match.After[0].Line != 4 || match.After[0].Text != "four" {
		t.Fatalf("after = %#v", match.After)
	}
}

func TestSearchContextStopsAtFileEdges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, "example"), "doc.txt", "needle\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := v.Search("example", SearchOptions{Query: "needle", Context: 3})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	match := result.Matches[0]
	if len(match.Before) != 0 {
		t.Fatalf("before = %#v, want empty", match.Before)
	}
	if len(match.After) != 0 {
		t.Fatalf("after = %#v, want empty（末尾换行不该造出空行）", match.After)
	}
}

func TestSearchNormalizesContextCap(t *testing.T) {
	t.Parallel()

	if got := NormalizeSearchContext(-1); got != 0 {
		t.Fatalf("NormalizeSearchContext(-1) = %d", got)
	}
	if got := NormalizeSearchContext(MaxSearchContext + 5); got != MaxSearchContext {
		t.Fatalf("NormalizeSearchContext(超限) = %d, want %d", got, MaxSearchContext)
	}
	if got := NormalizeSearchContext(2); got != 2 {
		t.Fatalf("NormalizeSearchContext(2) = %d", got)
	}
}

func TestSearchReportsOversizedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	writeRepoFile(t, dir, "small.txt", "needle\n")
	writeRepoFile(t, dir, "huge.txt", "0123456789abcdef0123456789abcdef0123456789abcdef")

	limits := DefaultLimits()
	limits.MaxSearchFileBytes = 16
	v, err := New(root, []string{"example"}, limits)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := v.Search("example", SearchOptions{Query: "needle"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.OversizedCount != 1 {
		t.Fatalf("OversizedCount = %d, want 1", result.OversizedCount)
	}
	if len(result.Oversized) != 1 || result.Oversized[0].Path != "huge.txt" {
		t.Fatalf("Oversized = %#v", result.Oversized)
	}
	if result.Oversized[0].Size != 48 {
		t.Fatalf("Size = %d, want 48", result.Oversized[0].Size)
	}
}

func TestSearchWithoutOversizedKeepsListEmpty(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRepoFile(t, filepath.Join(root, "example"), "small.txt", "needle\n")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := v.Search("example", SearchOptions{Query: "needle"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.OversizedCount != 0 || len(result.Oversized) != 0 {
		t.Fatalf("Oversized = %#v / %d", result.Oversized, result.OversizedCount)
	}
}
