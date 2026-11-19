package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testAPIKey = "0123456789abcdef"

// newCommandServer 起一个假服务端，把请求交给 handler；用例结束时自动关闭。
func newCommandServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// connectionArgs 返回指向假服务端的连接参数，避免每条用例重复拼配置路径。
func connectionArgs(t *testing.T, serverURL string) []string {
	t.Helper()

	return []string{
		"--server", serverURL,
		"--key", testAPIKey,
		"--config", filepath.Join(t.TempDir(), "absent.json"),
	}
}

// runCommand 执行一次命令，返回退出码与两路输出。
func runCommand(args []string) (int, string, string) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := Run(args, stdout, stderr)
	return code, stdout.String(), stderr.String()
}

// changeBody 是加入名单的请求体，字段名取自契约。
type changeBody struct {
	URLs    []string `json:"urls"`
	Branch  string   `json:"branch"`
	History string   `json:"history"`
	Reload  *bool    `json:"reload"`
}

// unblockBody 是解除停止同步的请求体。
type unblockBody struct {
	ClearFailures *bool `json:"clearFailures"`
}

func TestRepoAddSendsBatchAndReportsReload(t *testing.T) {
	t.Parallel()

	var (
		method string
		path   string
		body   changeBody
	)
	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"added":[{"name":"demo","url":"https://example.com/demo.git","branch":"main","effectiveHistory":"shallow"}],"repositories":2,"reloaded":{"configReloaded":true,"indexReloaded":true,"repositories":2}}`))
	})

	code, stdout, stderr := runCommand(append([]string{
		"repo", "add", "https://example.com/demo.git",
		"--branch", "main", "--history", "shallow",
	}, connectionArgs(t, server.URL)...))

	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if method != http.MethodPost || path != "/v1/repos" {
		t.Fatalf("request = %s %s", method, path)
	}
	if len(body.URLs) != 1 || body.URLs[0] != "https://example.com/demo.git" {
		t.Fatalf("urls = %#v", body.URLs)
	}
	if body.Branch != "main" || body.History != "shallow" {
		t.Fatalf("body = %#v", body)
	}
	if body.Reload == nil || !*body.Reload {
		t.Fatalf("reload = %v, want 显式为真", body.Reload)
	}
	for _, want := range []string{"已加入 demo", "分支 main", "生效档位 shallow", "名单共 2 个仓库", "已立即生效"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %q, 缺 %q", stdout, want)
		}
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRepoAddNoReloadSendsFalseAndExplains(t *testing.T) {
	t.Parallel()

	var body changeBody
	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"added":[{"name":"demo","url":"https://example.com/demo.git"}],"repositories":1}`))
	})

	code, stdout, stderr := runCommand(append([]string{
		"repo", "add", "https://example.com/demo.git", "--no-reload",
	}, connectionArgs(t, server.URL)...))

	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if body.Reload == nil || *body.Reload {
		t.Fatalf("reload = %v, want 显式为假", body.Reload)
	}
	if !strings.Contains(stdout, "未通知服务立即加载") {
		t.Fatalf("stdout = %q", stdout)
	}
	if strings.Contains(stdout, "已立即生效") {
		t.Fatalf("stdout = %q, 不该声称已生效", stdout)
	}
}

func TestRepoAddRejectsUnknownHistoryWithoutRequest(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("不该发出请求: %s %s", r.Method, r.URL.Path)
	})

	code, _, stderr := runCommand(append([]string{
		"repo", "add", "https://example.com/demo.git", "--history", "deep",
	}, connectionArgs(t, server.URL)...))

	if code != exitUsage {
		t.Fatalf("code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "shallow") || !strings.Contains(stderr, "full") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRepoAddRequiresURL(t *testing.T) {
	t.Parallel()

	code, _, stderr := runCommand([]string{"repo", "add"})
	if code != exitUsage {
		t.Fatalf("code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "用法") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRepoAddMapsContractFailures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		status  int
		code    string
		want    int
		message string
	}{
		{name: "重名", status: http.StatusConflict, code: "duplicate_name", want: exitConflict, message: "名单里已有同名仓库"},
		{name: "地址不合法", status: http.StatusBadRequest, code: "invalid_url", want: exitUsage, message: "地址不合法"},
		{name: "名字不合法", status: http.StatusBadRequest, code: "invalid_name", want: exitUsage, message: "推不出合法的仓库名"},
		{name: "档位不合法", status: http.StatusBadRequest, code: "invalid_history", want: exitUsage, message: "历史档位不合法"},
		{name: "分支不合法", status: http.StatusBadRequest, code: "invalid_branch", want: exitUsage, message: "分支名不合法"},
		{name: "请求体不合法", status: http.StatusBadRequest, code: "invalid_body", want: exitUsage, message: "请求体不是合法结构化数据"},
		{name: "写盘失败", status: http.StatusInternalServerError, code: "index_write_failed", want: exitFailure, message: "名单写盘失败"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(`{"code":"` + testCase.code + `","message":"服务端说明"}`))
			})

			code, _, stderr := runCommand(append([]string{
				"repo", "add", "https://example.com/demo.git",
			}, connectionArgs(t, server.URL)...))

			if code != testCase.want {
				t.Fatalf("code = %d, want %d（stderr = %q）", code, testCase.want, stderr)
			}
			if !strings.Contains(stderr, testCase.message) {
				t.Fatalf("stderr = %q, 缺 %q", stderr, testCase.message)
			}
			if !strings.Contains(stderr, testCase.code) {
				t.Fatalf("stderr = %q, 缺标识 %q", stderr, testCase.code)
			}
			if !strings.Contains(stderr, "服务端说明") {
				t.Fatalf("stderr = %q, 缺服务端原文", stderr)
			}
		})
	}
}

func TestRepoListMeasuresOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	var (
		lock    sync.Mutex
		measure []string
	)
	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/repos" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		lock.Lock()
		measure = append(measure, r.URL.Query().Get("measure"))
		lock.Unlock()
		_, _ = w.Write([]byte(`{"repositories":[{"name":"demo","branch":"main","url":"https://example.com/demo.git","history":"shallow","effectiveHistory":"shallow","onDisk":true,"gitBytes":1048576,"measuredBytes":2097152}]}`))
	})

	code, stdout, stderr := runCommand(append([]string{"repo", "list"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"demo", "https://example.com/demo.git", "shallow", "已落盘", "1.0 MB"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %q, 缺 %q", stdout, want)
		}
	}

	code, stdout, stderr = runCommand(append([]string{"repo", "list", "--sizes"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "2.0 MB") {
		t.Fatalf("stdout = %q, 缺现测体积", stdout)
	}

	lock.Lock()
	defer lock.Unlock()
	if len(measure) != 2 || measure[0] != "" || measure[1] != "true" {
		t.Fatalf("measure 参数 = %#v", measure)
	}
}

func TestRepoListShowsDeclaredVersusEffectiveHistory(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"repositories":[{"name":"demo","url":"https://example.com/demo.git","history":"full","effectiveHistory":"shallow","onDisk":false}]}`))
	})

	code, stdout, stderr := runCommand(append([]string{"repo", "list"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "full→shallow") {
		t.Fatalf("stdout = %q, 声明与生效不一致要写出来", stdout)
	}
	if !strings.Contains(stdout, "未落盘") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestRepoRemoveKeepsDataByDefault(t *testing.T) {
	t.Parallel()

	var (
		method   string
		path     string
		keepData string
	)
	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		keepData = r.URL.Query().Get("keepData")
		_, _ = w.Write([]byte(`{"name":"demo","removed":true,"dataExisted":true,"dataKept":true}`))
	})

	code, stdout, stderr := runCommand(append([]string{"repo", "rm", "demo"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if method != http.MethodDelete || path != "/v1/repos/demo" {
		t.Fatalf("request = %s %s", method, path)
	}
	if keepData != "true" {
		t.Fatalf("keepData = %q, want true", keepData)
	}
	if !strings.Contains(stdout, "磁盘数据保留") {
		t.Fatalf("stdout = %q", stdout)
	}
}

// 服务端说「操作前就没有副本」时，不能把它说成「数据被删了」。
func TestRepoRemoveReportsMissingCopyHonestly(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"demo","removed":true,"dataExisted":false,"dataKept":false}`))
	})

	code, stdout, stderr := runCommand(append([]string{"repo", "rm", "demo"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "磁盘上没有它的副本") {
		t.Fatalf("stdout = %q", stdout)
	}
	if strings.Contains(stdout, "已删除") {
		t.Fatalf("原本就没有副本，不该说成删除：%q", stdout)
	}
}

func TestRepoRemoveDropsDataWhenAsked(t *testing.T) {
	t.Parallel()

	var keepData string
	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		keepData = r.URL.Query().Get("keepData")
		_, _ = w.Write([]byte(`{"name":"demo","removed":true,"dataExisted":true,"dataKept":false}`))
	})

	code, stdout, stderr := runCommand(append([]string{
		"repo", "rm", "demo", "--drop-data",
	}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if keepData != "false" {
		t.Fatalf("keepData = %q, want false（--drop-data 才连数据一起删）", keepData)
	}
	if !strings.Contains(stdout, "磁盘数据已删除") {
		t.Fatalf("stdout = %q", stdout)
	}
}

// TestRepoRemoveHelpKeepsSafeDefault 守住「默认安全」：不加开关绝不删数据。
func TestRepoRemoveHelpKeepsSafeDefault(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runCommand([]string{"repo", "rm", "-h"})
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	// 子命令的帮助由 flag 包打出，走的是 stderr；两个流合起来看。
	help := stdout + stderr
	if strings.Contains(help, "keep-data") {
		t.Fatalf("帮助里仍有旧开关: %q", help)
	}
	if !strings.Contains(help, "-drop-data") {
		t.Fatalf("帮助里缺 --drop-data: %q", help)
	}
	// 默认必须是假：真值开关会在帮助里标出 (default true)。
	if strings.Contains(help, "(default true)") {
		t.Fatalf("开关不该默认开启: %q", help)
	}
}

func TestRepoRemoveMapsMissingRepository(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"repo_not_found","message":"仓库不存在"}`))
	})

	code, _, stderr := runCommand(append([]string{"repo", "rm", "ghost"}, connectionArgs(t, server.URL)...))
	if code != exitNotFound {
		t.Fatalf("code = %d, want %d", code, exitNotFound)
	}
	if !strings.Contains(stderr, "不在名单") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRepoRemoveWithoutChangeIsSuccess(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"ghost","removed":false,"dataKept":true}`))
	})

	code, stdout, stderr := runCommand(append([]string{"repo", "rm", "ghost"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, want 0（没有变化也是成功）", code)
	}
	if !strings.Contains(stdout, "不在名单里，未做改动") {
		t.Fatalf("stdout = %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRepoUnblockExplainsClearedState(t *testing.T) {
	t.Parallel()

	var (
		method string
		path   string
		body   unblockBody
	)
	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"name":"demo","cleared":true,"previous":{"blocked":true,"blockedSize":12582912,"remoteMissing":true,"consecutiveFailures":3,"lastError":"远端不可用"}}`))
	})

	code, stdout, stderr := runCommand(append([]string{"repo", "unblock", "demo"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if method != http.MethodPost || path != "/v1/repos/demo/unblock" {
		t.Fatalf("request = %s %s", method, path)
	}
	if body.ClearFailures == nil || !*body.ClearFailures {
		t.Fatalf("clearFailures = %v", body.ClearFailures)
	}
	for _, want := range []string{"已解除 demo", "体积超限 12.0 MB", "远端消失", "连续失败 3 次", "远端不可用"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %q, 缺 %q", stdout, want)
		}
	}
}

func TestRepoUnblockWithoutBlockIsSuccess(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"demo","cleared":false,"previous":{}}`))
	})

	code, stdout, stderr := runCommand(append([]string{"repo", "unblock", "demo"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, want 0（本来没被拦也是成功）", code)
	}
	if !strings.Contains(stdout, "未处于停止同步状态") {
		t.Fatalf("stdout = %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRepoUnblockRequiresRepository(t *testing.T) {
	t.Parallel()

	code, _, stderr := runCommand([]string{"repo", "unblock"})
	if code != exitUsage {
		t.Fatalf("code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "用法") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestRepoCompactSendsDryRunAndReportsWouldCompact(t *testing.T) {
	t.Parallel()

	var (
		method string
		path   string
		dryRun string
	)
	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		path = r.URL.Path
		dryRun = r.URL.Query().Get("dryRun")
		_, _ = w.Write([]byte(`{"name":"demo","action":"would_compact","branch":"main","before":{"shallow":false,"gitBytes":104857600},"after":{"shallow":true,"gitBytes":0},"durationMs":1500}`))
	})

	code, stdout, stderr := runCommand(append([]string{
		"repo", "compact", "--dry-run", "demo",
	}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if method != http.MethodPost || path != "/v1/repos/demo/compact" {
		t.Fatalf("request = %s %s", method, path)
	}
	if dryRun != "true" {
		t.Fatalf("dryRun = %q, want true", dryRun)
	}
	for _, want := range []string{"预演：将会就地转为浅克隆", "100.0 MB → -", "耗时 1.5s", "预演完成：待转换 1 个"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %q, 缺 %q", stdout, want)
		}
	}
}

func TestRepoCompactCoversEverySuccessAction(t *testing.T) {
	t.Parallel()

	actions := map[string]string{
		"alpha": "compacted",
		"bravo": "already_shallow",
		"delta": "skipped_full_history",
	}
	var (
		lock    sync.Mutex
		ordered []string
	)
	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/repos" {
			_, _ = w.Write([]byte(`{"repositories":[{"name":"delta"},{"name":"alpha"},{"name":"bravo"}]}`))
			return
		}

		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/repos/"), "/compact")
		lock.Lock()
		ordered = append(ordered, name)
		lock.Unlock()

		action := actions[name]
		_, _ = w.Write([]byte(`{"name":"` + name + `","action":"` + action + `","branch":"main","before":{"shallow":false,"gitBytes":10485760},"after":{"shallow":true,"gitBytes":1048576},"durationMs":2000}`))
	})

	code, stdout, stderr := runCommand(append([]string{"repo", "compact"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		"已就地转为浅克隆",
		"已是浅克隆，无需转换",
		"跳过（名单要求完整历史）",
		"完成：转换 1 个，已是浅克隆 1 个，要求完整历史 1 个",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %q, 缺 %q", stdout, want)
		}
	}
	lock.Lock()
	defer lock.Unlock()
	if strings.Join(ordered, ",") != "alpha,bravo,delta" {
		t.Fatalf("处理顺序 = %v, want 名单排序后的顺序", ordered)
	}
}

func TestRepoCompactMapsContractFailures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		code   string
		want   int
	}{
		{name: "正在同步", status: http.StatusConflict, code: "sync_busy", want: exitConflict},
		{name: "游离头指针", status: http.StatusBadRequest, code: "detached_head", want: exitUsage},
		{name: "远端不可达", status: 424, code: "remote_unreachable", want: exitNetwork},
		{name: "版本管理工具失败", status: http.StatusInternalServerError, code: "git_failed", want: exitFailure},
		{name: "不在名单", status: http.StatusNotFound, code: "repo_not_found", want: exitNotFound},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(`{"code":"` + testCase.code + `","message":"服务端说明"}`))
			})

			code, _, stderr := runCommand(append([]string{
				"repo", "compact", "demo",
			}, connectionArgs(t, server.URL)...))

			if code != testCase.want {
				t.Fatalf("code = %d, want %d（stderr = %q）", code, testCase.want, stderr)
			}
			if !strings.Contains(stderr, "失败") {
				t.Fatalf("stderr = %q", stderr)
			}
		})
	}
}

func TestRepoCompactWithoutRosterIsSuccess(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"repositories":[]}`))
	})

	code, stdout, stderr := runCommand(append([]string{"repo", "compact"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "名单里没有仓库") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestRepoCompactJSONStaysMachineReadable(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"repositories":[]}`))
	})

	code, stdout, stderr := runCommand(append([]string{
		"repo", "compact", "--json",
	}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}

	var payload struct {
		DryRun  bool  `json:"dryRun"`
		Results []any `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout = %q 不是合法 JSON: %v", stdout, err)
	}
	if payload.DryRun || len(payload.Results) != 0 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestRepoCheckJSONCountsEveryState(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/repos/ghost/check":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"repo_not_found","message":"仓库不存在"}`))
		case "/v1/repos/down/check":
			_, _ = w.Write([]byte(`{"repo":"down","reachable":false,"error":"remote unavailable"}`))
		default:
			_, _ = w.Write([]byte(`{"repo":"up","reachable":true}`))
		}
	})

	code, stdout, _ := runCommand(append([]string{
		"repo", "check", "up", "down", "ghost", "--json",
	}, connectionArgs(t, server.URL)...))
	if code != exitFailure {
		t.Fatalf("code = %d, want %d（第一个有问题的是不可达，按其它失败记）", code, exitFailure)
	}

	var payload struct {
		Checked     int `json:"checked"`
		Reachable   int `json:"reachable"`
		Unreachable int `json:"unreachable"`
		NotListed   int `json:"notListed"`
		Failed      int `json:"failed"`
		Results     []struct {
			Repo  string `json:"repo"`
			State string `json:"state"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout = %q 不是合法 JSON: %v", stdout, err)
	}
	if payload.Checked != 3 || payload.Reachable != 1 || payload.Unreachable != 1 || payload.NotListed != 1 || payload.Failed != 0 {
		t.Fatalf("payload = %#v", payload)
	}
	wanted := []string{"up:reachable", "down:unreachable", "ghost:not_listed"}
	for index, want := range wanted {
		if got := payload.Results[index].Repo + ":" + payload.Results[index].State; got != want {
			t.Fatalf("results[%d] = %q, want %q", index, got, want)
		}
	}
}

func TestRepoCheckProbesRosterInStableOrder(t *testing.T) {
	t.Parallel()

	delays := map[string]time.Duration{"alpha": 30 * time.Millisecond, "mid": 15 * time.Millisecond}
	var (
		lock   sync.Mutex
		probes = map[string]int{}
	)
	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/repos" {
			// 名单顺序故意乱掉：输出必须按名字排序，才能与名单改动无关。
			_, _ = w.Write([]byte(`{"repositories":[{"name":"zeta"},{"name":"alpha"},{"name":"mid"}]}`))
			return
		}

		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/repos/"), "/check")
		lock.Lock()
		probes[name]++
		lock.Unlock()

		time.Sleep(delays[name])
		_, _ = w.Write([]byte(`{"repo":"` + name + `","reachable":true}`))
	})

	code, stdout, stderr := runCommand(append([]string{"repo", "check"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}

	wanted := []string{"alpha\t可达", "mid\t可达", "zeta\t可达"}
	position := 0
	for _, want := range wanted {
		found := strings.Index(stdout[position:], want)
		if found < 0 {
			t.Fatalf("stdout = %q, 缺 %q 或顺序不对", stdout, want)
		}
		position += found
	}
	if !strings.Contains(stdout, "检查 3 个仓库：可达 3") {
		t.Fatalf("stdout = %q", stdout)
	}

	lock.Lock()
	defer lock.Unlock()
	for _, name := range []string{"alpha", "mid", "zeta"} {
		if probes[name] != 1 {
			t.Fatalf("%s 探测了 %d 次", name, probes[name])
		}
	}
}

func TestRepoCheckSeparatesMissingFromUnreachable(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/repos" {
			t.Errorf("给了仓库名就不该再取名单")
		}
		switch r.URL.Path {
		case "/v1/repos/ghost/check":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"repo_not_found","message":"仓库不存在"}`))
		case "/v1/repos/down/check":
			_, _ = w.Write([]byte(`{"repo":"down","reachable":false,"error":"remote repository unavailable"}`))
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	})

	code, stdout, stderr := runCommand(append([]string{
		"repo", "check", "ghost", "down",
	}, connectionArgs(t, server.URL)...))

	if code != exitNotFound {
		t.Fatalf("code = %d, want %d（stderr = %q）", code, exitNotFound, stderr)
	}
	if !strings.Contains(stdout, "ghost\t不在名单") {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "down\t不可达\tremote repository unavailable") {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stdout, "不在名单 1，探测失败 0") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestRepoCheckMapsProbeFailure(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"unauthorized","message":"key 无效"}`))
	})

	code, stdout, stderr := runCommand(append([]string{
		"repo", "check", "demo",
	}, connectionArgs(t, server.URL)...))

	if code != exitAuth {
		t.Fatalf("code = %d, want %d（stderr = %q）", code, exitAuth, stderr)
	}
	if !strings.Contains(stdout, "demo\t探测失败") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestCacheListShowsEntriesAndTotal(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/cache" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"files":[{"name":"demo-abcdef123456.zip","repo":"demo","revision":"abcdef1234567890","bytes":2097152,"modifiedAt":"2026-01-02T03:04:05Z"}],"totalBytes":2097152}`))
	})

	code, stdout, stderr := runCommand(append([]string{"cache", "list"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"demo-abcdef123456.zip", "demo", "abcdef123456", "2.0 MB", "合计 1 个文件"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %q, 缺 %q", stdout, want)
		}
	}
}

func TestCacheListEmptyIsSuccess(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"files":[],"totalBytes":0}`))
	})

	code, stdout, stderr := runCommand(append([]string{"cache", "list"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, want 0（目录不存在等于空清单）", code)
	}
	if !strings.Contains(stdout, "缓存为空") {
		t.Fatalf("stdout = %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestCacheCleanRequiresConfirmation(t *testing.T) {
	t.Parallel()

	var confirm string
	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		confirm = r.URL.Query().Get("confirm")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"confirm_required","message":"未确认"}`))
	})

	code, _, stderr := runCommand(append([]string{"cache", "clean"}, connectionArgs(t, server.URL)...))
	if code != exitUsage {
		t.Fatalf("code = %d, want %d", code, exitUsage)
	}
	if confirm != "" {
		t.Fatalf("confirm = %q, 没确认就不该带确认参数", confirm)
	}
	if !strings.Contains(stderr, "显式确认") || !strings.Contains(stderr, "--yes") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestCacheCleanSendsConfirmAndReportsSkips(t *testing.T) {
	t.Parallel()

	var (
		method  string
		repo    string
		confirm string
	)
	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		repo = r.URL.Query().Get("repo")
		confirm = r.URL.Query().Get("confirm")
		_, _ = w.Write([]byte(`{"removed":2,"bytes":3145728,"skipped":[{"name":"demo-9.zip","reason":"正在生成"}]}`))
	})

	code, stdout, stderr := runCommand(append([]string{
		"cache", "clean", "--yes", "demo",
	}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if method != http.MethodDelete {
		t.Fatalf("method = %q", method)
	}
	if repo != "demo" || confirm != "1" {
		t.Fatalf("query = repo=%q confirm=%q", repo, confirm)
	}
	if !strings.Contains(stdout, "已清理 仓库 demo 的缓存：2 个文件，3.0 MB") {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "跳过 demo-9.zip：正在生成") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestCacheCleanBusyMapsToConflict(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"cache_busy","message":"正在生成"}`))
	})

	code, _, stderr := runCommand(append([]string{"cache", "clean", "--yes"}, connectionArgs(t, server.URL)...))
	if code != exitConflict {
		t.Fatalf("code = %d, want %d", code, exitConflict)
	}
	if !strings.Contains(stderr, "缓存正在生成") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestCacheCleanNothingToDoIsSuccess(t *testing.T) {
	t.Parallel()

	server := newCommandServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"removed":0,"bytes":0}`))
	})

	code, stdout, stderr := runCommand(append([]string{"cache", "clean", "--yes"}, connectionArgs(t, server.URL)...))
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "没有可清理的产物") {
		t.Fatalf("stdout = %q", stdout)
	}
}
