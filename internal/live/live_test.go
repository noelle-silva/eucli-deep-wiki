package live

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eucli-deep-wiki/internal/config"
	"eucli-deep-wiki/internal/vault"
)

// testEnv 是一份可以直接改盘的运行期状态。
type testEnv struct {
	state      *State
	configPath string
	indexPath  string
	dataDir    string
}

func newTestEnv(t *testing.T, configBody string, indexBody string) *testEnv {
	t.Helper()

	dataDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.json")
	writeTestFile(t, configPath, configBody, dataDir)
	indexPath := filepath.Join(dataDir, config.DefaultRepositoryIndex)
	if indexBody != "" {
		if err := os.WriteFile(indexPath, []byte(indexBody), 0o600); err != nil {
			t.Fatalf("write index: %v", err)
		}
	}

	// 仓库目录建成空壳：查询接口只要求目录存在，这里不关心里面有什么。
	if err := os.MkdirAll(filepath.Join(dataDir, config.DefaultRepositoryRoot, "example"), 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	state, err := Open(Options{ConfigPath: configPath})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return &testEnv{state: state, configPath: configPath, indexPath: indexPath, dataDir: dataDir}
}

// writeTestFile 写配置：$DATA_ROOT 会被替换成数据目录。
func writeTestFile(t *testing.T, path string, body string, dataDir string) {
	t.Helper()

	replaced := strings.ReplaceAll(body, "$DATA_ROOT", filepath.ToSlash(dataDir))
	if err := os.WriteFile(path, []byte(replaced), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

// rewriteIndex 改索引文件，模拟 repo add 或手工编辑。
func (e *testEnv) rewriteIndex(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(e.indexPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
}

// rewriteConfig 改配置文件，模拟改 apiKeys、阈值等取值。
func (e *testEnv) rewriteConfig(t *testing.T, body string) {
	t.Helper()
	writeTestFile(t, e.configPath, body, e.dataDir)
}

const (
	testIndexOne = `{"repositories":[{"name":"example","url":"https://example.com/example.git"}]}`
	testIndexTwo = `{
		"repositories": [
			{"name": "example", "url": "https://example.com/example.git"},
			{"name": "fresh", "url": "https://example.com/fresh.git"}
		]
	}`
)

func testConfigBody() string {
	return `{
		"dataRoot": "$DATA_ROOT",
		"apiKeys": ["0123456789abcdef"],
		"syncInterval": "6h",
		"gcThresholdMB": 2048,
		"sizeLimitMB": 5120
	}`
}

func TestOpenBuildsInitialSnapshot(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testConfigBody(), testIndexOne)
	snapshot := env.state.Snapshot()

	if len(snapshot.Repositories) != 1 || snapshot.Repositories[0].Name != "example" {
		t.Fatalf("Repositories = %#v", snapshot.Repositories)
	}
	if snapshot.ConfigLoadedAt.IsZero() || snapshot.IndexLoadedAt.IsZero() {
		t.Fatal("首次加载必须记下加载时间")
	}
	if len(snapshot.NeedsRestart) != 0 {
		t.Fatalf("启动时 NeedsRestart = %#v, want 空", snapshot.NeedsRestart)
	}
	if !snapshot.LastReload.ConfigReloaded || !snapshot.LastReload.IndexReloaded {
		t.Fatalf("LastReload = %#v", snapshot.LastReload)
	}
}

func TestEnsureFreshPicksUpAddedRepository(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testConfigBody(), testIndexOne)
	env.rewriteIndex(t, testIndexTwo)

	env.state.EnsureFresh()
	snapshot := env.state.Snapshot()

	if len(snapshot.Repositories) != 2 {
		t.Fatalf("Repositories = %#v, want 2 条", snapshot.Repositories)
	}
	// 只读视图必须一起更新：同步清单换了、查询仍 404 才是最糟的结果。
	names := snapshot.Vault.Repositories()
	if len(names) != 2 || names[1] != "fresh" {
		t.Fatalf("Vault.Repositories() = %#v", names)
	}
	if _, err := snapshot.Vault.Meta("fresh"); err == nil {
		t.Fatal("尚未同步的仓库应报错，但必须留在清单里")
	}
}

func TestEnsureFreshDropsRemovedRepositoryFromView(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testConfigBody(), testIndexTwo)

	// 仓库目录先准备好，确保 404 是「从索引移除」造成的，而不是「本地没同步」。
	repoDir := filepath.Join(env.state.repositoryDir, "fresh")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	env.rewriteIndex(t, testIndexOne)
	env.state.EnsureFresh()
	snapshot := env.state.Snapshot()

	if _, err := snapshot.Vault.Meta("fresh"); err == nil {
		t.Fatal("移除后的仓库必须查不到")
	} else if err != vault.ErrRepoNotFound {
		t.Fatalf("Meta() error = %v, want ErrRepoNotFound", err)
	}
	// 数据目录保持原样：移除索引不等于删数据。
	if _, err := os.Stat(repoDir); err != nil {
		t.Fatalf("数据目录不应被删除: %v", err)
	}
}

func TestEnsureFreshReloadsAPIKeys(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testConfigBody(), testIndexOne)
	if got := env.state.APIKeys(); len(got) != 1 {
		t.Fatalf("APIKeys() = %#v", got)
	}

	env.rewriteConfig(t, `{
		"dataRoot": "$DATA_ROOT",
		"apiKeys": ["0123456789abcdef", "fedcba9876543210"]
	}`)

	env.state.EnsureFresh()
	keys := env.state.APIKeys()
	if len(keys) != 2 || keys[1] != "fedcba9876543210" {
		t.Fatalf("APIKeys() = %#v", keys)
	}
}

func TestEnsureFreshSkipsUnchangedFiles(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testConfigBody(), testIndexOne)
	before := env.state.Snapshot()

	// 文件没动时，刷新不应重新解析：加载时间保持原值。
	env.state.EnsureFresh()
	env.state.EnsureFresh()
	after := env.state.Snapshot()

	if !after.ConfigLoadedAt.Equal(before.ConfigLoadedAt) || !after.IndexLoadedAt.Equal(before.IndexLoadedAt) {
		t.Fatalf("未变化的文件被重复加载: before=%v after=%v", before.ConfigLoadedAt, after.ConfigLoadedAt)
	}
}

func TestReloadKeepsOldConfigOnInvalidJSON(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testConfigBody(), testIndexOne)
	env.rewriteConfig(t, `{"apiKeys": `)

	result := env.state.Reload()
	if result.ConfigReloaded {
		t.Fatal("非法 JSON 不应算作重载成功")
	}
	if result.Error == "" {
		t.Fatal("重载失败必须给出原因")
	}

	snapshot := env.state.Snapshot()
	// 旧配置继续工作：key 还是原来那把，周期也没变。
	if len(snapshot.Config.APIKeys) != 1 || snapshot.Config.APIKeys[0] != "0123456789abcdef" {
		t.Fatalf("APIKeys = %#v, want 旧配置", snapshot.Config.APIKeys)
	}
	if snapshot.Config.SyncInterval != "6h" {
		t.Fatalf("SyncInterval = %q, want 6h", snapshot.Config.SyncInterval)
	}
	if snapshot.LastReload.Error == "" {
		t.Fatal("/status 里必须能看到最近一次重载失败原因")
	}

	// 修好文件后可以再次加载成功，不需要重启。
	env.rewriteConfig(t, testConfigBody())
	result = env.state.Reload()
	if result.Error != "" || !result.ConfigReloaded {
		t.Fatalf("恢复后 Reload() = %#v", result)
	}
}

func TestReloadKeepsOldIndexOnInvalidJSON(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testConfigBody(), testIndexOne)
	env.rewriteIndex(t, `{"repositories":[{"name":"../outside","url":"https://example.com/x.git"}]}`)

	result := env.state.Reload()
	if result.IndexReloaded {
		t.Fatal("非法索引不应算作重载成功")
	}
	if result.Error == "" {
		t.Fatal("重载失败必须给出原因")
	}

	snapshot := env.state.Snapshot()
	if len(snapshot.Repositories) != 1 || snapshot.Repositories[0].Name != "example" {
		t.Fatalf("Repositories = %#v, want 旧清单", snapshot.Repositories)
	}
	if _, err := snapshot.Vault.Meta("example"); err != nil {
		t.Fatalf("旧只读视图应继续可用: %v", err)
	}
}

func TestReloadAppliesHotFields(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testConfigBody(), testIndexOne)
	env.rewriteConfig(t, `{
		"dataRoot": "$DATA_ROOT",
		"apiKeys": ["0123456789abcdef"],
		"syncInterval": "90s",
		"gcThresholdMB": 12,
		"gcGrowthMB": 3,
		"sizeLimitMB": 34,
		"defaultHistory": "full",
		"gitCommandTimeout": "3m"
	}`)

	result := env.state.Reload()
	if result.Error != "" {
		t.Fatalf("Reload() error = %q", result.Error)
	}

	status := env.state.Snapshot().Status()
	if status.SyncInterval != "90s" {
		t.Fatalf("SyncInterval = %q", status.SyncInterval)
	}
	if status.GCThresholdMB != 12 || status.GCGrowthMB != 3 || status.SizeLimitMB != 34 {
		t.Fatalf("阈值未生效: %#v", status)
	}
	if status.DefaultHistory != config.HistoryFull {
		t.Fatalf("DefaultHistory = %q", status.DefaultHistory)
	}

	cfg := env.state.Snapshot().Config
	if cfg.SyncIntervalDuration != 90*time.Second {
		t.Fatalf("SyncIntervalDuration = %v", cfg.SyncIntervalDuration)
	}
	if cfg.GitCommandTimeoutDuration != 3*time.Minute {
		t.Fatalf("GitCommandTimeoutDuration = %v", cfg.GitCommandTimeoutDuration)
	}
}

func TestNeedsRestartTracksStartupBoundFields(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testConfigBody(), testIndexOne)
	if got := env.state.Snapshot().NeedsRestart; len(got) != 0 {
		t.Fatalf("启动时 NeedsRestart = %#v", got)
	}

	// 监听地址改了：热加载拿不到它，必须在状态里点名要求重启。
	env.rewriteConfig(t, `{
		"dataRoot": "$DATA_ROOT",
		"httpAddress": "127.0.0.1:9998",
		"apiKeys": ["0123456789abcdef"]
	}`)

	result := env.state.Reload()
	if len(result.NeedsRestart) != 1 || result.NeedsRestart[0] != fieldHTTPAddress {
		t.Fatalf("NeedsRestart = %#v", result.NeedsRestart)
	}
	if got := env.state.Snapshot().Status().NeedsRestart; len(got) != 1 {
		t.Fatalf("Status().NeedsRestart = %#v", got)
	}

	// 改回去，提示随之消失。
	env.rewriteConfig(t, testConfigBody())
	if got := env.state.Reload().NeedsRestart; len(got) != 0 {
		t.Fatalf("改回后 NeedsRestart = %#v, want 空", got)
	}
}

func TestNeedsRestartCoversDataPaths(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testConfigBody(), testIndexOne)
	env.rewriteConfig(t, `{
		"dataRoot": "$DATA_ROOT",
		"repositoryRoot": "other",
		"repositoryIndex": "other.json",
		"apiKeys": ["0123456789abcdef"]
	}`)

	result := env.state.Reload()
	if len(result.NeedsRestart) != 2 {
		t.Fatalf("NeedsRestart = %#v, want repositoryRoot 与 repositoryIndex", result.NeedsRestart)
	}
	if result.NeedsRestart[0] != fieldRepositoryRoot || result.NeedsRestart[1] != fieldRepositoryIndex {
		t.Fatalf("NeedsRestart = %#v", result.NeedsRestart)
	}
	// 实际读写位置不变：索引仍从启动时那个文件读。
	if len(env.state.Snapshot().Repositories) != 1 {
		t.Fatalf("Repositories = %#v", env.state.Snapshot().Repositories)
	}
}

func TestChangedFiresOnlyOnEffectiveChange(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testConfigBody(), testIndexOne)
	changes := env.state.Changed()

	// 内容没变的显式重载不该打断等待中的同步循环。
	env.state.Reload()
	select {
	case <-changes:
		t.Fatal("内容没变却收到了变更通知")
	default:
	}

	env.rewriteIndex(t, testIndexTwo)
	env.state.Reload()
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("仓库清单变化后必须通知订阅者")
	}
}

func TestReloadUsesHTTPAddressOverride(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.json")
	writeTestFile(t, configPath, testConfigBody(), dataDir)

	state, err := Open(Options{ConfigPath: configPath, HTTPAddress: "0.0.0.0:9999"})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if got := state.Snapshot().Config.HTTPAddress; got != "0.0.0.0:9999" {
		t.Fatalf("HTTPAddress = %q", got)
	}
	// 命令行覆盖同样作用于每一次重载，因此不会被判成「改了要重启」。
	if got := state.Reload().NeedsRestart; len(got) != 0 {
		t.Fatalf("NeedsRestart = %#v", got)
	}
}

func TestStatusReflectsReloadCount(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testConfigBody(), testIndexOne)
	env.rewriteIndex(t, testIndexTwo)

	result := env.state.Reload()
	if result.Repositories != 2 {
		t.Fatalf("Repositories = %d, want 2", result.Repositories)
	}
	if got := env.state.Snapshot().Status().RepositoryCount; got != 2 {
		t.Fatalf("RepositoryCount = %d, want 2", got)
	}
}

func TestReloadResultMatchesRecordedSnapshot(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testConfigBody(), testIndexOne)
	env.rewriteIndex(t, testIndexTwo)

	result := env.state.Reload()
	recorded := env.state.Snapshot().LastReload

	// 接口返回值、/status 里的 lastReload 与日志必须读同一条结果。
	if result.Repositories != 2 || recorded.Repositories != 2 {
		t.Fatalf("Repositories = %d / %d, want 2", result.Repositories, recorded.Repositories)
	}
	if !result.ConfigReloaded || !result.IndexReloaded {
		t.Fatalf("result = %#v, want 两者都已生效", result)
	}
	if !result.At.Equal(recorded.At) {
		t.Fatalf("At = %v / %v", result.At, recorded.At)
	}
	if len(result.NeedsRestart) != len(recorded.NeedsRestart) {
		t.Fatalf("NeedsRestart = %#v / %#v", result.NeedsRestart, recorded.NeedsRestart)
	}
}
