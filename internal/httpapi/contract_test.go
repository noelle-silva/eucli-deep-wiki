package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"eucli-deep-wiki/internal/apiclient"
)

// TestClientContract 让真正的客户端打到真正的路由上。
//
// 两边的单元测试各自用自己想象中的 JSON，字段名一旦写岔，两边都会"通过"。
// 这里把服务端与客户端接起来跑一遍，任何命名不一致都会当场失败。
func TestClientContract(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(newRouterWithNestedRepository(t))
	defer server.Close()

	client, err := apiclient.New(server.URL, testAPIKey)
	if err != nil {
		t.Fatalf("apiclient.New() error = %v", err)
	}
	ctx := context.Background()

	repositories, err := client.Repositories(ctx)
	if err != nil {
		t.Fatalf("Repositories() error = %v", err)
	}
	if len(repositories) != 1 || repositories[0].Name != "example" {
		t.Fatalf("repositories = %#v", repositories)
	}
	if repositories[0].Revision != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("revision = %q", repositories[0].Revision)
	}

	meta, err := client.Meta(ctx, "example")
	if err != nil {
		t.Fatalf("Meta() error = %v", err)
	}
	if meta.Branch != "main" {
		t.Fatalf("branch = %q", meta.Branch)
	}

	tree, err := client.Tree(ctx, "example", apiclient.TreeOptions{Depth: 2, DirsOnly: true})
	if err != nil {
		t.Fatalf("Tree() error = %v", err)
	}
	if tree.Depth != 2 {
		t.Fatalf("tree depth = %d, want 2", tree.Depth)
	}
	if len(tree.Entries) != 2 || tree.Entries[0].Type != "dir" {
		t.Fatalf("tree entries = %#v", tree.Entries)
	}
	for _, entry := range tree.Entries {
		if entry.Depth == 0 {
			t.Fatalf("entry 缺少 depth: %#v", entry)
		}
	}

	file, err := client.File(ctx, "example", apiclient.FileOptions{Path: "docs/guide.md", Lines: "2-2"})
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}
	if file.Content != "line two\n" {
		t.Fatalf("content = %q", file.Content)
	}
	if file.TotalLines != 2 || file.FirstLine != 2 || file.LastLine != 2 {
		t.Fatalf("行信息 = %d / %d-%d", file.TotalLines, file.FirstLine, file.LastLine)
	}
	if file.Size != 18 || file.ReturnedBytes != 9 {
		t.Fatalf("字节信息 = %d / %d", file.Size, file.ReturnedBytes)
	}

	clamped, err := client.File(ctx, "example", apiclient.FileOptions{Path: "docs/guide.md", MaxBytes: 12})
	if err != nil {
		t.Fatalf("File(maxBytes) error = %v", err)
	}
	if clamped.Truncated != "bytes" || clamped.LastLine != 1 {
		t.Fatalf("截断信息 = %q / %d", clamped.Truncated, clamped.LastLine)
	}

	exact, err := client.Find(ctx, "example", apiclient.FindOptions{Pattern: "guide"})
	if err != nil {
		t.Fatalf("Find(exact) error = %v", err)
	}
	if len(exact.Entries) != 0 {
		t.Fatalf("精确匹配不该命中 guide.md: %#v", exact.Entries)
	}

	substring, err := client.Find(ctx, "example", apiclient.FindOptions{Pattern: "guide", Substring: true})
	if err != nil {
		t.Fatalf("Find(substring) error = %v", err)
	}
	if len(substring.Entries) != 1 || substring.Entries[0].Name != "guide.md" {
		t.Fatalf("子串匹配 = %#v", substring.Entries)
	}

	search, err := client.Search(ctx, "example", apiclient.SearchOptions{Query: "two", Context: 1})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(search.Matches) != 1 {
		t.Fatalf("matches = %#v", search.Matches)
	}
	if len(search.Matches[0].Before) != 1 || search.Matches[0].Before[0].Text != "line one" {
		t.Fatalf("context = %#v", search.Matches[0].Before)
	}

	_, err = client.File(ctx, "example", apiclient.FileOptions{Path: "missing.md"})
	var apiErr *apiclient.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "path_not_found" {
		t.Fatalf("缺失文件 error = %v", err)
	}

	_, err = client.File(ctx, "example", apiclient.FileOptions{Path: "docs/guide.md", Lines: "9-3"})
	if !errors.As(err, &apiErr) || apiErr.Code != "invalid_lines" {
		t.Fatalf("坏行区间 error = %v", err)
	}
}
