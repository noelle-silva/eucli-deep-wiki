package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"eucli-deep-wiki-server/internal/cache"
	"eucli-deep-wiki-server/internal/live"
	"eucli-deep-wiki-server/internal/ops"
	"eucli-deep-wiki-server/internal/repoindex"
	"eucli-deep-wiki-server/internal/state"
	"eucli-deep-wiki-server/internal/status"
	"eucli-deep-wiki-server/internal/vault"
)

// env 是一套完整的管理接口环境：生效视图、同步状态、缓存目录、运维能力与路由。
//
// 它与生产装配同构——状态与缓存都是真实实现，只有版本管理能力是替身，
// 这样「接口层的说法」与「运维层的做法」在同一条路上被一起验证。
type env struct {
	router    http.Handler
	source    *stubLive
	states    *state.Store
	statePath string
	cacheDir  string
	git       *stubGit
}

// envOptions 是测试要控制的那几个依赖；为零值的一项取默认实现。
type envOptions struct {
	// Live 指定生效视图，默认只含仓库 example。
	Live *stubLive
	// Git 指定版本管理替身，默认「完整且可瘦身」。
	Git *stubGit
	// Exclusive 指定同步闸门；为空表示闸门空闲。
	Exclusive func(action func()) bool
	// Cache 指定产物缓存能力；为空时用真实实现指向一个临时目录。
	Cache ArtifactStore
	// Sync、Remote、States 为空时不注册对应接口。
	Sync   SyncTrigger
	Remote RemoteChecker
	States StateSource
}

// idleGate 是一道永远空闲的同步闸门。
func idleGate(action func()) bool {
	action()
	return true
}

// newEnv 按给定选项组装一套环境。
func newEnv(t *testing.T, options envOptions) *env {
	t.Helper()

	source := options.Live
	if source == nil {
		source = newTestLive(t, []string{testAPIKey})
	}
	git := options.Git
	if git == nil {
		git = &stubGit{}
	}
	exclusive := options.Exclusive
	if exclusive == nil {
		exclusive = idleGate
	}

	statePath := state.Path(t.TempDir())
	guard, err := state.Open(statePath)
	if err != nil {
		t.Fatalf("state.Open() error = %v", err)
	}
	cacheDir := t.TempDir()
	artifacts := options.Cache
	if artifacts == nil {
		artifacts = cache.Open(cacheDir)
	}

	// 状态源与运维能力默认用的是同一份状态，与生产装配一致：
	// 解封写进去的东西，名单总览与 /v1/state 立刻就能看到。
	states := options.States
	if states == nil {
		states = guard
	}

	manager := ops.New(ops.Options{
		Source:    source,
		States:    guard,
		Git:       git,
		Exclusive: exclusive,
	})

	router := NewRouter(Options{
		Store:  status.NewStore(),
		Live:   source,
		Sync:   options.Sync,
		Remote: options.Remote,
		States: states,
		Cache:  artifacts,
		Ops:    manager,
	})
	return &env{
		router:    router,
		source:    source,
		states:    guard,
		statePath: statePath,
		cacheDir:  cacheDir,
		git:       git,
	}
}

// newManagementEnv 用默认生效视图（只含仓库 example）组装环境。
func newManagementEnv(t *testing.T) *env {
	t.Helper()
	return newEnv(t, envOptions{})
}

// do 发一次带钥匙的请求，body 非空时作为 JSON 正文。
func (e *env) do(t *testing.T, method string, target string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	return e.send(t, method, target, reader)
}

// doRaw 发一次带钥匙的请求，正文原样送出（用于不合法的正文）。
func (e *env) doRaw(t *testing.T, method string, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	return e.send(t, method, target, bytes.NewBufferString(body))
}

func (e *env) send(t *testing.T, method string, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(method, target, body)
	request.Header.Set("Authorization", "Bearer "+testAPIKey)
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	e.router.ServeHTTP(recorder, request)
	return recorder
}

// storedIndex 读回磁盘上的名单，用于断言「写没写、写了什么」。
func (e *env) storedIndex(t *testing.T) []string {
	t.Helper()

	raw, err := os.ReadFile(e.source.snapshot.IndexPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var payload struct {
		Repositories []struct {
			Name string `json:"name"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	names := make([]string, len(payload.Repositories))
	for index, repository := range payload.Repositories {
		names[index] = repository.Name
	}
	return names
}

// writeCacheFile 在缓存目录里放一个文件，用于清单与清理的用例。
func (e *env) writeCacheFile(t *testing.T, name string, size int) {
	t.Helper()

	if err := os.MkdirAll(e.cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(e.cacheDir, name), make([]byte, size), 0o600); err != nil {
		t.Fatalf("write cache file: %v", err)
	}
}

// stubGit 是版本管理能力的替身：测试不碰真实远端与真实仓库。
type stubGit struct {
	shallow      bool
	shallowAfter bool
	isShallowErr error
	compactErr   error
	size         int64
	sizeAfter    int64
	sizeErr      error

	compacted []string
}

func (g *stubGit) ProbeRemote(_ context.Context, _ string) error {
	return nil
}

func (g *stubGit) IsShallow(_ context.Context, _ string) (bool, error) {
	if g.isShallowErr != nil {
		return false, g.isShallowErr
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

// decodeJSON 把响应体解成给定结构。
func decodeJSON[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()

	var payload T
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	return payload
}

// requireFailure 断言响应是一次失败，且状态码与错误标识都与契约一致。
func requireFailure(t *testing.T, recorder *httptest.ResponseRecorder, statusCode int, code string) {
	t.Helper()

	if recorder.Code != statusCode {
		t.Fatalf("status = %d, want %d (body = %s)", recorder.Code, statusCode, recorder.Body.String())
	}
	payload := decodeJSON[apiError](t, recorder)
	if payload.Code != code {
		t.Fatalf("code = %q, want %q (body = %s)", payload.Code, code, recorder.Body.String())
	}
	if payload.Message == "" {
		t.Fatalf("失败响应缺少说明: %s", recorder.Body.String())
	}
}

// testRevision 是替身仓库的提交号。
const testRevision = "0123456789abcdef0123456789abcdef01234567"

// newLive 准备一份生效视图：repositories 是名单，onDisk 是同时建出目录与 HEAD 的仓库名。
//
// 名单文件、仓库目录、只读视图三者互相对齐，与一次真实的运行期快照同构。
func newLive(t *testing.T, keys []string, repositories []repoindex.Repository, onDisk ...string) *stubLive {
	t.Helper()

	root := t.TempDir()
	names := make([]string, len(repositories))
	for index, repository := range repositories {
		names[index] = repository.Name
	}
	repoVault, err := vault.New(root, names, vault.DefaultLimits())
	if err != nil {
		t.Fatalf("vault.New() error = %v", err)
	}

	for _, name := range onDisk {
		repositoryDir := filepath.Join(root, name)
		writeFile(t, repositoryDir, ".git/HEAD", "ref: refs/heads/main\n")
		writeFile(t, repositoryDir, ".git/refs/heads/main", testRevision+"\n")
		writeFile(t, repositoryDir, "docs/guide.md", "line one\nline two\n")
	}

	// 名单文件也是真实存在的一份：管理接口会读写它，替身没法代替磁盘。
	indexPath := filepath.Join(t.TempDir(), "repositories.json")
	index := repoindex.Index{Repositories: append([]repoindex.Repository{}, repositories...)}
	if err := index.Save(indexPath); err != nil {
		t.Fatalf("repoindex.Save() error = %v", err)
	}

	return &stubLive{snapshot: live.Snapshot{
		Config:         newTestConfig(t, keys),
		Repositories:   repositories,
		Vault:          repoVault,
		RepositoryDir:  root,
		IndexPath:      indexPath,
		ConfigLoadedAt: time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC),
		IndexLoadedAt:  time.Date(2025, 3, 4, 5, 6, 8, 0, time.UTC),
	}}
}

// newTestLive 准备一份只含仓库 example 的生效视图。
func newTestLive(t *testing.T, keys []string) *stubLive {
	t.Helper()

	return newLive(t, keys, []repoindex.Repository{
		{Name: "example", URL: "https://example.com/example.git"},
	}, "example")
}

// stubArtifacts 是产物缓存能力的替身。
//
// 只在真实文件系统上难以稳定制造的失败里用它：缓存正忙、读目录失败、
// 删除失败都取决于并发与宿主环境的脾气，用替身才能把结论钉住。
type stubArtifacts struct {
	listErr  error
	cleanErr error
}

func (s *stubArtifacts) Ensure(_ context.Context, _ string, _ string, _ string, _ time.Duration) (string, error) {
	return "", s.listErr
}

func (s *stubArtifacts) List(_ cache.ListOptions) (cache.ListResult, error) {
	return cache.ListResult{Files: []cache.Entry{}}, s.listErr
}

func (s *stubArtifacts) Clean(_ cache.CleanOptions) (cache.CleanResult, error) {
	return cache.CleanResult{Skipped: []cache.Skipped{}}, s.cleanErr
}
