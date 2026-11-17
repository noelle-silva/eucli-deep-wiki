package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
