package vault

import (
	"errors"
	"testing"
)

func TestParseLineRange(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want LineRange
	}{
		{"", LineRange{}},
		{"  ", LineRange{}},
		{"362", LineRange{Start: 362, End: 362}},
		{"362-520", LineRange{Start: 362, End: 520}},
		{"362-", LineRange{Start: 362}},
		{"-520", LineRange{End: 520}},
		{" 7 - 9 ", LineRange{Start: 7, End: 9}},
	}

	for _, testCase := range cases {
		got, err := ParseLineRange(testCase.raw)
		if err != nil {
			t.Fatalf("ParseLineRange(%q) error = %v", testCase.raw, err)
		}
		if got != testCase.want {
			t.Fatalf("ParseLineRange(%q) = %#v, want %#v", testCase.raw, got, testCase.want)
		}
	}
}

func TestParseLineRangeRejectsBadInput(t *testing.T) {
	t.Parallel()

	bad := []string{"abc", "-", "0-5", "5-3", "3-x", "1-2-3", "-0"}
	for _, raw := range bad {
		if _, err := ParseLineRange(raw); !errors.Is(err, ErrInvalidLineRange) {
			t.Fatalf("ParseLineRange(%q) error = %v, want ErrInvalidLineRange", raw, err)
		}
	}
}

func TestLineRangeStringRoundTrip(t *testing.T) {
	t.Parallel()

	ranges := []LineRange{
		{},
		{Start: 3, End: 3},
		{Start: 3, End: 9},
		{Start: 3},
		{End: 9},
	}
	for _, span := range ranges {
		written := span.String()
		parsed, err := ParseLineRange(written)
		if err != nil {
			t.Fatalf("ParseLineRange(%q) error = %v", written, err)
		}
		if parsed != span {
			t.Fatalf("round trip %#v -> %q -> %#v", span, written, parsed)
		}
	}
}

func TestCountLines(t *testing.T) {
	t.Parallel()

	cases := map[string]int{
		"":       0,
		"\n":     1,
		"a":      1,
		"a\n":    1,
		"a\nb":   2,
		"a\n\n":  2,
		"a\nb\n": 2,
	}
	for content, want := range cases {
		if got := countLines([]byte(content)); got != want {
			t.Fatalf("countLines(%q) = %d, want %d", content, got, want)
		}
	}
}

func TestSliceLines(t *testing.T) {
	t.Parallel()

	content := []byte("a\nb\nc\nd\n")
	cases := []struct {
		span  LineRange
		want  string
		first int
		last  int
	}{
		{LineRange{}, "a\nb\nc\nd\n", 1, 4},
		{LineRange{Start: 2, End: 3}, "b\nc\n", 2, 3},
		{LineRange{Start: 3}, "c\nd\n", 3, 4},
		{LineRange{End: 2}, "a\nb\n", 1, 2},
		{LineRange{Start: 3, End: 3}, "c\n", 3, 3},
		{LineRange{Start: 9}, "", 5, 4},
	}

	for _, testCase := range cases {
		chunk, first, last := sliceLines(content, testCase.span)
		if string(chunk) != testCase.want || first != testCase.first || last != testCase.last {
			t.Fatalf("sliceLines(%#v) = (%q, %d, %d), want (%q, %d, %d)",
				testCase.span, chunk, first, last, testCase.want, testCase.first, testCase.last)
		}
	}
}

func TestSliceLinesWithoutTrailingNewline(t *testing.T) {
	t.Parallel()

	content := []byte("a\nb")
	chunk, first, last := sliceLines(content, LineRange{Start: 2, End: 2})
	if string(chunk) != "b" || first != 2 || last != 2 {
		t.Fatalf("sliceLines = (%q, %d, %d)", chunk, first, last)
	}
}

func TestClampToBytes(t *testing.T) {
	t.Parallel()

	content := []byte("aaaa\nbbbb\ncccc\n")

	kept, last, truncated := clampToBytes(content, 1, 1000, 3)
	if truncated || string(kept) != string(content) || last != 3 {
		t.Fatalf("未超限时不应截断：(%q, %d, %v)", kept, last, truncated)
	}

	kept, last, truncated = clampToBytes(content, 1, 12, 3)
	if !truncated || string(kept) != "aaaa\nbbbb\n" || last != 2 {
		t.Fatalf("超限时应退到完整行：(%q, %d, %v)", kept, last, truncated)
	}

	kept, last, truncated = clampToBytes(content, 2, 3, 3)
	if !truncated || len(kept) != 0 || last != 1 {
		t.Fatalf("第一行就超限时应给空内容：(%q, %d, %v)", kept, last, truncated)
	}
}
