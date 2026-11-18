package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"eucli-deep-wiki/internal/repoindex"
	"eucli-deep-wiki/internal/state"
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

func TestRepoListReadsIndex(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	indexPath := filepath.Join(dataDir, "repositories.json")
	indexBody := `{"repositories":[{"name":"example","url":"https://github.com/example/example.git"}]}`
	if err := os.WriteFile(indexPath, []byte(indexBody), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "config.json")
	configBody := `{
		"dataRoot": "` + filepath.ToSlash(dataDir) + `",
		"apiKeys": ["0123456789abcdef"]
	}`
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	stdout := &bytes.Buffer{}
	code := Run([]string{"repo", "list", "--service-config", configPath}, stdout, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stdout.String(), "example") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestHelpIsAvailableEverywhere(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		{"help"},
		{"--help"},
		{"repo", "--help"},
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
	if code := Run([]string{"repo", "nope"}, &bytes.Buffer{}, stderr); code != 2 {
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

func TestRepoCheckProbesEveryIndexedRepository(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer 0123456789abcdef" {
			t.Errorf("Authorization = %q", got)
		}
		switch r.URL.Path {
		case "/v1/repos/alive/check":
			_, _ = w.Write([]byte(`{"repo":"alive","reachable":true}`))
		case "/v1/repos/gone/check":
			_, _ = w.Write([]byte(`{"repo":"gone","reachable":false,"error":"Repository not found"}`))
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	dataDir := t.TempDir()
	indexBody := `{"repositories":[
		{"name":"alive","url":"https://github.com/example/alive.git"},
		{"name":"gone","url":"https://github.com/example/gone.git"}
	]}`
	if err := os.WriteFile(filepath.Join(dataDir, "repositories.json"), []byte(indexBody), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	configPath := writeServiceConfig(t, dataDir, server.URL)

	stdout := &bytes.Buffer{}
	code := Run([]string{"repo", "check", "--service-config", configPath}, stdout, &bytes.Buffer{})

	if code != 1 {
		t.Fatalf("code = %d, want 1（存在不可达仓库）", code)
	}
	output := stdout.String()
	if !strings.Contains(output, "alive\t可达") {
		t.Fatalf("stdout = %q", output)
	}
	if !strings.Contains(output, "gone\t不可达\tRepository not found") {
		t.Fatalf("stdout = %q", output)
	}
}

func TestRepoCheckAcceptsExplicitRepository(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/repos/alive/check" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"repo":"alive","reachable":true}`))
	}))
	defer server.Close()

	dataDir := t.TempDir()
	indexBody := `{"repositories":[
		{"name":"alive","url":"https://github.com/example/alive.git"},
		{"name":"gone","url":"https://github.com/example/gone.git"}
	]}`
	if err := os.WriteFile(filepath.Join(dataDir, "repositories.json"), []byte(indexBody), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	configPath := writeServiceConfig(t, dataDir, server.URL)

	stdout := &bytes.Buffer{}
	code := Run([]string{"repo", "check", "alive", "--service-config", configPath}, stdout, &bytes.Buffer{})

	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if output := stdout.String(); !strings.Contains(output, "alive\t可达") {
		t.Fatalf("stdout = %q", output)
	}
}

func TestRepoUnblockClearsRemoteMissing(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	configPath := writeServiceConfig(t, dataDir, "http://127.0.0.1:9997")

	statePath := filepath.Join(dataDir, state.FileName)
	stateBody := `{"repositories":{"example":{"consecutiveFailures":3,"lastError":"Repository not found","remoteMissing":true,"remoteMissingAt":"2025-03-04T05:06:07Z"}}}`
	if err := os.WriteFile(statePath, []byte(stateBody), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	stdout := &bytes.Buffer{}
	if code := Run([]string{"repo", "unblock", "example", "--service-config", configPath}, stdout, &bytes.Buffer{}); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "已解除") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	guard, err := state.Open(statePath)
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	got := guard.Get("example")
	if got.RemoteMissing || !got.RemoteMissingAt.IsZero() || got.ConsecutiveFailures != 0 || got.LastError != "" {
		t.Fatalf("state = %#v, want cleared", got)
	}
}

func TestRepoListMarksRemoteMissing(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	indexBody := `{"repositories":[{"name":"example","url":"https://github.com/example/example.git"}]}`
	if err := os.WriteFile(filepath.Join(dataDir, "repositories.json"), []byte(indexBody), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	stateBody := `{"repositories":{"example":{"consecutiveFailures":3,"remoteMissing":true}}}`
	if err := os.WriteFile(filepath.Join(dataDir, state.FileName), []byte(stateBody), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	configPath := writeServiceConfig(t, dataDir, "http://127.0.0.1:9997")

	stdout := &bytes.Buffer{}
	if code := Run([]string{"repo", "list", "--service-config", configPath}, stdout, &bytes.Buffer{}); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "MISSING") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestReposMarksRemoteMissing(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/repos":
			_, _ = w.Write([]byte(`{"repositories":[{"name":"example","branch":"main","revision":"abcdef1234567890"}]}`))
		case "/v1/state":
			_, _ = w.Write([]byte(`{"repositories":{"example":{"remoteMissing":true}}}`))
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

// writeServiceConfig 写一份指向指定地址的服务配置，返回配置路径。
func writeServiceConfig(t *testing.T, dataDir string, serverURL string) string {
	t.Helper()

	address := strings.TrimPrefix(serverURL, "http://")
	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"dataRoot": "` + filepath.ToSlash(dataDir) + `",
		"httpAddress": "` + address + `",
		"apiKeys": ["0123456789abcdef"]
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
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
	code := Run([]string{"find", "example", "*.md", "--limit", "20000", "--offset", "5",
		"--server", server.URL, "--key", "0123456789abcdef",
		"--config", filepath.Join(t.TempDir(), "absent.json")}, stdout, &bytes.Buffer{})

	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	output := stdout.String()
	if !strings.Contains(output, "单次最多返回 10000 条") {
		t.Fatalf("stdout = %q", output)
	}
	if !strings.Contains(output, "--limit 已按上限处理") {
		t.Fatalf("stdout = %q", output)
	}
	if !strings.Contains(output, "--offset 6") {
		t.Fatalf("stdout = %q", output)
	}
}

func TestRepoCheckKeepsGoingWhenRepositoryUnknown(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/repos/alive/check":
			_, _ = w.Write([]byte(`{"repo":"alive","reachable":true}`))
		case "/v1/repos/ghost/check":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"repo_not_found","message":"仓库不存在或尚未同步"}`))
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	configPath := writeServiceConfig(t, t.TempDir(), server.URL)

	stdout := &bytes.Buffer{}
	code := Run([]string{"repo", "check", "alive", "ghost", "--service-config", configPath}, stdout, &bytes.Buffer{})

	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	output := stdout.String()
	if !strings.Contains(output, "alive\t可达") {
		t.Fatalf("stdout = %q", output)
	}
	if !strings.Contains(output, "ghost\t未知仓库") {
		t.Fatalf("stdout = %q", output)
	}
	if !strings.Contains(output, "检查 2 个仓库") || !strings.Contains(output, "未知 1") {
		t.Fatalf("stdout = %q", output)
	}
}

func TestRepoCheckJSONOutput(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"repo":"alive","reachable":true}`))
	}))
	defer server.Close()

	configPath := writeServiceConfig(t, t.TempDir(), server.URL)

	stdout := &bytes.Buffer{}
	code := Run([]string{"repo", "check", "alive", "--json", "--service-config", configPath}, stdout, &bytes.Buffer{})

	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}

	var payload struct {
		Checked   int `json:"checked"`
		Reachable int `json:"reachable"`
		Results   []struct {
			Repo      string `json:"repo"`
			Reachable bool   `json:"reachable"`
		} `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v (%q)", err, stdout.String())
	}
	if payload.Checked != 1 || payload.Reachable != 1 || len(payload.Results) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
}

// requestLog 记录假服务收到的请求，供断言命令到底发了什么。
type requestLog struct {
	mu       sync.Mutex
	requests []string
}

func (l *requestLog) record(request *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = append(l.requests, request.Method+" "+request.URL.Path)
}

func (l *requestLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.requests...)
}

func TestRepoAddReloadsRunningService(t *testing.T) {
	t.Parallel()

	log := &requestLog{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		if r.Header.Get("Authorization") != "Bearer 0123456789abcdef" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"configReloaded":true,"indexReloaded":true,"repositories":1,"needsRestart":[]}`))
	}))
	defer server.Close()

	dataDir := t.TempDir()
	configPath := writeServiceConfig(t, dataDir, server.URL)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := Run([]string{
		"repo", "add", "https://github.com/example/example.git",
		"--service-config", configPath,
	}, stdout, stderr)

	if code != 0 {
		t.Fatalf("code = %d, want 0（stderr = %q）", code, stderr.String())
	}
	if requests := log.snapshot(); len(requests) != 1 || requests[0] != "POST /v1/reload" {
		t.Fatalf("requests = %#v", requests)
	}
	if !strings.Contains(stdout.String(), "added example") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "reloaded 1") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	index, err := repoindex.Load(filepath.Join(dataDir, "repositories.json"))
	if err != nil {
		t.Fatalf("repoindex.Load() error = %v", err)
	}
	if len(index.Repositories) != 1 || index.Repositories[0].Name != "example" {
		t.Fatalf("Repositories = %#v", index.Repositories)
	}
}

func TestRepoAddSkipsReloadWithFlag(t *testing.T) {
	t.Parallel()

	log := &requestLog{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		_, _ = w.Write([]byte(`{"configReloaded":true,"indexReloaded":true,"repositories":1}`))
	}))
	defer server.Close()

	configPath := writeServiceConfig(t, t.TempDir(), server.URL)

	stdout := &bytes.Buffer{}
	code := Run([]string{
		"repo", "add", "https://github.com/example/example.git", "--no-reload",
		"--service-config", configPath,
	}, stdout, &bytes.Buffer{})

	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if requests := log.snapshot(); len(requests) != 0 {
		t.Fatalf("--no-reload 不该发请求，requests = %#v", requests)
	}
	if strings.Contains(stdout.String(), "reloaded") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRepoAddWarnsWhenServiceUnreachable(t *testing.T) {
	t.Parallel()

	// 先占一个端口再放掉，得到一个「一定连不上」的地址。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	configPath := writeServiceConfig(t, t.TempDir(), "http://"+address)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := Run([]string{
		"repo", "add", "https://github.com/example/example.git",
		"--service-config", configPath,
	}, stdout, stderr)

	if code != 0 {
		t.Fatalf("code = %d, want 0（索引已写入，服务不可达不算失败）", code)
	}
	if !strings.Contains(stderr.String(), "已写入索引，服务将在下一轮同步前自动加载") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "added example") {
		t.Fatalf("stdout = %q", stdout.String())
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
