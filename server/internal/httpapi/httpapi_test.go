package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"eucli-deep-wiki-server/internal/config"
	"eucli-deep-wiki-server/internal/live"
	"eucli-deep-wiki-server/internal/repoindex"
	"eucli-deep-wiki-server/internal/state"
	"eucli-deep-wiki-server/internal/vault"
)

const testAPIKey = "0123456789abcdef"

func TestListRepositories(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}

	var payload struct {
		Repositories []vault.Meta `json:"repositories"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Repositories) != 1 || payload.Repositories[0].Name != "example" {
		t.Fatalf("repositories = %#v", payload.Repositories)
	}
	if payload.Repositories[0].Revision != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("revision = %q", payload.Repositories[0].Revision)
	}
}

func TestShowFileReturnsContentAndETag(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/file?path=docs/guide.md"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "line one\nline two\n" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
	if recorder.Header().Get("ETag") == "" {
		t.Fatal("ETag 缺失")
	}
	if recorder.Header().Get("X-Repo-Revision") == "" {
		t.Fatal("X-Repo-Revision 缺失")
	}
}

func TestShowFileHonoursIfNoneMatch(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, authorized(http.MethodGet, "/v1/repos/example/file?path=docs/guide.md"))
	etag := first.Header().Get("ETag")

	second := httptest.NewRecorder()
	request := authorized(http.MethodGet, "/v1/repos/example/file?path=docs/guide.md")
	request.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(second, request)

	if second.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", second.Body.String())
	}
}

func TestShowFileRejectsMissingPath(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/file"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestShowFileRejectsTraversal(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/file?path=../secret.txt"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestShowFileReportsUnknownRepository(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/nope/file?path=README.md"))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestSearchRequiresQuery(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/search"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestSearchFindsMatches(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/search?q=two"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var payload vault.SearchResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Matches) != 1 || payload.Matches[0].Path != "docs/guide.md" {
		t.Fatalf("matches = %#v", payload.Matches)
	}
}

func TestTreeListsEntries(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/tree?path=docs"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var payload vault.Tree
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Entries) != 1 || payload.Entries[0].Name != "guide.md" {
		t.Fatalf("entries = %#v", payload.Entries)
	}
}

func TestProtectedEndpointsRequireKey(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	for _, target := range []string{
		"/status",
		"/v1/repos",
		"/v1/repos/example",
		"/v1/repos/example/tree",
		"/v1/repos/example/file?path=docs/guide.md",
		"/v1/repos/example/search?q=two",
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401", target, recorder.Code)
		}
		if recorder.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("%s 缺少 WWW-Authenticate", target)
		}
	}
}

func TestProtectedEndpointsRejectWrongKey(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/repos", nil)
	request.Header.Set("Authorization", "Bearer wrong-key-000000")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestProtectedEndpointsAcceptSecondKey(t *testing.T) {
	t.Parallel()

	handler := newTestRouterWithKeys(t, []string{testAPIKey, "fedcba9876543210"})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/repos", nil)
	request.Header.Set("Authorization", "Bearer fedcba9876543210")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestHealthzStaysPublic(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
}

func TestAuthenticatorRejectsEverythingWhenNoKeyIsConfigured(t *testing.T) {
	t.Parallel()

	// key 集合来自运行期状态；空集合必须全部拒绝，不能因为「没配置」就放行。
	handler := NewAuthenticator(staticKeys(nil)).Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/repos", nil)
	request.Header.Set("Authorization", "Bearer anything-at-all")
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func authorized(method string, target string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("Authorization", "Bearer "+testAPIKey)
	return request
}

func authorizedRequest(method string, target string) *http.Request {
	return authorized(method, target)
}

// staticKeys 是固定 key 集合，供不需要重载的鉴权用例使用。
type staticKeys []string

func (k staticKeys) APIKeys() []string {
	return k
}

// stubLive 是固定的生效视图：给出一份快照，被要求重载时返回预设结果。
type stubLive struct {
	snapshot  live.Snapshot
	reload    live.Result
	refreshed int
}

func (s *stubLive) EnsureFresh() { s.refreshed++ }

func (s *stubLive) Snapshot() live.Snapshot { return s.snapshot }

func (s *stubLive) Reload() live.Result { return s.reload }

func (s *stubLive) APIKeys() []string { return s.snapshot.Config.APIKeys }

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	return newRouterWith(t, newTestLive(t, []string{testAPIKey}), nil, nil)
}

func newTestRouterWithKeys(t *testing.T, keys []string) http.Handler {
	t.Helper()
	return newRouterWith(t, newTestLive(t, keys), nil, nil)
}

func newTestRouterWithSync(t *testing.T, trigger SyncTrigger) http.Handler {
	t.Helper()
	return newRouterWith(t, newTestLive(t, []string{testAPIKey}), trigger, nil)
}

func newTestRouterWithRemote(t *testing.T, checker RemoteChecker) http.Handler {
	t.Helper()
	return newRouterWith(t, newTestLive(t, []string{testAPIKey}), nil, checker)
}

func newTestRouterWithStates(t *testing.T, states StateSource) http.Handler {
	t.Helper()

	return newEnv(t, envOptions{States: states}).router
}

// newRouterWith 用一份固定的生效视图组装路由。
func newRouterWith(t *testing.T, source *stubLive, trigger SyncTrigger, checker RemoteChecker) http.Handler {
	t.Helper()
	return newEnv(t, envOptions{Live: source, Sync: trigger, Remote: checker}).router
}

// newTestConfig 写一份最小可用配置并加载，让 key 与超时都走真实校验。
func newTestConfig(t *testing.T, keys []string) *config.Config {
	t.Helper()

	payload := map[string]any{
		"dataRoot": filepath.ToSlash(t.TempDir()),
		"apiKeys":  keys,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode config: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	return cfg
}

func writeFile(t *testing.T, dir string, name string, content string) {
	t.Helper()

	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("make dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestArchiveReportsMissingPath(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/archive?path=nodir"))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

type stubStates map[string]state.RepoState

func (s stubStates) Snapshot() map[string]state.RepoState {
	return s
}

func TestStateEndpointReportsBlockedRepository(t *testing.T) {
	t.Parallel()

	handler := newTestRouterWithStates(t, stubStates{
		"example": {Blocked: true, BlockedSize: 1 << 30},
	})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/state"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "\"blocked\":true") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestStateEndpointReportsRemoteStatus(t *testing.T) {
	t.Parallel()

	handler := newTestRouterWithStates(t, stubStates{
		"example": {
			ConsecutiveFailures: 3,
			LastError:           "Repository not found",
			RemoteMissing:       true,
			RemoteMissingAt:     time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC),
		},
	})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/state"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}

	var payload struct {
		Repositories map[string]state.RepoState `json:"repositories"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := payload.Repositories["example"]
	if !got.RemoteMissing || got.ConsecutiveFailures != 3 || got.LastError != "Repository not found" {
		t.Fatalf("state = %#v", got)
	}
	if got.RemoteMissingAt.IsZero() {
		t.Fatal("RemoteMissingAt 未透出")
	}
}

func TestFindEndpointReturnsMatches(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/find?name=*.md"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "docs/guide.md") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestFindEndpointRequiresPattern(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/find"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestReloadEndpointRequiresKey(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/reload", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if recorder.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("缺少 WWW-Authenticate")
	}
}

func TestReloadEndpointReturnsResult(t *testing.T) {
	t.Parallel()

	source := newTestLive(t, []string{testAPIKey})
	source.reload = live.Result{
		At:             time.Date(2025, 3, 4, 5, 6, 9, 0, time.UTC),
		ConfigReloaded: true,
		IndexReloaded:  true,
		Repositories:   4,
		NeedsRestart:   []string{"httpAddress"},
	}
	handler := newRouterWith(t, source, nil, nil)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodPost, "/v1/reload"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var payload live.Result
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.ConfigReloaded || !payload.IndexReloaded || payload.Repositories != 4 {
		t.Fatalf("payload = %#v", payload)
	}
	if len(payload.NeedsRestart) != 1 || payload.NeedsRestart[0] != "httpAddress" {
		t.Fatalf("NeedsRestart = %#v", payload.NeedsRestart)
	}
}

func TestReloadEndpointReportsFailure(t *testing.T) {
	t.Parallel()

	source := newTestLive(t, []string{testAPIKey})
	source.reload = live.Result{
		At:            time.Date(2025, 3, 4, 5, 6, 9, 0, time.UTC),
		IndexReloaded: true,
		Repositories:  1,
		NeedsRestart:  []string{},
		Error:         "配置加载失败（config.json）: invalid character",
	}
	handler := newRouterWith(t, source, nil, nil)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodPost, "/v1/reload"))

	// 请求本身被正确处理了，失败原因在结果里，因此仍然是 200。
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "invalid character") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestStatusIncludesRuntimeState(t *testing.T) {
	t.Parallel()

	source := newTestLive(t, []string{testAPIKey})
	source.snapshot.NeedsRestart = []string{"httpAddress", "dataRoot"}
	source.snapshot.LastReload = live.Result{
		At:             time.Date(2025, 3, 4, 5, 6, 9, 0, time.UTC),
		ConfigReloaded: true,
		IndexReloaded:  false,
		Repositories:   1,
		NeedsRestart:   []string{"httpAddress", "dataRoot"},
	}
	handler := newRouterWith(t, source, nil, nil)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/status"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Running         bool           `json:"running"`
		Summary         map[string]int `json:"summary"`
		ConfigLoadedAt  time.Time      `json:"configLoadedAt"`
		IndexLoadedAt   time.Time      `json:"indexLoadedAt"`
		RepositoryCount int            `json:"repositoryCount"`
		SyncInterval    string         `json:"syncInterval"`
		GCThresholdMB   int            `json:"gcThresholdMB"`
		NeedsRestart    []string       `json:"needsRestart"`
		LastReload      live.Result    `json:"lastReload"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !payload.ConfigLoadedAt.Equal(source.snapshot.ConfigLoadedAt) {
		t.Fatalf("configLoadedAt = %v", payload.ConfigLoadedAt)
	}
	if !payload.IndexLoadedAt.Equal(source.snapshot.IndexLoadedAt) {
		t.Fatalf("indexLoadedAt = %v", payload.IndexLoadedAt)
	}
	if payload.RepositoryCount != 1 {
		t.Fatalf("repositoryCount = %d", payload.RepositoryCount)
	}
	if payload.SyncInterval != source.snapshot.Config.SyncInterval {
		t.Fatalf("syncInterval = %q", payload.SyncInterval)
	}
	if payload.GCThresholdMB != source.snapshot.Config.GCThresholdMB {
		t.Fatalf("gcThresholdMB = %d", payload.GCThresholdMB)
	}
	if len(payload.NeedsRestart) != 2 {
		t.Fatalf("needsRestart = %#v", payload.NeedsRestart)
	}
	if payload.LastReload.Error != "" || !payload.LastReload.ConfigReloaded {
		t.Fatalf("lastReload = %#v", payload.LastReload)
	}

	// 原有字段语义不变：仍然有 running 与 summary。
	if payload.Running {
		t.Fatal("running 应为 false")
	}
	if payload.Summary == nil {
		t.Fatal("summary 不应为 null")
	}
}

func TestProtectedEndpointsRefreshRuntimeState(t *testing.T) {
	t.Parallel()

	source := newTestLive(t, []string{testAPIKey})
	handler := newRouterWith(t, source, nil, nil)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos"))

	if source.refreshed == 0 {
		t.Fatal("受保护接口进入前应刷新运行期状态")
	}
}

func TestListRepositoriesKeepsUnsynchronizedRepository(t *testing.T) {
	t.Parallel()

	source := newTestLive(t, []string{testAPIKey})
	// 索引里多了一个还没同步到本地的仓库。
	source.snapshot.Repositories = append(source.snapshot.Repositories,
		repoindex.Repository{Name: "fresh", URL: "https://example.com/fresh.git"})
	source.snapshot.Vault, _ = vault.New(source.snapshot.RepositoryDir, []string{"example", "fresh"}, vault.DefaultLimits())
	handler := newRouterWith(t, source, nil, nil)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Repositories []vault.Meta `json:"repositories"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Repositories) != 2 || payload.Repositories[1].Name != "fresh" {
		t.Fatalf("repositories = %#v", payload.Repositories)
	}
}

// newRouterWithNestedRepository 用一份带两层目录的仓库组装路由。
func newRouterWithNestedRepository(t *testing.T) http.Handler {
	t.Helper()

	source := newTestLive(t, []string{testAPIKey})
	repoDir := filepath.Join(source.snapshot.RepositoryDir, "example")
	writeFile(t, repoDir, "docs/deep/more.md", "deep needle line\n")

	// 新文件要进同一份视图，因此按同一根目录重建 vault。
	rebuilt, err := vault.New(source.snapshot.RepositoryDir, []string{"example"}, vault.DefaultLimits())
	if err != nil {
		t.Fatalf("vault.New() error = %v", err)
	}
	source.snapshot.Vault = rebuilt
	return newRouterWith(t, source, nil, nil)
}

func TestShowFileSlicesLines(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/file?path=docs/guide.md&lines=2-2"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "line two\n" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
	if got := recorder.Header().Get("X-File-Total-Lines"); got != "2" {
		t.Fatalf("X-File-Total-Lines = %q", got)
	}
	if got := recorder.Header().Get("X-File-First-Line"); got != "2" {
		t.Fatalf("X-File-First-Line = %q", got)
	}
	if got := recorder.Header().Get("X-File-Last-Line"); got != "2" {
		t.Fatalf("X-File-Last-Line = %q", got)
	}
}

func TestShowFileClampsBytes(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/file?path=docs/guide.md&maxBytes=12"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "line one\n" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
	if got := recorder.Header().Get("X-File-Truncated"); got != "bytes" {
		t.Fatalf("X-File-Truncated = %q", got)
	}
	if got := recorder.Header().Get("X-File-Last-Line"); got != "1" {
		t.Fatalf("X-File-Last-Line = %q, want 1", got)
	}
}

func TestShowFileGivesNoPartialLine(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/file?path=docs/guide.md&maxBytes=4"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty（第一行就超限时不给半行）", recorder.Body.String())
	}
}

func TestShowFileRejectsInvalidLines(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/file?path=docs/guide.md&lines=abc"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "invalid_lines") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestShowTreeExpandsDepth(t *testing.T) {
	t.Parallel()

	handler := newRouterWithNestedRepository(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/tree?depth=2"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var tree vault.Tree
	if err := json.Unmarshal(recorder.Body.Bytes(), &tree); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tree.Depth != 2 {
		t.Fatalf("depth = %d", tree.Depth)
	}

	got := make([]string, 0, len(tree.Entries))
	for _, entry := range tree.Entries {
		got = append(got, entry.Path)
	}
	want := []string{"docs", "docs/deep", "docs/guide.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %#v, want %#v", got, want)
	}
}

func TestShowTreeFiltersDirectories(t *testing.T) {
	t.Parallel()

	handler := newRouterWithNestedRepository(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/tree?depth=2&dirsOnly=true"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var tree vault.Tree
	if err := json.Unmarshal(recorder.Body.Bytes(), &tree); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, entry := range tree.Entries {
		if entry.Type != "dir" {
			t.Fatalf("非目录混进结果: %#v", entry)
		}
	}
	if len(tree.Entries) != 2 {
		t.Fatalf("entries = %#v", tree.Entries)
	}
}

// newRouterWithDependencyDirectory 用一份带依赖目录的仓库组装路由。
//
// node_modules 是「按名字排除」要跳过的目录，src/node_modules 是同名的普通文件：
// 两者放在不同的层，才能验证「只排除目录，同名文件照常返回」。
func newRouterWithDependencyDirectory(t *testing.T) http.Handler {
	t.Helper()

	source := newTestLive(t, []string{testAPIKey})
	repoDir := filepath.Join(source.snapshot.RepositoryDir, "example")
	writeFile(t, repoDir, "node_modules/left-pad/index.js", "module.exports = {};\n")
	writeFile(t, repoDir, "src/node_modules", "不是一个目录\n")

	// 新文件要进同一份视图，因此按同一根目录重建 vault。
	rebuilt, err := vault.New(source.snapshot.RepositoryDir, []string{"example"}, vault.DefaultLimits())
	if err != nil {
		t.Fatalf("vault.New() error = %v", err)
	}
	source.snapshot.Vault = rebuilt
	return newRouterWith(t, source, nil, nil)
}

// treePathsOf 发一次目录请求，取出条目路径：顺序即服务端的展开顺序。
func treePathsOf(t *testing.T, handler http.Handler, target string) []string {
	t.Helper()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, target))
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s: status = %d, body = %s", target, recorder.Code, recorder.Body.String())
	}
	tree := decodeJSON[vault.Tree](t, recorder)
	paths := make([]string, 0, len(tree.Entries))
	for _, entry := range tree.Entries {
		paths = append(paths, entry.Path)
	}
	return paths
}

// withinExcluded 判断一个条目是不是「被排除的目录本身或它里面的东西」。
func withinExcluded(path string, excluded string) bool {
	return path == excluded || strings.HasPrefix(path, excluded+"/")
}

func TestShowTreeExcludesNamedDirectories(t *testing.T) {
	t.Parallel()

	handler := newRouterWithDependencyDirectory(t)

	// 不传 exclude：依赖目录照常出现在结果里，与改动前一致。
	baseline := treePathsOf(t, handler, "/v1/repos/example/tree?depth=3")
	wantBaseline := []string{
		"docs", "docs/guide.md",
		"node_modules", "node_modules/left-pad", "node_modules/left-pad/index.js",
		"src", "src/node_modules",
	}
	if strings.Join(baseline, ",") != strings.Join(wantBaseline, ",") {
		t.Fatalf("不带 exclude 的 entries = %#v, want %#v", baseline, wantBaseline)
	}

	// 传了 exclude：命中的目录连同内部一起消失，同名普通文件照常返回。
	filtered := treePathsOf(t, handler, "/v1/repos/example/tree?depth=3&exclude=node_modules")
	wantFiltered := []string{"docs", "docs/guide.md", "src", "src/node_modules"}
	if strings.Join(filtered, ",") != strings.Join(wantFiltered, ",") {
		t.Fatalf("带 exclude 的 entries = %#v, want %#v", filtered, wantFiltered)
	}
	for _, path := range filtered {
		if withinExcluded(path, "node_modules") {
			t.Fatalf("被排除的目录仍出现在结果里: %#v", filtered)
		}
	}

	// 空值与多余逗号一律忽略：与不传是同一份结果。
	for _, raw := range []string{"", "%2C%2C"} {
		empty := treePathsOf(t, handler, "/v1/repos/example/tree?depth=3&exclude="+raw)
		if strings.Join(empty, ",") != strings.Join(baseline, ",") {
			t.Fatalf("exclude=%q 的 entries = %#v, want %#v", raw, empty, baseline)
		}
	}
}

func TestShowTreeRejectsInvalidFlag(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/tree?dirsOnly=maybe"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "invalid_flag") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestFindAcceptsSubstringMatch(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	exact := httptest.NewRecorder()
	handler.ServeHTTP(exact, authorized(http.MethodGet, "/v1/repos/example/find?name=guide"))

	var exactPayload vault.FindResult
	if err := json.Unmarshal(exact.Body.Bytes(), &exactPayload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(exactPayload.Entries) != 0 {
		t.Fatalf("精确匹配不该命中 guide.md: %#v", exactPayload.Entries)
	}

	substring := httptest.NewRecorder()
	handler.ServeHTTP(substring, authorized(http.MethodGet, "/v1/repos/example/find?name=guide&match=substring"))

	var substringPayload vault.FindResult
	if err := json.Unmarshal(substring.Body.Bytes(), &substringPayload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(substringPayload.Entries) != 1 || substringPayload.Entries[0].Name != "guide.md" {
		t.Fatalf("子串匹配 entries = %#v", substringPayload.Entries)
	}
}

func TestFindRejectsInvalidMatch(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/find?name=a&match=fuzzy"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "invalid_match") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestSearchIncludesContext(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos/example/search?q=two&context=1"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var result vault.SearchResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("matches = %#v", result.Matches)
	}
	if len(result.Matches[0].Before) != 1 || result.Matches[0].Before[0].Text != "line one" {
		t.Fatalf("before = %#v", result.Matches[0].Before)
	}
}

func TestListRepositoriesMergesSyncState(t *testing.T) {
	t.Parallel()

	syncedAt := time.Date(2026, 5, 6, 7, 8, 0, 0, time.UTC)
	handler := newTestRouterWithStates(t, stubStates{"example": state.RepoState{
		LastSyncAt:  syncedAt,
		GitBytes:    2048,
		Blocked:     true,
		BlockedSize: 4096,
	}})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized(http.MethodGet, "/v1/repos"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Repositories []struct {
			Name        string    `json:"name"`
			LastSyncAt  time.Time `json:"lastSyncAt"`
			GitBytes    int64     `json:"gitBytes"`
			Blocked     bool      `json:"blocked"`
			BlockedSize int64     `json:"blockedSize"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Repositories) != 1 {
		t.Fatalf("repositories = %#v", payload.Repositories)
	}

	repository := payload.Repositories[0]
	if !repository.LastSyncAt.Equal(syncedAt) || repository.GitBytes != 2048 {
		t.Fatalf("同步事实 = %#v", repository)
	}
	if !repository.Blocked || repository.BlockedSize != 4096 {
		t.Fatalf("保护状态 = %#v", repository)
	}
}
