package serviceprofile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"eucli-deep-wiki-server/internal/config"
)

const testSeedKey = "0123456789abcdef"

// writeProfile 直接落一份画像原文，模拟平台写好或人工编辑后的文件。
func writeProfile(t *testing.T, dataDir string, body string) string {
	t.Helper()

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("创建数据目录: %v", err)
	}
	path := Path(dataDir)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写入画像: %v", err)
	}
	return path
}

// readProfileText 读出画像原文，用于确认文件有没有被动过。
func readProfileText(t *testing.T, path string) string {
	t.Helper()

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取画像: %v", err)
	}
	return string(payload)
}

func TestPathKeepsProfileInDataRoot(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "data")
	if got, want := Path(dataDir), filepath.Join(dataDir, FileName); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

// 首次启动：按配置文件的初值生成画像并落盘，结构必须与平台约定一致。
func TestEnsureGeneratesProfileFromSeed(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "data")
	profile, err := Ensure(dataDir, Seed{Port: 9998, Key: testSeedKey})
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if profile.Port != 9998 || profile.Key != testSeedKey {
		t.Fatalf("Profile = %#v, want 初值", profile)
	}

	path := Path(dataDir)
	payload := readProfileText(t, path)
	var decoded struct {
		Port *int    `json:"port"`
		Key  *string `json:"key"`
	}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("画像不是合法 JSON: %v\n%s", err, payload)
	}
	if decoded.Port == nil || *decoded.Port != 9998 {
		t.Fatalf("画像 port = %v, want 9998", decoded.Port)
	}
	if decoded.Key == nil || *decoded.Key != testSeedKey {
		t.Fatalf("画像 key = %v, want 初值", decoded.Key)
	}
}

// 初值里没有可用的端口时退到默认端口：0 与越界值都算「不可用」。
func TestEnsureUsesDefaultPortWhenSeedHasNoUsablePort(t *testing.T) {
	t.Parallel()

	for _, seedPort := range []int{0, -1, 70000} {
		seedPort := seedPort
		t.Run("seedPort="+strconv.Itoa(seedPort), func(t *testing.T) {
			t.Parallel()

			dataDir := t.TempDir()
			profile, err := Ensure(dataDir, Seed{Port: seedPort, Key: testSeedKey})
			if err != nil {
				t.Fatalf("Ensure() error = %v", err)
			}
			if profile.Port != config.DefaultPort {
				t.Fatalf("Port = %d, want %d", profile.Port, config.DefaultPort)
			}
			if profile.Key != testSeedKey {
				t.Fatalf("口令不该被换掉: %#v", profile)
			}
		})
	}
}

// 初值里没有可用的口令时随机生成一把：空串、太短、带空白都算「不可用」。
func TestEnsureGeneratesRandomKeyWhenSeedHasNoUsableKey(t *testing.T) {
	t.Parallel()

	for _, seedKey := range []string{"", "short", " with-space-aaaaaaaa", "\t0123456789abcdef"} {
		seedKey := seedKey
		t.Run("seedKey="+strings.ReplaceAll(seedKey, "\t", "tab"), func(t *testing.T) {
			t.Parallel()

			dataDir := t.TempDir()
			profile, err := Ensure(dataDir, Seed{Port: 9998, Key: seedKey})
			if err != nil {
				t.Fatalf("Ensure() error = %v", err)
			}
			if profile.Key == seedKey {
				t.Fatalf("口令没有被换掉: %q", profile.Key)
			}
			if err := config.ValidateAPIKey(profile.Key); err != nil {
				t.Fatalf("生成的口令自己就不可用: %v", err)
			}
			if profile.Port != 9998 {
				t.Fatalf("Port = %d, want 9998", profile.Port)
			}

			// 落盘的就是生效的：平台读到的口令必须能通过鉴权。
			loaded, err := Load(dataDir)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if loaded != profile {
				t.Fatalf("落盘画像 = %#v, want %#v", loaded, profile)
			}
		})
	}
}

// 画像已存在：它就是权威，初值再不同也不改写它。
func TestEnsureKeepsExistingProfile(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	const existing = "{\n  \"port\": 12001,\n  \"key\": \"platform-issued-key-0001\"\n}\n"
	path := writeProfile(t, dataDir, existing)

	profile, err := Ensure(dataDir, Seed{Port: 9997, Key: testSeedKey})
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if profile.Port != 12001 || profile.Key != "platform-issued-key-0001" {
		t.Fatalf("Profile = %#v, want 文件里的那份", profile)
	}
	if got := readProfileText(t, path); got != existing {
		t.Fatalf("既有画像被改写了:\n%s", got)
	}
}

// 平台编辑会保留文档里的其它字段：多出来的字段不该让画像读不出来。
func TestLoadToleratesExtraDocumentFields(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	writeProfile(t, dataDir, `{"port": 12002, "key": "platform-issued-key-0002", "note": "平台保留字段"}`)

	profile, err := Load(dataDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if profile.Port != 12002 || profile.Key != "platform-issued-key-0002" {
		t.Fatalf("Profile = %#v", profile)
	}
}

// 画像不合法：快速失败、说清位置与规矩，且一个字节都不改。
func TestEnsureFailsOnInvalidProfile(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "非 JSON", body: `{"port": 9997,`, want: "不是合法的 JSON"},
		{name: "不是对象", body: `[9997, "key"]`, want: "不是合法的 JSON"},
		{name: "缺 port", body: `{"key": "0123456789abcdef"}`, want: "缺少 port"},
		{name: "port 不是整数", body: `{"port": "9997", "key": "0123456789abcdef"}`, want: "不是合法的 JSON"},
		{name: "port 越界", body: `{"port": 70000, "key": "0123456789abcdef"}`, want: "port 无效"},
		{name: "port 为零", body: `{"port": 0, "key": "0123456789abcdef"}`, want: "port 无效"},
		{name: "缺 key", body: `{"port": 9997}`, want: "缺少 key"},
		{name: "key 太短", body: `{"port": 9997, "key": "short"}`, want: "key 无效"},
		{name: "key 带空白", body: `{"port": 9997, "key": "0123456789abcdef "}`, want: "key 无效"},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dataDir := t.TempDir()
			path := writeProfile(t, dataDir, testCase.body)

			if _, err := Ensure(dataDir, Seed{Port: 9997, Key: testSeedKey}); err == nil {
				t.Fatal("Ensure() error = nil, want 不合法画像被拒绝")
			} else {
				message := err.Error()
				if !strings.Contains(message, path) {
					t.Fatalf("错误里没说是哪个文件: %s", message)
				}
				if !strings.Contains(message, testCase.want) {
					t.Fatalf("错误原因 = %q, want 含 %q", message, testCase.want)
				}
			}

			if got := readProfileText(t, path); got != testCase.body {
				t.Fatalf("不合法的画像被改写了:\n%s", got)
			}
		})
	}
}

// 口令不得进日志，也不得进错误：换掉的、拒绝的都不回显原文。
func TestErrorsDoNotEchoKey(t *testing.T) {
	t.Parallel()

	const secret = "short-secret"
	dataDir := t.TempDir()
	writeProfile(t, dataDir, `{"port": 9997, "key": "`+secret+`"}`)

	_, err := Ensure(dataDir, Seed{})
	if err == nil {
		t.Fatal("Ensure() error = nil, want 口令太短被拒绝")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("错误里回显了口令: %s", err.Error())
	}
}

// 文件缺失要能被上层认出来（os.ErrNotExist 保留在错误链里），否则「首次生成」无从判断。
func TestLoadReportsMissingFile(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	_, err := Load(dataDir)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load() error = %v, want os.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), Path(dataDir)) {
		t.Fatalf("错误里没说是哪个文件: %s", err.Error())
	}
}

// 数据目录还不存在（干净的应用目录）时也要能落盘。
func TestEnsureCreatesMissingDataDir(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "fresh", "data")
	if _, err := Ensure(dataDir, Seed{Port: 9997, Key: testSeedKey}); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if _, err := os.Stat(Path(dataDir)); err != nil {
		t.Fatalf("画像没有落盘: %v", err)
	}
}

func TestLoadAcceptsUTF8BOM(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	body := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"port": 12003, "key": "`+testSeedKey+`"}`)...)
	path := Path(dataDir)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("创建数据目录: %v", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("写入画像: %v", err)
	}

	profile, err := Load(dataDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if profile.Port != 12003 {
		t.Fatalf("Profile = %#v", profile)
	}
}
