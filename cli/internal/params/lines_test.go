package params

import (
	"errors"
	"testing"
)

func TestParseLineRangeAcceptsAllFourSyntaxes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want LineRange
	}{
		{"", LineRange{}},
		{"   ", LineRange{}},
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

	bad := []string{"abc", "-", "0-5", "5-3", "3-x", "1-2-3", "-0", "0"}
	for _, raw := range bad {
		if _, err := ParseLineRange(raw); !errors.Is(err, ErrInvalidLineRange) {
			t.Fatalf("ParseLineRange(%q) error = %v, want ErrInvalidLineRange", raw, err)
		}
	}
}

// 归一回写是命令行这份实现的唯一用途：写法里的空格与端点简写都要收成
// 服务端认识的那一种形态。
func TestLineRangeWritesBackNormalized(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want string
	}{
		{"362", "362"},
		{"362-520", "362-520"},
		{"362-", "362-"},
		{"-520", "-520"},
		{" 7 - 9 ", "7-9"},
		{"8-8", "8"},
	}

	for _, testCase := range cases {
		span, err := ParseLineRange(testCase.raw)
		if err != nil {
			t.Fatalf("ParseLineRange(%q) error = %v", testCase.raw, err)
		}
		if got := span.String(); got != testCase.want {
			t.Fatalf("ParseLineRange(%q).String() = %q, want %q", testCase.raw, got, testCase.want)
		}
	}
}

func TestLineRangeRoundTrip(t *testing.T) {
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

func TestLineRangeIsZero(t *testing.T) {
	t.Parallel()

	if !(LineRange{}).IsZero() {
		t.Fatal("零值应为未限定")
	}
	if (LineRange{Start: 1}).IsZero() {
		t.Fatal("只有起点时不是未限定")
	}
	if (LineRange{End: 1}).IsZero() {
		t.Fatal("只有终点时不是未限定")
	}
}
