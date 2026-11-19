package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunWithoutCommandPrintsUsage(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	if code := Run(nil, stdout, stderr); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "用法") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	t.Parallel()

	code := Run([]string{"nope"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestRunHelp(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	if code := Run([]string{"help"}, stdout, &bytes.Buffer{}); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "eucli-dw") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestConfigSetAndShow(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")

	if code := Run([]string{"config", "set", "server", "http://127.0.0.1:9997", "--config", configPath}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("config set server code = %d", code)
	}
	if code := Run([]string{"config", "set", "key", "0123456789abcdef", "--config", configPath}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("config set key code = %d", code)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var stored struct {
		Server string `json:"server"`
		APIKey string `json:"apiKey"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if stored.Server != "http://127.0.0.1:9997" || stored.APIKey != "0123456789abcdef" {
		t.Fatalf("stored = %#v", stored)
	}

	stdout := &bytes.Buffer{}
	if code := Run([]string{"config", "show", "--config", configPath}, stdout, &bytes.Buffer{}); code != 0 {
		t.Fatalf("config show code = %d", code)
	}
	if strings.Contains(stdout.String(), "0123456789abcdef") {
		t.Fatalf("show leaked key: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "0123****cdef") {
		t.Fatalf("show = %q", stdout.String())
	}
}

func TestConfigSetRejectsInvalidServer(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.json")
	stderr := &bytes.Buffer{}

	code := Run([]string{"config", "set", "server", "not-a-url", "--config", configPath}, &bytes.Buffer{}, stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("config 不应被写入: %v", err)
	}
}

func TestGetPrintsContent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer 0123456789abcdef" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte("hello\n"))
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	code := Run([]string{
		"get", "example", "README.md",
		"--server", server.URL,
		"--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json"),
	}, stdout, &bytes.Buffer{})

	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if stdout.String() != "hello\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestGetWithoutConfigurationExplainsSetup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", t.TempDir())
	} else {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	}
	t.Setenv("EUCLI_DW_CONFIG", "")

	stderr := &bytes.Buffer{}
	if code := Run([]string{"get", "example", "README.md"}, &bytes.Buffer{}, stderr); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "config set server") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestGetRequiresArguments(t *testing.T) {
	t.Parallel()

	if code := Run([]string{"get"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestHelpIsAvailableEverywhere(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		{"help"},
		{"--help"},
		{"tree", "--help"},
		{"config", "--help"},
		{"get", "--help"},
		{"sync", "-h"},
	}

	for _, args := range cases {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		code := Run(args, stdout, stderr)

		if code != 0 {
			t.Fatalf("%v code = %d, want 0", args, code)
		}
		if stdout.Len() == 0 && stderr.Len() == 0 {
			t.Fatalf("%v 没有任何帮助输出", args)
		}
	}
}

func TestUnknownSubcommandExplainsUsage(t *testing.T) {
	t.Parallel()

	stderr := &bytes.Buffer{}
	if code := Run([]string{"config", "nope"}, &bytes.Buffer{}, stderr); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "子命令") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestReposListsServerRepositories(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/repos":
			_, _ = w.Write([]byte(`{"repositories":[{"name":"example","branch":"main","revision":"abcdef1234567890"}]}`))
		case "/v1/state":
			_, _ = w.Write([]byte(`{"repositories":{}}`))
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	code := Run([]string{"repos", "--server", server.URL, "--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json")}, stdout, &bytes.Buffer{})

	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stdout.String(), "example") || !strings.Contains(stdout.String(), "abcdef123456") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestSyncAllRejectsRepositoryArgument(t *testing.T) {
	t.Parallel()

	stderr := &bytes.Buffer{}
	code := Run([]string{"sync", "--all", "some-repo"}, &bytes.Buffer{}, stderr)

	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "不能同时使用") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestJSONModeEmitsStructuredError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"repo_not_found","message":"仓库不存在"}`))
	}))
	defer server.Close()

	stderr := &bytes.Buffer{}
	code := Run([]string{"get", "ghost", "README.md", "--json",
		"--server", server.URL, "--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json")}, &bytes.Buffer{}, stderr)

	if code != exitNotFound {
		t.Fatalf("code = %d, want %d", code, exitNotFound)
	}

	var payload struct {
		OK     bool   `json:"ok"`
		Code   string `json:"code"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &payload); err != nil {
		t.Fatalf("stderr 不是合法 JSON: %v (%q)", err, stderr.String())
	}
	if payload.OK || payload.Code != "repo_not_found" || payload.Status != 404 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestReposMarksRemoteMissing(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/repos":
			// 合并后的视图把远端状态直接放在仓库条目里，不必再单独查一次状态接口。
			_, _ = w.Write([]byte(`{"repositories":[{"name":"example","branch":"main","revision":"abcdef1234567890","remoteMissing":true}]}`))
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	code := Run([]string{"repos", "--server", server.URL, "--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json")}, stdout, &bytes.Buffer{})

	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stdout.String(), "MISSING") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestSyncJSONOutputsResult(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/repos/example/sync" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"name":"example","action":"updated","branch":"main","duration":1500000000}`))
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	code := Run([]string{"sync", "example", "--json",
		"--server", server.URL, "--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json")}, stdout, &bytes.Buffer{})

	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	var payload struct {
		Name       string `json:"name"`
		Action     string `json:"action"`
		DurationMs int64  `json:"durationMs"`
		Error      string `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v (%q)", err, stdout.String())
	}
	if payload.Name != "example" || payload.Action != "updated" || payload.DurationMs != 1500 || payload.Error != "" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestSyncJSONReportsRepositoryFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"example","action":"failed","duration":0,"error":"boom"}`))
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := Run([]string{"sync", "example", "--json",
		"--server", server.URL, "--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json")}, stdout, stderr)

	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}

	var result struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v (%q)", err, stdout.String())
	}
	if result.Error != "boom" {
		t.Fatalf("stdout = %s", stdout.String())
	}

	var failure struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &failure); err != nil {
		t.Fatalf("stderr 不是合法 JSON: %v (%q)", err, stderr.String())
	}
	if failure.OK || failure.Message != "boom" {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestSyncAllJSONOutputsStarted(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sync" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	code := Run([]string{"sync", "--all", "--json",
		"--server", server.URL, "--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json")}, stdout, &bytes.Buffer{})

	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v (%q)", err, stdout.String())
	}
	if payload.Status != "started" {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestFindHelpStatesDefaultAndMaxLimit(t *testing.T) {
	t.Parallel()

	stderr := &bytes.Buffer{}
	if code := Run([]string{"find", "--help"}, &bytes.Buffer{}, stderr); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	output := stderr.String()
	if !strings.Contains(output, "default 200") {
		t.Fatalf("帮助下缺少默认值: %q", output)
	}
	if !strings.Contains(output, "上限 10000") {
		t.Fatalf("帮助下缺少上限: %q", output)
	}
}

func TestFindSendsDefaultLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "200" {
			t.Errorf("limit = %q, want 200", got)
		}
		_, _ = w.Write([]byte(`{"repo":"example","pattern":"*.md","offset":0,"entries":[{"name":"a.md","path":"docs/a.md","type":"file","size":1}],"scannedFiles":2,"truncated":false}`))
	}))
	defer server.Close()

	code := Run([]string{"find", "example", "*.md",
		"--server", server.URL, "--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json")}, &bytes.Buffer{}, &bytes.Buffer{})

	if code != 0 {
		t.Fatalf("code = %d", code)
	}
}

func TestFindTruncatedHintStatesMaxLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if got := query.Get("limit"); got != "20000" {
			t.Errorf("limit = %q, want 20000", got)
		}
		if got := query.Get("offset"); got != "5" {
			t.Errorf("offset = %q, want 5", got)
		}
		_, _ = w.Write([]byte(`{"repo":"example","pattern":"*.md","offset":5,"entries":[{"name":"a.md","path":"docs/a.md","type":"file","size":12}],"scannedFiles":5,"truncated":true}`))
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := Run([]string{"find", "example", "*.md", "--limit", "20000", "--offset", "5",
		"--server", server.URL, "--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json")}, stdout, stderr)

	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	// 条目走 stdout，关于条目的说明走 stderr，管道下游只需处理数据。
	if !strings.Contains(stdout.String(), "docs/a.md") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	output := stderr.String()
	if !strings.Contains(output, "单次最多返回 10000 条") {
		t.Fatalf("stderr = %q", output)
	}
	if !strings.Contains(output, "--limit 已按上限处理") {
		t.Fatalf("stderr = %q", output)
	}
	if !strings.Contains(output, "--offset 6") {
		t.Fatalf("stderr = %q", output)
	}
}

func TestReloadCommandPrintsResult(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/reload" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"configReloaded": true,
			"indexReloaded": true,
			"repositories": 3,
			"needsRestart": ["httpAddress"]
		}`))
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	code := Run([]string{
		"reload", "--server", server.URL, "--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json"),
	}, stdout, &bytes.Buffer{})

	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	output := stdout.String()
	if !strings.Contains(output, "已重载") || !strings.Contains(output, "仓库数:   3") {
		t.Fatalf("stdout = %q", output)
	}
	if !strings.Contains(output, "需要重启: httpAddress") {
		t.Fatalf("stdout = %q", output)
	}
}

func TestReloadCommandReportsFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"configReloaded": false,
			"indexReloaded": true,
			"repositories": 1,
			"needsRestart": [],
			"error": "配置加载失败（config.json）: invalid character"
		}`))
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := Run([]string{
		"reload", "--json", "--server", server.URL, "--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json"),
	}, stdout, stderr)

	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), `"configReloaded": false`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), `"ok": false`) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestStatusShowsRuntimeReloadState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			_, _ = w.Write([]byte(`{
				"running": false,
				"total": 2,
				"summary": {},
				"results": [],
				"singleResults": [],
				"configLoadedAt": "2025-03-04T05:06:07Z",
				"indexLoadedAt": "2025-03-04T05:06:08Z",
				"repositoryCount": 2,
				"syncInterval": "90s",
				"gcThresholdMB": 12,
				"needsRestart": ["httpAddress"],
				"lastReload": {
					"at": "2025-03-04T05:06:09Z",
					"configReloaded": false,
					"indexReloaded": true,
					"error": "配置加载失败（config.json）: invalid character"
				}
			}`))
		case "/v1/state":
			_, _ = w.Write([]byte(`{"repositories":{}}`))
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	stdout := &bytes.Buffer{}
	code := Run([]string{
		"status", "--server", server.URL, "--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json"),
	}, stdout, &bytes.Buffer{})

	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	output := stdout.String()
	if !strings.Contains(output, "仓库数:   2") || !strings.Contains(output, "同步周期: 90s") {
		t.Fatalf("stdout = %q", output)
	}
	if !strings.Contains(output, "最近重载: 失败（配置加载失败") {
		t.Fatalf("stdout = %q", output)
	}
	if !strings.Contains(output, "需要重启: httpAddress") {
		t.Fatalf("stdout = %q", output)
	}
}

// runWithServer 用一份指向假服务的参数执行一次查询命令。
func runWithServer(t *testing.T, server *httptest.Server, args ...string) (int, string, string) {
	t.Helper()

	full := append(append([]string{}, args...),
		"--server", server.URL,
		"--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json"),
	)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := Run(full, stdout, stderr)
	return code, stdout.String(), stderr.String()
}

func TestGetSendsLineRangeAndReportsRest(t *testing.T) {
	t.Parallel()

	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Header().Set("X-File-Total-Lines", "40")
		w.Header().Set("X-File-First-Line", "3")
		w.Header().Set("X-File-Last-Line", "12")
		_, _ = w.Write([]byte("body\n"))
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "get", "example", "doc.txt", "--lines", "3 - 12")

	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if got := query.Get("lines"); got != "3-12" {
		t.Fatalf("lines = %q, want 3-12", got)
	}
	if stdout != "body\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "--lines 13-") {
		t.Fatalf("stderr 应提示如何继续：%q", stderr)
	}
}

func TestGetReadsMultiplePathsInOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("path") {
		case "a.txt":
			// 第一段更慢：输出顺序仍然由位置参数决定，与完成先后无关。
			time.Sleep(20 * time.Millisecond)
			_, _ = w.Write([]byte("aaa\n"))
		case "b.txt":
			_, _ = w.Write([]byte("bbb\n"))
		default:
			t.Errorf("path = %q", r.URL.Query().Get("path"))
		}
	}))
	defer server.Close()

	code, stdout, _ := runWithServer(t, server, "get", "example", "a.txt", "b.txt")

	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	want := "==> a.txt <==\naaa\n==> b.txt <==\nbbb\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestGetRejectsInvalidLineRange(t *testing.T) {
	t.Parallel()

	stderr := &bytes.Buffer{}
	code := Run([]string{"get", "example", "doc.txt", "--lines", "9-3"}, &bytes.Buffer{}, stderr)

	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "起始行") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestGetSendsMaxBytesAndExplainsTruncation(t *testing.T) {
	t.Parallel()

	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		w.Header().Set("X-File-Total-Lines", "40")
		w.Header().Set("X-File-First-Line", "1")
		w.Header().Set("X-File-Last-Line", "3")
		w.Header().Set("X-File-Truncated", "bytes")
		_, _ = w.Write([]byte("one\n"))
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "get", "example", "doc.txt", "--max-bytes", "4096")

	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if got := query.Get("maxBytes"); got != "4096" {
		t.Fatalf("maxBytes = %q", got)
	}
	if stdout != "one\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "--max-bytes 截断到第 3 行") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestTreeSendsDepthAndDirsOnly(t *testing.T) {
	t.Parallel()

	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = w.Write([]byte(`{"repo":"example","path":"","depth":2,"entries":[
			{"name":"docs","path":"docs","type":"dir","depth":1},
			{"name":"guide.md","path":"docs/guide.md","type":"file","size":9,"depth":2}
		],"truncated":false}`))
	}))
	defer server.Close()

	code, stdout, _ := runWithServer(t, server, "tree", "example", "--depth", "2", "--dirs-only")

	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if got := query.Get("depth"); got != "2" {
		t.Fatalf("depth = %q", got)
	}
	if got := query.Get("dirsOnly"); got != "true" {
		t.Fatalf("dirsOnly = %q", got)
	}
	if !strings.Contains(stdout, "docs/") || !strings.Contains(stdout, "docs/guide.md\t9") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestTreeRejectsZeroDepth(t *testing.T) {
	t.Parallel()

	stderr := &bytes.Buffer{}
	if code := Run([]string{"tree", "example", "--depth", "0"}, &bytes.Buffer{}, stderr); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func TestFindSendsSubstringMatch(t *testing.T) {
	t.Parallel()

	queries := make([]url.Values, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query())
		_, _ = w.Write([]byte(`{"repo":"example","pattern":"main","entries":[],"scannedFiles":0,"truncated":false}`))
	}))
	defer server.Close()

	if code, _, _ := runWithServer(t, server, "find", "example", "main"); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if got := queries[0].Get("match"); got != "" {
		t.Fatalf("默认不该带 match：%q", got)
	}

	if code, _, _ := runWithServer(t, server, "find", "example", "main", "--substring"); code != 0 {
		t.Fatalf("code = %d", code)
	}
	if got := queries[1].Get("match"); got != "substring" {
		t.Fatalf("match = %q, want substring", got)
	}
}

func TestReposSortsBySize(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"repositories":[
			{"name":"small","gitBytes":1024},
			{"name":"large","gitBytes":1048576},
			{"name":"medium","gitBytes":4096}
		]}`))
	}))
	defer server.Close()

	code, stdout, _ := runWithServer(t, server, "repos", "--sort", "size")

	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	order := make([]string, 0, 3)
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		order = append(order, strings.SplitN(line, "\t", 2)[0])
	}
	want := []string{"large", "medium", "small"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %#v, want %#v", order, want)
	}
}

func TestReposSortsBySyncTime(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"repositories":[
			{"name":"old","lastSyncAt":"2026-01-01T00:00:00Z"},
			{"name":"never"},
			{"name":"fresh","lastSyncAt":"2026-05-01T00:00:00Z"}
		]}`))
	}))
	defer server.Close()

	code, stdout, _ := runWithServer(t, server, "repos", "--sort", "time")

	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	order := make([]string, 0, 3)
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		order = append(order, strings.SplitN(line, "\t", 2)[0])
	}
	want := []string{"fresh", "old", "never"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %#v, want %#v", order, want)
	}
}

func TestOverviewRendersRepositorySummary(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/repos/example":
			_, _ = w.Write([]byte(`{"name":"example","branch":"main","revision":"abcdef1234567890","shallow":true,"gitBytes":2097152,"lastSyncAt":"2026-05-01T08:30:00Z"}`))
		case "/v1/repos/example/tree":
			_, _ = w.Write([]byte(`{"repo":"example","path":"","depth":2,"entries":[
				{"name":"README.md","path":"README.md","type":"file","size":20,"depth":1},
				{"name":"go.mod","path":"go.mod","type":"file","size":32,"depth":1},
				{"name":"docs","path":"docs","type":"dir","depth":1}
			],"truncated":false}`))
		case "/v1/repos/example/file":
			w.Header().Set("X-File-Total-Lines", "3")
			w.Header().Set("X-File-First-Line", "1")
			w.Header().Set("X-File-Last-Line", "1")
			_, _ = w.Write([]byte("# example\n"))
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "overview", "example")

	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"example", "浅克隆", "== 目录（2 层，3 项） ==", "== README.md", "== go.mod"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout 缺少 %q：\n%s", want, stdout)
		}
	}
}

func TestSearchRendersContextWithoutDroppingMatches(t *testing.T) {
	t.Parallel()

	// 两处命中相邻，上下文互相重叠：命中行必须按命中格式出现一次，不能缩成上下文。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"repo":"example","query":"needle","offset":0,"scannedFiles":1,"truncated":false,"matches":[
			{"path":"docs/a.md","line":3,"text":"third needle","before":[{"line":2,"text":"second"}],"after":[{"line":4,"text":"fourth needle"}]},
			{"path":"docs/a.md","line":4,"text":"fourth needle","before":[{"line":3,"text":"third needle"}],"after":[{"line":5,"text":"fifth"}]}
		]}`))
	}))
	defer server.Close()

	code, stdout, _ := runWithServer(t, server, "search", "example", "needle", "--context", "1")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}

	want := strings.Join([]string{
		"docs/a.md-2- second",
		"docs/a.md:3: third needle",
		"docs/a.md:4: fourth needle",
		"docs/a.md-5- fifth",
		"",
	}, "\n")
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestSearchPrintsOversizedNotice(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"repo":"example","query":"needle","offset":0,"scannedFiles":0,"truncated":false,"matches":[],
			"oversizedFiles":[{"path":"huge.js","size":52428800}],"oversizedFilesCount":3}`))
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "search", "example", "needle")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"跳过 3 个", "huge.js", "另有 2 个未列出"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr 缺少 %q：%q", want, stderr)
		}
	}
}

func TestFindSuggestsSubstringWhenExactMisses(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("match") == "substring" {
			_, _ = w.Write([]byte(`{"repo":"example","pattern":"main","entries":[
				{"name":"main.go","path":"pkg/main.go","type":"file","size":31},
				{"name":"main_test.go","path":"pkg/main_test.go","type":"file","size":35}
			],"scannedFiles":5,"truncated":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"repo":"example","pattern":"main","entries":[],"scannedFiles":5,"truncated":false}`))
	}))
	defer server.Close()

	code, stdout, stderr := runWithServer(t, server, "find", "example", "main")

	if code != 0 {
		t.Fatalf("code = %d, want 0（落空不是错误）", code)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"没有匹配的文件", "另有 2 个文件名包含 \"main\"", "pkg/main.go", "加 --substring 查看"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr 缺少 %q：%q", want, stderr)
		}
	}
}

func TestFindSkipsSuggestionWhenItCannotApply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "已显式要求子串", args: []string{"find", "example", "main", "--substring"}},
		{name: "模式含通配符", args: []string{"find", "example", "*.md"}},
		{name: "正在翻页", args: []string{"find", "example", "main", "--offset", "10"}},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var calls int
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls++
				mu.Unlock()
				_, _ = w.Write([]byte(`{"repo":"example","pattern":"main","entries":[],"scannedFiles":5,"truncated":false}`))
			}))
			defer server.Close()

			if code, _, _ := runWithServer(t, server, testCase.args...); code != 0 {
				t.Fatalf("code = %d", code)
			}

			mu.Lock()
			defer mu.Unlock()
			if calls != 1 {
				t.Fatalf("请求次数 = %d, want 1（不该为提示再查一次）", calls)
			}
		})
	}
}
