package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"eucli-deep-wiki-server/internal/config"
	"eucli-deep-wiki-server/internal/live"
	"eucli-deep-wiki-server/internal/repoindex"
	"eucli-deep-wiki-server/internal/state"
	"eucli-deep-wiki-server/internal/vault"
)

// testFixture 是一份可用的运行期环境：数据目录、名单文件、仓库目录与状态文件。
type testFixture struct {
	dataDir       string
	repositoryDir string
	indexPath     string
	source        *stubSource
	states        *state.Store
}

// newFixture 准备一份只有指定仓库的环境。
//
// repositories 里被标为「已落盘」的那些由调用方通过 markOnDisk 创建目录；
// 默认一个都不落盘，这样「名单里有、磁盘上还没有」这条事实可以被单独断言。
func newFixture(t *testing.T, repositories []repoindex.Repository, defaultHistory string) *testFixture {
	t.Helper()

	dataDir := t.TempDir()
	repositoryDir := filepath.Join(dataDir, "repository")
	indexPath := filepath.Join(dataDir, "repositories.json")

	names := make([]string, len(repositories))
	for index, repository := range repositories {
		names[index] = repository.Name
	}
	view, err := vault.New(repositoryDir, names, vault.DefaultLimits())
	if err != nil {
		t.Fatalf("vault.New() error = %v", err)
	}

	index := repoindex.Index{Repositories: append([]repoindex.Repository{}, repositories...)}
	if err := index.Save(indexPath); err != nil {
		t.Fatalf("repoindex.Save() error = %v", err)
	}

	cfg := &config.Config{DataRoot: dataDir, DefaultHistory: defaultHistory}
	source := &stubSource{snapshot: live.Snapshot{
		Config:        cfg,
		Repositories:  repositories,
		Vault:         view,
		RepositoryDir: repositoryDir,
		IndexPath:     indexPath,
	}}

	guard, err := state.Open(state.Path(dataDir))
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}

	return &testFixture{
		dataDir:       dataDir,
		repositoryDir: repositoryDir,
		indexPath:     indexPath,
		source:        source,
		states:        guard,
	}
}

// markOnDisk 在仓库目录下创建指定仓库的副本目录。
func (f *testFixture) markOnDisk(t *testing.T, names ...string) {
	t.Helper()

	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(f.repositoryDir, name), 0o755); err != nil {
			t.Fatalf("create repository dir %s: %v", name, err)
		}
	}
}

// manager 用指定依赖组装运维能力。
//
// exclusive 为 nil 时按「闸门空闲」处理：直接执行动作。
func (f *testFixture) manager(git Git, exclusive func(action func()) bool) *Manager {
	if exclusive == nil {
		exclusive = func(action func()) bool {
			action()
			return true
		}
	}
	return New(Options{Source: f.source, States: f.states, Git: git, Exclusive: exclusive})
}

// storedIndex 读回磁盘上的名单，用于断言「写没写、写了什么」。
func (f *testFixture) storedIndex(t *testing.T) *repoindex.Index {
	t.Helper()

	index, err := repoindex.Load(f.indexPath)
	if err != nil {
		t.Fatalf("repoindex.Load() error = %v", err)
	}
	return index
}

// stubSource 是一份可控的运行期视图。
type stubSource struct {
	mu       sync.Mutex
	snapshot live.Snapshot
	reload   live.Result
	reloads  int
}

func (s *stubSource) Snapshot() live.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot
}

func (s *stubSource) Reload() live.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloads++
	return s.reload
}

func (s *stubSource) reloadCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reloads
}

// stubGit 是一份可控的版本管理能力，测试不碰真实远端与真实仓库。
type stubGit struct {
	shallow      bool
	shallowAfter bool
	shallowErr   error
	compactErr   error
	probeErr     map[string]error
	size         int64
	// sizeAfter 是转换之后测得的体积；0 表示与转换前相同。
	sizeAfter int64
	sizeErr   error

	compacted []string
	probed    []string
}

func (g *stubGit) ProbeRemote(_ context.Context, url string) error {
	g.probed = append(g.probed, url)
	if g.probeErr == nil {
		return nil
	}
	return g.probeErr[url]
}

func (g *stubGit) IsShallow(_ context.Context, _ string) (bool, error) {
	if g.shallowErr != nil {
		return false, g.shallowErr
	}
	return g.shallow, nil
}

func (g *stubGit) Compact(_ context.Context, path string) error {
	if g.compactErr != nil {
		return g.compactErr
	}
	g.compacted = append(g.compacted, path)
	g.shallow = g.shallowAfter
	return nil
}

func (g *stubGit) GitDirectorySize(_ string) (int64, error) {
	if g.sizeErr != nil {
		return 0, g.sizeErr
	}
	if len(g.compacted) > 0 {
		return g.sizeAfter, nil
	}
	return g.size, nil
}

// errProbeFailed 是探测失败的假原因。
var errProbeFailed = errors.New("repository not found")
