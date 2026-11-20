package apiclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const testKey = "0123456789abcdef"

func TestRepositoriesSendsBearerKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
			t.Errorf("Authorization = %q", got)
		}
		if r.URL.Path != "/v1/repos" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"repositories": []map[string]string{
				{"name": "example", "branch": "main", "revision": "abc123"},
			},
		})
	}))
	defer server.Close()

	client, err := New(server.URL, testKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	repositories, err := client.Repositories(context.Background(), RepositoriesOptions{})
	if err != nil {
		t.Fatalf("Repositories() error = %v", err)
	}
	if len(repositories) != 1 || repositories[0].Name != "example" || repositories[0].Revision != "abc123" {
		t.Fatalf("repositories = %#v", repositories)
	}
}

func TestFileReturnsContentAndRevision(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/repos/example/file" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("path"); got != "docs/a.md" {
			t.Errorf("path query = %q", got)
		}
		w.Header().Set("X-Repo-Revision", "abc123")
		_, _ = w.Write([]byte("# hello\n"))
	}))
	defer server.Close()

	client, err := New(server.URL+"/", testKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	file, err := client.File(context.Background(), "example", FileOptions{Path: "docs/a.md"})
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}
	if file.Content != "# hello\n" {
		t.Fatalf("content = %q", file.Content)
	}
	if file.Revision != "abc123" {
		t.Fatalf("revision = %q", file.Revision)
	}
}

func TestErrorMapping(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"path_not_found","message":"路径不存在"}`))
	}))
	defer server.Close()

	client, err := New(server.URL, testKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = client.File(context.Background(), "example", FileOptions{Path: "missing.md"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusNotFound || apiErr.Code != "path_not_found" {
		t.Fatalf("apiErr = %#v", apiErr)
	}
}

func TestSearchPassesQueryAndLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "needle" {
			t.Errorf("q = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "5" {
			t.Errorf("limit = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"repo":    "example",
			"query":   "needle",
			"matches": []map[string]any{{"path": "a.md", "line": 3, "text": "needle here"}},
		})
	}))
	defer server.Close()

	client, err := New(server.URL, testKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := client.Search(context.Background(), "example", SearchOptions{Query: "needle", Limit: 5})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Matches) != 1 || result.Matches[0].Line != 3 {
		t.Fatalf("result = %#v", result)
	}
}

func TestNewRejectsInvalidServer(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "127.0.0.1:9997", "ftp://example.com"} {
		if _, err := New(value, testKey); err == nil {
			t.Fatalf("New(%q) error = nil, want error", value)
		}
	}
}

func TestCheckRepoSendsProbe(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/v1/repos/example/check" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
			t.Errorf("Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"repo":      "example",
			"reachable": false,
			"error":     "remote repository unavailable: Repository not found",
		})
	}))
	defer server.Close()

	client, err := New(server.URL, testKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	status, err := client.CheckRepo(context.Background(), "example")
	if err != nil {
		t.Fatalf("CheckRepo() error = %v", err)
	}
	if status.Repo != "example" || status.Reachable {
		t.Fatalf("status = %#v", status)
	}
	if !strings.Contains(status.Error, "Repository not found") {
		t.Fatalf("Error = %q", status.Error)
	}
}

func TestCheckRepoMapsMissingRepository(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"repo_not_found","message":"仓库不存在或尚未同步"}`))
	}))
	defer server.Close()

	client, err := New(server.URL, testKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = client.CheckRepo(context.Background(), "nope")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusNotFound || apiErr.Code != "repo_not_found" {
		t.Fatalf("apiErr = %#v", apiErr)
	}
}

func TestFileSendsRangeAndReadsRangeHeaders(t *testing.T) {
	t.Parallel()

	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("X-Repo-Revision", "abc123")
		w.Header().Set("X-File-Total-Lines", "40")
		w.Header().Set("X-File-First-Line", "3")
		w.Header().Set("X-File-Last-Line", "12")
		w.Header().Set("X-File-Bytes", "9")
		w.Header().Set("X-File-Size", "100")
		w.Header().Set("X-File-Truncated", "bytes")
		_, _ = w.Write([]byte("line two\n"))
	}))
	defer server.Close()

	client, err := New(server.URL, testKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	file, err := client.File(context.Background(), "example", FileOptions{
		Path:     "docs/guide.md",
		Lines:    "3-12",
		MaxBytes: 9,
	})
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}

	for _, want := range []string{"path=docs%2Fguide.md", "lines=3-12", "maxBytes=9"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query = %q, want 含 %q", query, want)
		}
	}
	if file.TotalLines != 40 || file.FirstLine != 3 || file.LastLine != 12 {
		t.Fatalf("行信息 = %d / %d-%d", file.TotalLines, file.FirstLine, file.LastLine)
	}
	if file.Size != 100 || file.ReturnedBytes != 9 || file.Truncated != "bytes" {
		t.Fatalf("file = %#v", file)
	}
}

func TestTreeSendsDepthAndDirsOnly(t *testing.T) {
	t.Parallel()

	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"repo":"example","path":"","depth":3,"entries":[],"truncated":false}`))
	}))
	defer server.Close()

	client, err := New(server.URL, testKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tree, err := client.Tree(context.Background(), "example", TreeOptions{Depth: 3, DirsOnly: true})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if !strings.Contains(query, "depth=3") || !strings.Contains(query, "dirsOnly=true") {
		t.Fatalf("query = %q", query)
	}
	if tree.Depth != 3 {
		t.Fatalf("Depth = %d", tree.Depth)
	}
}

// TestTreeJoinsExcludeNames 守住 exclude 的线上写法：逗号分隔的名表，空列表时不发。
func TestTreeJoinsExcludeNames(t *testing.T) {
	t.Parallel()

	queries := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"repo":"example","path":"","depth":1,"entries":[],"truncated":false}`))
	}))
	defer server.Close()

	client, err := New(server.URL, testKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := client.Tree(context.Background(), "example", TreeOptions{Exclude: []string{"node_modules", "vendor"}}); err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if _, err := client.Tree(context.Background(), "example", TreeOptions{}); err != nil {
		t.Fatalf("Tree() error = %v", err)
	}

	// 逗号在查询串里会被编码成 %2C，服务端解出来仍是逗号分隔的名表。
	decoded, err := url.ParseQuery(queries[0])
	if err != nil {
		t.Fatalf("ParseQuery() error = %v", err)
	}
	if got := decoded.Get("exclude"); got != "node_modules,vendor" {
		t.Fatalf("exclude = %q", got)
	}
	if strings.Contains(queries[1], "exclude") {
		t.Fatalf("不给名字时不该带 exclude：%q", queries[1])
	}
}

func TestFindSendsSubstringMatch(t *testing.T) {
	t.Parallel()

	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"repo":"example","pattern":"main","entries":[],"scannedFiles":0,"truncated":false}`))
	}))
	defer server.Close()

	client, err := New(server.URL, testKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := client.Find(context.Background(), "example", FindOptions{Pattern: "main", Substring: true}); err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if !strings.Contains(query, "match=substring") {
		t.Fatalf("query = %q", query)
	}
}

func TestRepositoriesSendsMeasureOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"repositories":[]}`))
	}))
	defer server.Close()

	client, err := New(server.URL, testKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := client.Repositories(context.Background(), RepositoriesOptions{}); err != nil {
		t.Fatalf("Repositories() error = %v", err)
	}
	// 缺省为假：不传 measure 就是没要求现测。
	if query != "" {
		t.Fatalf("query = %q, want 空", query)
	}

	if _, err := client.Repositories(context.Background(), RepositoriesOptions{Measure: true}); err != nil {
		t.Fatalf("Repositories() error = %v", err)
	}
	if query != "measure=true" {
		t.Fatalf("query = %q, want measure=true", query)
	}
}

func TestNewAcceptsPlainAndEncryptedSchemes(t *testing.T) {
	t.Parallel()

	// 协议不写死：明文与加密地址都照用，不做任何补前缀或改写。
	for _, value := range []string{"http://127.0.0.1:9997", "https://wiki.example.com/base"} {
		if _, err := New(value, testKey); err != nil {
			t.Fatalf("New(%q) error = %v", value, err)
		}
	}
}

func TestCompactHasItsOwnBudget(t *testing.T) {
	t.Parallel()

	if classCompact.budget() != compactBudget {
		t.Fatalf("classCompact.budget() = %v", classCompact.budget())
	}
	if compactBudget <= requestBudget {
		t.Fatalf("compactBudget = %v, requestBudget = %v", compactBudget, requestBudget)
	}
	if compactBudget < time.Minute {
		t.Fatalf("compactBudget = %v, want 分钟级", compactBudget)
	}
	if classSync.budget() != syncBudget || classRequest.budget() != requestBudget {
		t.Fatalf("按类预算对不上")
	}
}

func TestWithTimeoutOverridesBudget(t *testing.T) {
	t.Parallel()

	client, err := New("https://wiki.example.com", testKey)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	overridden := client.WithTimeout(time.Minute)
	if got := overridden.budget(classCompact); got != time.Minute {
		t.Fatalf("覆盖后的预算 = %v", got)
	}
	// 覆盖只作用于副本：原对象的按类预算不受影响。
	if got := client.budget(classCompact); got != compactBudget {
		t.Fatalf("原对象预算 = %v", got)
	}
}
