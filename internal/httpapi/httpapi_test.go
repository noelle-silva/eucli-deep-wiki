package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"eucli-deep-wiki/internal/status"
	"eucli-deep-wiki/internal/vault"
)

func TestListRepositories(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/repos", nil))

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
	request := httptest.NewRequest(http.MethodGet, "/v1/repos/example/file?path=docs/guide.md", nil)
	handler.ServeHTTP(recorder, request)

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
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/v1/repos/example/file?path=docs/guide.md", nil))
	etag := first.Header().Get("ETag")

	second := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/repos/example/file?path=docs/guide.md", nil)
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
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/repos/example/file", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestShowFileRejectsTraversal(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/repos/example/file?path=../secret.txt", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestShowFileReportsUnknownRepository(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/repos/nope/file?path=README.md", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

func TestSearchRequiresQuery(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/repos/example/search", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestSearchFindsMatches(t *testing.T) {
	t.Parallel()

	handler := newTestRouter(t)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/repos/example/search?q=two", nil))

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
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/repos/example/tree?path=docs", nil))

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

func newTestRouter(t *testing.T) http.Handler {
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
	return NewRouter(status.NewStore(), repoVault)
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
