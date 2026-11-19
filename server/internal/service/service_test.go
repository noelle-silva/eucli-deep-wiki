package service

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eucli-deep-wiki-server/internal/gitops"
	"eucli-deep-wiki-server/internal/live"
	"eucli-deep-wiki-server/internal/runner"
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

	code := Main([]string{"--config", filepath.Join(t.TempDir(), "absent.json")}, stderr)
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
}

// --once 走的是「起一次同步就退出」这条路：不起接口、不监听端口。
func TestMainSyncsOnceOnEmptyIndex(t *testing.T) {
	configPath := writeConfig(t)

	if code := Main([]string{"--config", configPath, "--once"}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
}

// writeConfig 写一份可用的服务端配置：数据目录指到临时目录，名单为空。
func writeConfig(t *testing.T) string {
	t.Helper()

	dataRoot := filepath.ToSlash(t.TempDir())
	configPath := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"dataRoot": "` + dataRoot + `",
		"httpAddress": "127.0.0.1:59999",
		"apiKeys": ["0123456789abcdef"]
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return configPath
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
