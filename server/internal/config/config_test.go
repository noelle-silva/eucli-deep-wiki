package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testKey = "0123456789abcdef"

func TestLoadAppliesDefaultsAndParsesDurations(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"apiKeys": ["`+testKey+`"]}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.DataRoot != DefaultDataRoot {
		t.Fatalf("DataRoot = %q", cfg.DataRoot)
	}
	if cfg.RepositoryRoot != DefaultRepositoryRoot {
		t.Fatalf("RepositoryRoot = %q", cfg.RepositoryRoot)
	}
	if cfg.RepositoryIndex != DefaultRepositoryIndex {
		t.Fatalf("RepositoryIndex = %q", cfg.RepositoryIndex)
	}
	if cfg.SyncIntervalDuration == 0 {
		t.Fatal("SyncIntervalDuration was not parsed")
	}
	if cfg.GitCommandTimeoutDuration == 0 {
		t.Fatal("GitCommandTimeoutDuration was not parsed")
	}
	if cfg.HTTPAddress != DefaultHTTPAddress {
		t.Fatalf("HTTPAddress = %q", cfg.HTTPAddress)
	}
	if len(cfg.APIKeys) != 1 || cfg.APIKeys[0] != testKey {
		t.Fatalf("APIKeys = %#v", cfg.APIKeys)
	}
}

func TestDerivedPathsResolveUnderDataRoot(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"dataRoot": "var", "repositoryRoot": "refs", "repositoryIndex": "index.json", "apiKeys": ["`+testKey+`"]}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.RepositoryDir(), filepath.Join("var", "refs"); got != want {
		t.Fatalf("RepositoryDir() = %q, want %q", got, want)
	}
	if got, want := cfg.RepositoryIndexPath(), filepath.Join("var", "index.json"); got != want {
		t.Fatalf("RepositoryIndexPath() = %q, want %q", got, want)
	}
}

func TestLoadAcceptsAbsoluteDataRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "eucli-data")
	path := writeConfig(t, `{"dataRoot": "`+filepath.ToSlash(root)+`", "apiKeys": ["`+testKey+`"]}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got, want := cfg.RepositoryDir(), filepath.Join(root, DefaultRepositoryRoot); got != want {
		t.Fatalf("RepositoryDir() = %q, want %q", got, want)
	}
	if got, want := cfg.RepositoryIndexPath(), filepath.Join(root, DefaultRepositoryIndex); got != want {
		t.Fatalf("RepositoryIndexPath() = %q, want %q", got, want)
	}
}

func TestLoadRejectsEscapingDataRoot(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"dataRoot": "../outside", "apiKeys": ["`+testKey+`"]}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want unsafe dataRoot error")
	}
}

func TestLoadRejectsNestedRepositoryRoot(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"repositoryRoot": "tmp/repository", "apiKeys": ["`+testKey+`"]}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want nested repositoryRoot error")
	}
}

func TestLoadRejectsNestedRepositoryIndex(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"repositoryIndex": "nested/index.json", "apiKeys": ["`+testKey+`"]}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want nested repositoryIndex error")
	}
}

func TestLoadRejectsInvalidHTTPAddress(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"httpAddress": "127.0.0.1:not-a-port", "apiKeys": ["`+testKey+`"]}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want invalid httpAddress error")
	}
}

func TestLoadRejectsNonPositiveSyncInterval(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"syncInterval": "0s", "apiKeys": ["`+testKey+`"]}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want non-positive syncInterval error")
	}
}

// 整项不写是合法配置：首次启动由服务画像生成一把强口令并落盘。
func TestLoadAllowsMissingAPIKeys(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v, want 允许不写 apiKeys", err)
	}
	if len(cfg.APIKeys) != 0 {
		t.Fatalf("APIKeys = %#v, want 空", cfg.APIKeys)
	}
}

// 空数组与不写等价，同样是「让人自己按默认规矩生成」。
func TestLoadAllowsEmptyAPIKeys(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"apiKeys": []}`)

	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v, want 允许空数组", err)
	}
}

func TestLoadRejectsShortAPIKey(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"apiKeys": ["short"]}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want short apiKey error")
	}
}

func TestLoadRejectsDuplicateAPIKey(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"apiKeys": ["`+testKey+`", "`+testKey+`"]}`)

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want duplicate apiKey error")
	}
}

func TestLoadAcceptsMultipleAPIKeys(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, `{"apiKeys": ["`+testKey+`", "fedcba9876543210"]}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.APIKeys) != 2 {
		t.Fatalf("APIKeys = %#v", cfg.APIKeys)
	}
}

func TestSetHTTPAddressOverridesValidatedAddress(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	if err := cfg.SetHTTPAddress("0.0.0.0:9997"); err != nil {
		t.Fatalf("SetHTTPAddress() error = %v", err)
	}
	if cfg.HTTPAddress != "0.0.0.0:9997" {
		t.Fatalf("HTTPAddress = %q", cfg.HTTPAddress)
	}
}

func TestSetHTTPAddressAcceptsPortOnlyListenAddress(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	if err := cfg.SetHTTPAddress(":9997"); err != nil {
		t.Fatalf("SetHTTPAddress() error = %v", err)
	}
	if cfg.HTTPAddress != ":9997" {
		t.Fatalf("HTTPAddress = %q", cfg.HTTPAddress)
	}
}

func TestSetHTTPAddressRejectsInvalidAddress(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	if err := cfg.SetHTTPAddress("9997"); err == nil {
		t.Fatal("SetHTTPAddress() error = nil, want invalid address error")
	}
}

func writeConfig(t *testing.T, data string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadAcceptsUTF8BOM(t *testing.T) {
	t.Parallel()

	body := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"apiKeys":["`+testKey+`"]}`)...)
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

// 默认端口与默认监听地址里的端口必须是同一个事实：画像生成会用 DefaultPort，
// 而配置默认值用的是 DefaultHTTPAddress。
func TestDefaultPortMatchesDefaultHTTPAddress(t *testing.T) {
	t.Parallel()

	cfg := &Config{HTTPAddress: DefaultHTTPAddress}
	if got := cfg.ListenPort(); got != DefaultPort {
		t.Fatalf("DefaultHTTPAddress 的端口 = %d, want DefaultPort = %d", got, DefaultPort)
	}
}

func TestListenPortReadsAddress(t *testing.T) {
	t.Parallel()

	cases := map[string]int{
		"127.0.0.1:9997": 9997,
		"0.0.0.0:12001":  12001,
		":9998":          9998,
		"":               0,
		"9997":           0,
		"127.0.0.1:x":    0,
		"127.0.0.1:0":    0,
	}
	for address, want := range cases {
		cfg := &Config{HTTPAddress: address}
		if got := cfg.ListenPort(); got != want {
			t.Fatalf("ListenPort(%q) = %d, want %d", address, got, want)
		}
	}
}

func TestFirstAPIKeyIsFirstOrEmpty(t *testing.T) {
	t.Parallel()

	if got := (&Config{}).FirstAPIKey(); got != "" {
		t.Fatalf("FirstAPIKey() = %q, want 空串", got)
	}
	if got := (&Config{APIKeys: []string{testKey, "fedcba9876543210"}}).FirstAPIKey(); got != testKey {
		t.Fatalf("FirstAPIKey() = %q, want %q", got, testKey)
	}
}

// 画像接管之后：端口换画像的、钥匙只剩画像那一把，监听宿主仍然由配置文件说了算。
func TestAdoptProfileTakesAuthorityOverPortAndKeys(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		HTTPAddress: "0.0.0.0:9997",
		APIKeys:     []string{testKey, "fedcba9876543210"},
	}
	if err := cfg.AdoptProfile(12001, "platform-issued-key-0001"); err != nil {
		t.Fatalf("AdoptProfile() error = %v", err)
	}
	if got, want := cfg.HTTPAddress, "0.0.0.0:12001"; got != want {
		t.Fatalf("HTTPAddress = %q, want %q", got, want)
	}
	if got := cfg.APIKeys; len(got) != 1 || got[0] != "platform-issued-key-0001" {
		t.Fatalf("APIKeys = %#v, want 只剩画像那一把", got)
	}
}

func TestAdoptProfileKeepsPortOnlyHost(t *testing.T) {
	t.Parallel()

	cfg := &Config{HTTPAddress: ":9997", APIKeys: []string{testKey}}
	if err := cfg.AdoptProfile(12002, testKey); err != nil {
		t.Fatalf("AdoptProfile() error = %v", err)
	}
	if got, want := cfg.HTTPAddress, ":12002"; got != want {
		t.Fatalf("HTTPAddress = %q, want %q", got, want)
	}
}

// 画像与配置口径一致：不合法就失败，且一个字段都不落值。
func TestAdoptProfileRejectsInvalidFacts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		port int
		key  string
	}{
		{name: "端口为零", port: 0, key: testKey},
		{name: "端口越界", port: 70000, key: testKey},
		{name: "口令为空", port: 12003, key: ""},
		{name: "口令太短", port: 12003, key: "weak-key"},
		{name: "口令带空白", port: 12003, key: testKey + " "},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Config{HTTPAddress: "127.0.0.1:9997", APIKeys: []string{testKey}}
			if err := cfg.AdoptProfile(testCase.port, testCase.key); err == nil {
				t.Fatal("AdoptProfile() error = nil, want 不可用的画像事实被拒绝")
			}
			if cfg.HTTPAddress != "127.0.0.1:9997" || len(cfg.APIKeys) != 1 || cfg.APIKeys[0] != testKey {
				t.Fatalf("失败后配置被改了: %#v", cfg)
			}
		})
	}
}

func TestValidateAPIKeyCoversTheSameRulesAsTheConfigFile(t *testing.T) {
	t.Parallel()

	if err := ValidateAPIKey(testKey); err != nil {
		t.Fatalf("ValidateAPIKey() error = %v", err)
	}
	for _, key := range []string{"", "short", " " + testKey, testKey + " ", "0123456789abcde f"} {
		if err := ValidateAPIKey(key); err == nil {
			t.Fatalf("ValidateAPIKey(%q) error = nil, want 拒绝", key)
		}
	}
}

func TestValidatePortCoversTheSameRange(t *testing.T) {
	t.Parallel()

	for _, port := range []int{1, 9997, 65535} {
		if err := ValidatePort(port); err != nil {
			t.Fatalf("ValidatePort(%d) error = %v", port, err)
		}
	}
	for _, port := range []int{0, -1, 65536} {
		if err := ValidatePort(port); err == nil {
			t.Fatalf("ValidatePort(%d) error = nil, want 拒绝", port)
		}
	}
}
