package service

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eucli-deep-wiki-server/internal/gitops"
	"eucli-deep-wiki-server/internal/live"
	"eucli-deep-wiki-server/internal/release"
	"eucli-deep-wiki-server/internal/runner"
	"eucli-deep-wiki-server/internal/serviceprofile"
	"eucli-deep-wiki-server/internal/state"
	"eucli-deep-wiki-server/internal/status"
)

func TestParseDefaultsToConfigsPath(t *testing.T) {
	t.Parallel()

	settings, code, finished := parse(nil, &bytes.Buffer{})
	if finished {
		t.Fatalf("parse() 不该提前结束，code = %d", code)
	}
	if settings.configPath != DefaultConfigPath {
		t.Fatalf("configPath = %q", settings.configPath)
	}
	if settings.httpAddress != "" {
		t.Fatalf("httpAddress = %q", settings.httpAddress)
	}
	if settings.once {
		t.Fatal("默认不是只同步一轮")
	}
	if settings.version {
		t.Fatal("默认不打印版本")
	}
}

func TestParseAcceptsOverrides(t *testing.T) {
	t.Parallel()

	settings, _, finished := parse([]string{
		"--config", "/etc/eucli/config.json",
		"--http-address", "0.0.0.0:9997",
		"--once",
	}, &bytes.Buffer{})
	if finished {
		t.Fatal("parse() 不该提前结束")
	}
	if settings.configPath != "/etc/eucli/config.json" {
		t.Fatalf("configPath = %q", settings.configPath)
	}
	if settings.httpAddress != "0.0.0.0:9997" {
		t.Fatalf("httpAddress = %q", settings.httpAddress)
	}
	if !settings.once {
		t.Fatal("once 应为 true")
	}
}

func TestParseRejectsPositionalArguments(t *testing.T) {
	t.Parallel()

	stderr := &bytes.Buffer{}
	_, code, finished := parse([]string{"serve"}, stderr)

	if !finished || code != 2 {
		t.Fatalf("code = %d, finished = %t", code, finished)
	}
	if !strings.Contains(stderr.String(), "位置参数") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestParseHelpIsNotFailure(t *testing.T) {
	t.Parallel()

	stderr := &bytes.Buffer{}
	_, code, finished := parse([]string{"--help"}, stderr)

	if !finished || code != 0 {
		t.Fatalf("code = %d, finished = %t", code, finished)
	}
	if !strings.Contains(stderr.String(), "用法") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestMainFailsWhenConfigIsMissing(t *testing.T) {
	stderr := &bytes.Buffer{}

	code := Main([]string{"--config", filepath.Join(t.TempDir(), "absent.json")}, &bytes.Buffer{}, stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
}

// --once 走的是「起一次同步就退出」这条路：不起接口、不监听端口。
func TestMainSyncsOnceOnEmptyIndex(t *testing.T) {
	configPath := writeConfig(t)

	if code := Main([]string{"--config", configPath, "--once"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
}

// writeConfig 写一份可用的服务端配置：数据目录指到临时目录，名单为空。
func writeConfig(t *testing.T) string {
	t.Helper()

	return writeConfigIn(t, t.TempDir())
}

// writeConfigIn 把服务端配置指到指定的数据目录，用于预置数据目录里的东西。
func writeConfigIn(t *testing.T, dataRoot string) string {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"dataRoot": "` + filepath.ToSlash(dataRoot) + `",
		"httpAddress": "127.0.0.1:59999",
		"apiKeys": ["0123456789abcdef"]
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
}

// -version 打印的就是版本文件里的版本号：代码里没有第二份版本号。
func TestMainPrintsVersionFromReleaseFile(t *testing.T) {
	stdout := &bytes.Buffer{}

	if code := Main([]string{"--version"}, stdout, &bytes.Buffer{}); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}

	info, err := release.Load()
	if err != nil {
		t.Fatalf("release.Load() error = %v", err)
	}
	if got, want := strings.TrimSpace(stdout.String()), ProgramName+" "+info.Version; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

// 就绪行是对外契约：平台按 is ready 判定可用，文字不能随意改。
func TestReadyNoticeKeepsManifestMatchWord(t *testing.T) {
	t.Parallel()

	got := readyNotice("127.0.0.1:9997")
	want := "eucli-deep-wiki-server is ready — listening on http://127.0.0.1:9997"
	if got != want {
		t.Fatalf("readyNotice() = %q, want %q", got, want)
	}
	if !strings.Contains(got, "is ready") {
		t.Fatalf("就绪行里没有清单的匹配词: %q", got)
	}
}

// 画像不合法时启动必须失败：不带着猜测起床，也不去占端口。
func TestMainFailsOnInvalidProfile(t *testing.T) {
	dataRoot := t.TempDir()
	profilePath := serviceprofile.Path(dataRoot)
	if err := os.WriteFile(profilePath, []byte(`{"port": 59999, "key": "weak-key"}`), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	stderr := &bytes.Buffer{}
	// 就绪行只从 log 出：把它接下来，确认既没有「已就绪」也没有假装在听。
	logOutput := captureLog(t)

	code := Main([]string{"--config", writeConfigIn(t, dataRoot)}, &bytes.Buffer{}, stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	logged := logOutput()
	if strings.Contains(logged, "is ready") {
		t.Fatalf("画像不合法却报了就绪: %s", logged)
	}
	if !strings.Contains(logged, profilePath) {
		t.Fatalf("日志里没说是哪个画像文件: %s", logged)
	}
	if strings.Contains(logged, "weak-key") {
		t.Fatalf("日志里回显了口令: %s", logged)
	}
}

// captureLog 接住一段时间的标准日志，返回取内容函数与恢复动作。
func captureLog(t *testing.T) func() string {
	t.Helper()

	buffer := &bytes.Buffer{}
	previous := log.Writer()
	log.SetOutput(buffer)

	t.Cleanup(func() { log.SetOutput(previous) })
	return buffer.String
}

// 装配出来的接口就是服务端真正对外的那些：用真实运行态起一个真实的 HTTP 服务，
// 拿真实请求打进去。漏接某个依赖（例如忘了把管理能力挂上去）会在这里露出来。
func TestAssembleServesManagedEndpoints(t *testing.T) {
	configPath := writeConfig(t)

	source, err := live.Open(live.Options{ConfigPath: configPath})
	if err != nil {
		t.Fatalf("live.Open() error = %v", err)
	}
	snapshot := source.Snapshot()

	guard, err := state.Open(state.Path(snapshot.Config.DataRoot))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	statuses := status.NewStore()
	git := gitops.New(snapshot.RepositoryDir, func() time.Duration {
		return snapshot.Config.GitCommandTimeoutDuration
	})
	syncer := runner.New(source, git, statuses, guard)

	handler := assemble(dependencies{
		source:   source,
		statuses: statuses,
		syncer:   syncer,
		states:   guard,
		git:      git,
		cacheDir: snapshot.Config.CacheDir(),
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	cases := []struct {
		name   string
		method string
		target string
		expect func(t *testing.T, status int, body string)
	}{
		{
			name:   "缓存清单",
			method: http.MethodGet,
			target: "/v1/cache",
			expect: func(t *testing.T, status int, body string) {
				t.Helper()
				if status != http.StatusOK {
					t.Fatalf("status = %d, body = %s", status, body)
				}
				var payload struct {
					Files []json.RawMessage `json:"files"`
				}
				if err := json.Unmarshal([]byte(body), &payload); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if payload.Files == nil {
					t.Fatalf("空清单应是空数组: %s", body)
				}
			},
		},
		{
			name:   "名单总览",
			method: http.MethodGet,
			target: "/v1/repos",
			expect: func(t *testing.T, status int, body string) {
				t.Helper()
				if status != http.StatusOK {
					t.Fatalf("status = %d, body = %s", status, body)
				}
				if !strings.Contains(body, `"repositories":[]`) {
					t.Fatalf("空名单应是空数组: %s", body)
				}
			},
		},
		{
			name:   "加入名单",
			method: http.MethodPost,
			target: "/v1/repos",
			expect: func(t *testing.T, status int, body string) {
				t.Helper()
				if status != http.StatusBadRequest {
					t.Fatalf("status = %d, body = %s", status, body)
				}
				if !strings.Contains(body, "invalid_url") {
					t.Fatalf("body = %s", body)
				}
			},
		},
		{
			name:   "缓存清理未确认",
			method: http.MethodDelete,
			target: "/v1/cache",
			expect: func(t *testing.T, status int, body string) {
				t.Helper()
				if status != http.StatusBadRequest || !strings.Contains(body, "confirm_required") {
					t.Fatalf("status = %d, body = %s", status, body)
				}
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			status, body := request(t, server, testCase.method, testCase.target)
			testCase.expect(t, status, body)
		})
	}

	// 新接口与原有接口用同一套鉴权：没有钥匙一律拒绝。
	response, err := http.Get(server.URL + "/v1/cache")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.StatusCode)
	}
}

// request 用带钥匙的请求打一次真实服务，返回状态码与响应体。
func request(t *testing.T, server *httptest.Server, method string, target string) (int, string) {
	t.Helper()

	bodyRequest, err := http.NewRequest(method, server.URL+target, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	bodyRequest.Header.Set("Authorization", "Bearer 0123456789abcdef")

	response, err := server.Client().Do(bodyRequest)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer response.Body.Close()

	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response.StatusCode, string(content)
}
