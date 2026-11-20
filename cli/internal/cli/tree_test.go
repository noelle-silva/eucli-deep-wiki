package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// queryRecorder 收集假服务端收到的查询参数：用例据此断言「到底往服务端发了什么」。
//
// 多段请求是并发的，因此读写都在锁里。
type queryRecorder struct {
	mutex   sync.Mutex
	queries []url.Values
}

func (r *queryRecorder) record(request *http.Request) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.queries = append(r.queries, request.URL.Query())
}

// all 返回已收到的全部查询参数；并发请求下顺序不确定。
func (r *queryRecorder) all() []url.Values {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return append([]url.Values{}, r.queries...)
}

// byPath 按 path 参数索引，多目录时用它看每个目录各自发了什么。
func (r *queryRecorder) byPath() map[string]url.Values {
	indexed := make(map[string]url.Values, len(r.queries))
	for _, query := range r.all() {
		indexed[query.Get("path")] = query
	}
	return indexed
}

// TestTreeWithoutDirectoryListsRepositoryRoot 守住「不给目录等同只看仓库根」：
// 只发一次请求，且输出不带段分隔行。
func TestTreeWithoutDirectoryListsRepositoryRoot(t *testing.T) {
	t.Parallel()

	recorder := &queryRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		_, _ = w.Write([]byte(`{"repo":"example","path":"","depth":1,"entries":[
			{"name":"src","path":"src","type":"dir","depth":1},
			{"name":"go.mod","path":"go.mod","type":"file","size":32,"depth":1}
		],"truncated":false}`))
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "tree", "example")

	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	queries := recorder.all()
	if len(queries) != 1 {
		t.Fatalf("请求次数 = %d, want 1", len(queries))
	}
	if got := queries[0].Get("path"); got != "" {
		t.Fatalf("path = %q, want 空（仓库根）", got)
	}
	want := "src/\ngo.mod\t32\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

// TestTreeWithSingleDirectoryOmitsSeparator 说明只给一个目录时不写分隔行：
// 输出可以直接进管道，与 get 单路径的做法一致。
func TestTreeWithSingleDirectoryOmitsSeparator(t *testing.T) {
	t.Parallel()

	recorder := &queryRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		_, _ = w.Write([]byte(`{"repo":"example","path":"src","depth":1,"entries":[
			{"name":"main.go","path":"src/main.go","type":"file","size":42,"depth":1}
		],"truncated":false}`))
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "tree", "example", "src")

	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if got := recorder.byPath()["src"].Get("path"); got != "src" {
		t.Fatalf("path = %q, want src", got)
	}
	if stdout != "src/main.go\t42\n" {
		t.Fatalf("stdout = %q", stdout)
	}
}

// TestTreeListsMultipleDirectoriesInCommandOrder 守住多目录的段数、顺序与分隔样式。
//
// 第一个目录故意最慢：输出顺序由命令行决定，与哪个请求先返回无关。
func TestTreeListsMultipleDirectoriesInCommandOrder(t *testing.T) {
	t.Parallel()

	recorder := &queryRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		target := r.URL.Query().Get("path")
		if target == "src" {
			time.Sleep(30 * time.Millisecond)
		}
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"repo":"example","path":%q,"depth":1,"entries":[{"name":"file.txt","path":"%s/file.txt","type":"file","size":%d,"depth":1}],"truncated":false}`,
			target, target, len(target))))
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "tree", "example", "src", "docs", "scripts")

	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}

	want := strings.Join([]string{
		"==> src <==",
		"src/file.txt\t3",
		"==> docs <==",
		"docs/file.txt\t4",
		"==> scripts <==",
		"scripts/file.txt\t7",
		"",
	}, "\n")
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}

	queries := recorder.byPath()
	if len(queries) != 3 {
		t.Fatalf("请求次数 = %d, want 3", len(queries))
	}
	for _, target := range []string{"src", "docs", "scripts"} {
		if _, ok := queries[target]; !ok {
			t.Fatalf("没有为 %q 发请求：%v", target, queries)
		}
	}
}

// TestTreeKeepsGoingWhenOneDirectoryFails 守住单段失败不拖垮整次调用：
// 成功的照常输出、失败的逐段说清、退出码按第一个失败算。
func TestTreeKeepsGoingWhenOneDirectoryFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("path") {
		case "missing":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"path_not_found","message":"路径不存在"}`))
		case "broken":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":"boom","message":"服务端出错"}`))
		default:
			_, _ = w.Write([]byte(`{"repo":"example","path":"docs","depth":1,"entries":[
				{"name":"a.md","path":"docs/a.md","type":"file","size":3,"depth":1}
			],"truncated":false}`))
		}
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "tree", "example", "missing", "docs", "broken")

	// 第一个失败是「目录不存在」，第二个是「其它失败」：退出码取第一个。
	if code != exitNotFound {
		t.Fatalf("code = %d, want %d", code, exitNotFound)
	}
	if stdout != "==> docs <==\ndocs/a.md\t3\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "missing: ") || !strings.Contains(stderr, "broken: ") {
		t.Fatalf("stderr 应逐段说明失败：%q", stderr)
	}
	if strings.Index(stderr, "missing: ") > strings.Index(stderr, "broken: ") {
		t.Fatalf("失败说明应按命令行顺序出现：%q", stderr)
	}
}

// TestTreeSendsExcludeAndHidesDependencyDirectories 守住默认压掉依赖噪声：
// 清单发给服务端（省条目配额），本地也按同一份清单收一遍（服务端还没跟上时
// 同样生效），说明只走标准错误。
func TestTreeSendsExcludeAndHidesDependencyDirectories(t *testing.T) {
	t.Parallel()

	recorder := &queryRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		_, _ = w.Write([]byte(`{"repo":"example","path":"","depth":2,"entries":[
			{"name":"node_modules","path":"node_modules","type":"dir","depth":1},
			{"name":"left-pad","path":"node_modules/left-pad","type":"dir","depth":2},
			{"name":"index.js","path":"node_modules/left-pad/index.js","type":"file","size":100,"depth":2},
			{"name":"Vendor","path":"Vendor","type":"dir","depth":1},
			{"name":"lib.go","path":"Vendor/lib.go","type":"file","size":20,"depth":2},
			{"name":"src","path":"src","type":"dir","depth":1},
			{"name":"main.go","path":"src/main.go","type":"file","size":42,"depth":2}
		],"truncated":false}`))
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "tree", "example", "--depth", "2")

	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if want := "src/\nsrc/main.go\t42\n"; stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	if got, want := recorder.byPath()[""].Get("exclude"), strings.Join(dependencyDirs, ","); got != want {
		t.Fatalf("exclude = %q, want %q", got, want)
	}
	for _, want := range []string{"默认不列出依赖与缓存目录", "node_modules", "--all"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr 缺少 %q：%q", want, stderr)
		}
	}
	if strings.Contains(stdout, "依赖") {
		t.Fatalf("说明不该混进 stdout：%q", stdout)
	}
}

// TestTreeAllListsDependencyDirectories 说明 --all 回到改动前的行为：
// 不发排除参数、不做本地过滤，也不出说明。
func TestTreeAllListsDependencyDirectories(t *testing.T) {
	t.Parallel()

	recorder := &queryRecorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder.record(r)
		_, _ = w.Write([]byte(`{"repo":"example","path":"","depth":2,"entries":[
			{"name":"node_modules","path":"node_modules","type":"dir","depth":1},
			{"name":"index.js","path":"node_modules/left-pad/index.js","type":"file","size":100,"depth":2},
			{"name":"src","path":"src","type":"dir","depth":1}
		],"truncated":false}`))
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "tree", "example", "--depth", "2", "--all")

	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	want := "node_modules/\nnode_modules/left-pad/index.js\t100\nsrc/\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
	if got := recorder.byPath()[""].Get("exclude"); got != "" {
		t.Fatalf("--all 不该发 exclude：%q", got)
	}
	if stderr != "" {
		t.Fatalf("--all 不该有排除说明：%q", stderr)
	}
}

// TestTreeKeepsFileNamedLikeDependencyDirectory 守住清单只作用于目录：
// 一个叫 vendor 的普通文件照常出现。
func TestTreeKeepsFileNamedLikeDependencyDirectory(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"repo":"example","path":"","depth":1,"entries":[
			{"name":"vendor","path":"vendor","type":"file","size":7,"depth":1},
			{"name":"Pods","path":"Pods","type":"dir","depth":1}
		],"truncated":false}`))
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "tree", "example")

	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if want := "vendor\t7\n"; stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

// TestOverviewHidesDependencyDirectoriesByDefault 守住 overview 与 tree 用的是同一份
// 清单：默认看不到 node_modules，--all 看得到。
func TestOverviewHidesDependencyDirectoriesByDefault(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/repos/example":
			_, _ = w.Write([]byte(`{"name":"example","branch":"main","revision":"abcdef1234567890"}`))
		case "/v1/repos/example/tree":
			_, _ = w.Write([]byte(`{"repo":"example","path":"","depth":2,"entries":[
				{"name":"README.md","path":"README.md","type":"file","size":20,"depth":1},
				{"name":"node_modules","path":"node_modules","type":"dir","depth":1},
				{"name":"left-pad","path":"node_modules/left-pad","type":"dir","depth":2}
			],"truncated":false}`))
		case "/v1/repos/example/file":
			w.Header().Set("X-File-Total-Lines", "1")
			w.Header().Set("X-File-First-Line", "1")
			w.Header().Set("X-File-Last-Line", "1")
			_, _ = w.Write([]byte("# example\n"))
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "overview", "example")
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, "node_modules") {
		t.Fatalf("默认不该出现 node_modules：\n%s", stdout)
	}
	if !strings.Contains(stdout, "== 目录（2 层，1 项） ==") {
		t.Fatalf("条目计数应按排除后算：\n%s", stdout)
	}
	if !strings.Contains(stderr, "默认不列出依赖与缓存目录") {
		t.Fatalf("stderr 应说明排除了依赖目录：%q", stderr)
	}

	code, stdout, stderr = runWithServer(t, server, "overview", "example", "--all")
	if code != exitOK {
		t.Fatalf("--all code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "node_modules/") || !strings.Contains(stdout, "== 目录（2 层，3 项） ==") {
		t.Fatalf("--all 应按原样列出全部条目：\n%s", stdout)
	}
	if stderr != "" {
		t.Fatalf("--all 不该有排除说明：%q", stderr)
	}
}

// TestOverviewTruncationHintPointsAtRealFlag 守住截断提示里只出现 overview 真有的开关：
// 它没有 --dirs-only，指过去只会把人带到沟里；能收窄条目的只有 --depth。
func TestOverviewTruncationHintPointsAtRealFlag(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/repos/example":
			_, _ = w.Write([]byte(`{"name":"example","branch":"main","revision":"abcdef1234567890"}`))
		case "/v1/repos/example/tree":
			_, _ = w.Write([]byte(`{"repo":"example","path":"","depth":2,"entries":[
				{"name":"src","path":"src","type":"dir","depth":1}
			],"truncated":true}`))
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	code, _, stderr := runWithServer(t, server, "overview", "example", "--readme-lines", "0", "--manifests", "0")

	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "目录已截断") || !strings.Contains(stderr, "--depth") {
		t.Fatalf("stderr 应指出真正管用的开关：%q", stderr)
	}
	if strings.Contains(stderr, "--dirs-only") {
		t.Fatalf("overview 没有 --dirs-only，提示不许指向它：%q", stderr)
	}
}

// TestTreeJSONAggregatesSegmentsAndFailures 守住 --json 的多段形态：
// 只有一段时是一段的对象，多段时是数组，有失败时包成 trees + failures。
func TestTreeJSONAggregatesSegmentsAndFailures(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") == "missing" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"path_not_found","message":"路径不存在"}`))
			return
		}
		_, _ = w.Write([]byte(`{"repo":"example","path":"src","depth":1,"entries":[
			{"name":"main.go","path":"src/main.go","type":"file","size":42,"depth":1}
		],"truncated":false}`))
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "tree", "example", "src", "docs", "--json")
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}

	var many []apiclientTree
	if err := json.Unmarshal([]byte(stdout), &many); err != nil {
		t.Fatalf("多段全部成功时应是数组：%v（%q）", err, stdout)
	}
	if len(many) != 2 {
		t.Fatalf("段数 = %d, want 2", len(many))
	}

	code, stdout, stderr = runWithServer(t, server, "tree", "example", "src", "missing", "--json")
	if code != exitNotFound {
		t.Fatalf("code = %d, want %d", code, exitNotFound)
	}

	var partial struct {
		Trees    []apiclientTree `json:"trees"`
		Failures []string        `json:"failures"`
	}
	if err := json.Unmarshal([]byte(stdout), &partial); err != nil {
		t.Fatalf("有失败时应是 trees + failures：%v（%q）", err, stdout)
	}
	if len(partial.Trees) != 1 || len(partial.Failures) != 1 {
		t.Fatalf("payload = %#v", partial)
	}
	if !strings.Contains(partial.Failures[0], "missing") {
		t.Fatalf("失败说明应带上目录名：%#v", partial.Failures)
	}

	code, stdout, stderr = runWithServer(t, server, "tree", "example", "src", "--json")
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	var single apiclientTree
	if err := json.Unmarshal([]byte(stdout), &single); err != nil {
		t.Fatalf("只有一段时应是对象：%v（%q）", err, stdout)
	}
	if single.Path != "src" || len(single.Entries) != 1 {
		t.Fatalf("single = %#v", single)
	}
}

// apiclientTree 是 --json 用例里最小的一份目录树视图，只取断言用得到的字段。
type apiclientTree struct {
	Path    string `json:"path"`
	Entries []struct {
		Path string `json:"path"`
	} `json:"entries"`
}
