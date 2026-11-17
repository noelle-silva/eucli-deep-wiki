package apiclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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

	repositories, err := client.Repositories(context.Background())
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

	file, err := client.File(context.Background(), "example", "docs/a.md")
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

	_, err = client.File(context.Background(), "example", "missing.md")
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

	result, err := client.Search(context.Background(), "example", "needle", 5)
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
