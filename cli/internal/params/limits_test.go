package params

import "testing"

// 这几个数是命令行的对外承诺：帮助文本与超限提示里写的就是它们。
// 取值必须与服务端实际生效的数一致，改动任何一个都要两边一起改。
func TestLimitConstants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  int
		want int
	}{
		{"DefaultTreeDepth", DefaultTreeDepth, 1},
		{"MaxTreeDepth", MaxTreeDepth, 8},
		{"DefaultFindLimit", DefaultFindLimit, 200},
		{"MaxFindLimit", MaxFindLimit, 10000},
		{"MaxSearchContext", MaxSearchContext, 10},
		{"MaxSearchLimit", MaxSearchLimit, 100},
	}

	for _, testCase := range cases {
		if testCase.got != testCase.want {
			t.Fatalf("%s = %d, want %d", testCase.name, testCase.got, testCase.want)
		}
	}
}

func TestNormalizeFindLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		requested int
		want      int
	}{
		{0, DefaultFindLimit},
		{-1, DefaultFindLimit},
		{1, 1},
		{DefaultFindLimit, DefaultFindLimit},
		{MaxFindLimit, MaxFindLimit},
		{MaxFindLimit + 1, MaxFindLimit},
	}

	for _, testCase := range cases {
		if got := NormalizeFindLimit(testCase.requested); got != testCase.want {
			t.Fatalf("NormalizeFindLimit(%d) = %d, want %d", testCase.requested, got, testCase.want)
		}
	}
}
