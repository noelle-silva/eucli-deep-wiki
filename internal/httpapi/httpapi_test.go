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

	"eucli-deep-wiki/internal/state"
	"eucli-deep-wiki/internal/status"
	"eucli-deep-wiki/internal/vault"
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

func TestNewAuthenticatorRejectsEmptyKeys(t *testing.T) {
	t.Parallel()

	if _, err := NewAuthenticator(nil); err == nil {
		t.Fatal("NewAuthenticator() error = nil, want error")
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

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	return buildRouter(t, []string{testAPIKey}, nil, nil)
}

func newTestRouterWithKeys(t *testing.T, keys []string) http.Handler {
	t.Helper()
	return buildRouter(t, keys, nil, nil)
}

func newTestRouterWithSync(t *testing.T, trigger SyncTrigger) http.Handler {
	t.Helper()
	return buildRouter(t, []string{testAPIKey}, trigger, nil)
}

func newTestRouterWithRemote(t *testing.T, checker RemoteChecker) http.Handler {
	t.Helper()
	return buildRouter(t, []string{testAPIKey}, nil, checker)
}

func newTestRouterWithStates(t *testing.T, states StateSource) http.Handler {
	t.Helper()

	root := t.TempDir()
	repoVault, err := vault.New(root, []string{"example"}, vault.DefaultLimits())
	if err != nil {
		t.Fatalf("vault.New() error = %v", err)
	}
	auth, err := NewAuthenticator([]string{testAPIKey})
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}
	return NewRouter(Options{
		Store:      status.NewStore(),
		Vault:      repoVault,
		Auth:       auth,
		States:     states,
		CacheDir:   t.TempDir(),
		GitTimeout: 30 * time.Second,
	})
}

func buildRouter(t *testing.T, keys []string, trigger SyncTrigger, checker RemoteChecker) http.Handler {
	t.Helper()

	root := t.TempDir()
	repoDir := filepath.Join(root, "example")
	writeFile(t, repoDir, ".git/HEAD", "ref: refs/heads/main\n")
	writeFile(t, repoDir, ".git/refs/heads/main", "0123456789abcdef0123456789abcdef01234567\n")
	writeFile(t, repoDir, "docs/guide.md", "line one\nline two\n")

	repoVault, err := vault.New(root, []string{"example"}, vault.DefaultLimits())
	if err != nil {
		t.Fatalf("vault.New() error = %v", err)
	}

	auth, err := NewAuthenticator(keys)
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}
	return NewRouter(Options{
		Store:      status.NewStore(),
		Vault:      repoVault,
		Auth:       auth,
		Sync:       trigger,
		Remote:     checker,
		States:     nil,
		CacheDir:   t.TempDir(),
		GitTimeout: 30 * time.Second,
	})
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
