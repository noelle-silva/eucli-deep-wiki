package vault

import (
	"path/filepath"
	"strconv"
	"testing"
)

func TestParseExcludedDirsIgnoresEmptyItems(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "空串", raw: "", want: nil},
		{name: "只有空白", raw: "   ", want: nil},
		{name: "只有逗号", raw: ",,", want: nil},
		{name: "单个名字", raw: "node_modules", want: []string{"node_modules"}},
		{name: "多余逗号", raw: ",node_modules,,vendor,", want: []string{"node_modules", "vendor"}},
		{name: "名字两侧空白", raw: " node_modules , vendor ", want: []string{"node_modules", "vendor"}},
		{name: "大小写归一", raw: "NODE_MODULES,Pods", want: []string{"node_modules", "pods"}},
		{name: "重复项", raw: "vendor,vendor", want: []string{"vendor"}},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := ParseExcludedDirs(testCase.raw)
			if len(got) != len(testCase.want) {
				t.Fatalf("ParseExcludedDirs(%q) = %#v, want %v", testCase.raw, got, testCase.want)
			}
			for _, name := range testCase.want {
				if _, ok := got[name]; !ok {
					t.Fatalf("ParseExcludedDirs(%q) = %#v, want 含 %q", testCase.raw, got, name)
				}
			}
		})
	}
}

func TestExcludedDirsSkipsOnlyDirectories(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		raw    string
		item   string
		isDir  bool
		skips  bool
		reason string
	}{
		{name: "命中目录", raw: "node_modules", item: "node_modules", isDir: true, skips: true, reason: "名字在名单里的目录要跳过"},
		{name: "命中目录但写法不同", raw: "node_modules", item: "NODE_MODULES", isDir: true, skips: true, reason: "大小写不敏感"},
		{name: "同名普通文件", raw: "node_modules", item: "node_modules", isDir: false, skips: false, reason: "只排除目录，同名文件照常返回"},
		{name: "未命中的目录", raw: "node_modules", item: "vendor", isDir: true, skips: false, reason: "名字不在名单里"},
		{name: "空白名单", raw: "", item: "node_modules", isDir: true, skips: false, reason: "空名单不排除任何目录"},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := ParseExcludedDirs(testCase.raw).Skips(testCase.item, testCase.isDir); got != testCase.skips {
				t.Fatalf("Skips(%q, isDir=%v) = %v, want %v（%s）", testCase.item, testCase.isDir, got, testCase.skips, testCase.reason)
			}
		})
	}
}

// treePaths 把一次展开的条目压成路径列表，顺序即服务端的展开顺序。
func treePaths(tree Tree) []string {
	paths := make([]string, 0, len(tree.Entries))
	for _, entry := range tree.Entries {
		paths = append(paths, entry.Path)
	}
	return paths
}

// samePaths 比较两份路径列表：条数、顺序、内容都必须一致。
func samePaths(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// newExcludeRepository 铺一个用来观察排除行为的仓库：
//
//	node_modules/left-pad/index.js   名字在名单里的目录，内部还有一层
//	docs/node_modules                同名的普通文件（在另一层，因此不与目录冲突）
//	src/main.go                      普通文件，用来确认别的条目没被牵连
func newExcludeRepository(t *testing.T) *Vault {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	writeRepoFile(t, dir, "node_modules/left-pad/index.js", "x")
	writeRepoFile(t, dir, "docs/node_modules", "x")
	writeRepoFile(t, dir, "src/main.go", "x")

	v, err := New(root, []string{"example"}, DefaultLimits())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return v
}

func TestTreeExcludesNamedDirectories(t *testing.T) {
	t.Parallel()

	v := newExcludeRepository(t)

	tree, err := v.Tree("example", TreeOptions{Depth: 3, Exclude: ParseExcludedDirs("node_modules")})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}

	// 被排除目录连同内部一起消失，其余条目原样保留；同名普通文件照常返回。
	want := []string{"docs", "docs/node_modules", "src", "src/main.go"}
	if got := treePaths(tree); !samePaths(got, want) {
		t.Fatalf("entries = %#v, want %#v", got, want)
	}
}

func TestTreeAcceptsEmptyExclude(t *testing.T) {
	t.Parallel()

	v := newExcludeRepository(t)

	withoutParam, err := v.Tree("example", TreeOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	emptyList, err := v.Tree("example", TreeOptions{Depth: 3, Exclude: ParseExcludedDirs("")})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}

	// 空列表等于不排除：与改动前的展开结果一字不差。
	if got, want := treePaths(emptyList), treePaths(withoutParam); !samePaths(got, want) {
		t.Fatalf("空 exclude 的 entries = %#v, want %#v", got, want)
	}
	// 不传名单时，命中候选的目录必须照常出现在结果里。
	want := []string{"docs", "docs/node_modules", "node_modules", "node_modules/left-pad", "node_modules/left-pad/index.js", "src", "src/main.go"}
	if got := treePaths(withoutParam); !samePaths(got, want) {
		t.Fatalf("不带 exclude 的 entries = %#v, want %#v", got, want)
	}
}

func TestTreeExcludeWorksWithDirsOnlyAndDepth(t *testing.T) {
	t.Parallel()

	v := newExcludeRepository(t)

	// 与 depth 一起用：只展开一层，被排除的目录在这一层就消失。
	shallow, err := v.Tree("example", TreeOptions{Depth: 1, Exclude: ParseExcludedDirs("node_modules")})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	wantShallow := []string{"docs", "src"}
	if got := treePaths(shallow); !samePaths(got, wantShallow) {
		t.Fatalf("depth=1 的 entries = %#v, want %#v", got, wantShallow)
	}

	// 与 dirsOnly 一起用：只剩目录，被排除的目录同样不出现。
	skeleton, err := v.Tree("example", TreeOptions{Depth: 3, DirsOnly: true, Exclude: ParseExcludedDirs("node_modules")})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	for _, entry := range skeleton.Entries {
		if entry.Type != "dir" {
			t.Fatalf("dirsOnly 时非目录混进结果: %#v", entry)
		}
	}
	if got, want := treePaths(skeleton), []string{"docs", "src"}; !samePaths(got, want) {
		t.Fatalf("dirsOnly 的 entries = %#v, want %#v", got, want)
	}
}

func TestTreeExcludeDoesNotSpendEntryBudget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "example")
	// 依赖目录里塞十个文件：若排除发生在拿到数据之后，它们会先把配额吃满。
	for index := 0; index < 10; index++ {
		writeRepoFile(t, dir, "node_modules/dep/"+strconv.Itoa(index)+".js", "x")
	}
	writeRepoFile(t, dir, "b.txt", "x")
	writeRepoFile(t, dir, "a.txt", "x")
	writeRepoFile(t, dir, "c.txt", "x")

	limits := DefaultLimits()
	limits.MaxTreeEntries = 3
	v, err := New(root, []string{"example"}, limits)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	withExclude, err := v.Tree("example", TreeOptions{Depth: 3, Exclude: ParseExcludedDirs("node_modules")})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	// 配额只花在真正会返回的条目上：三个普通文件全部拿到，且没有被截断。
	if got, want := treePaths(withExclude), []string{"a.txt", "b.txt", "c.txt"}; !samePaths(got, want) {
		t.Fatalf("带 exclude 的 entries = %#v, want %#v", got, want)
	}
	if withExclude.Truncated {
		t.Fatal("带 exclude 时没有超出条目上限，不该标记截断")
	}

	// 对照：不排除时，依赖目录与它的子项会把配额吃满。
	withoutExclude, err := v.Tree("example", TreeOptions{Depth: 3})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if len(withoutExclude.Entries) != limits.MaxTreeEntries {
		t.Fatalf("不带 exclude 的 entries = %d, want %d", len(withoutExclude.Entries), limits.MaxTreeEntries)
	}
	if !withoutExclude.Truncated {
		t.Fatal("不带 exclude 时超出条目上限，应标记截断")
	}
}

func TestTreeExcludeDoesNotBlockRequestedRoot(t *testing.T) {
	t.Parallel()

	v := newExcludeRepository(t)

	// 调用方点名要看这个目录：排除清单只管遍历途中遇到的目录，不该把入口挡上。
	tree, err := v.Tree("example", TreeOptions{Path: "node_modules", Depth: 2, Exclude: ParseExcludedDirs("node_modules")})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if got, want := treePaths(tree), []string{"node_modules/left-pad", "node_modules/left-pad/index.js"}; !samePaths(got, want) {
		t.Fatalf("entries = %#v, want %#v", got, want)
	}
}
