// Package apiclient 封装对 eucli-deep-wiki 只读接口的调用。
//
// 只依赖标准库，不引入服务端包；返回结构直接对应线上 JSON。
package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// APIError 表示服务端返回的错误。
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("HTTP %d %s: %s", e.StatusCode, e.Code, e.Message)
}

// RepoMeta 是一个仓库的元数据。
type RepoMeta struct {
	Name     string `json:"name"`
	Branch   string `json:"branch,omitempty"`
	Revision string `json:"revision,omitempty"`
}

// Entry 是目录项。
type Entry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size,omitempty"`
}

// Tree 是目录树结果。
type Tree struct {
	Repo      string  `json:"repo"`
	Path      string  `json:"path"`
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated,omitempty"`
}

// Match 是一处检索命中。
type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// SearchResult 是检索结果。
type SearchResult struct {
	Repo      string  `json:"repo"`
	Query     string  `json:"query"`
	Matches   []Match `json:"matches"`
	Truncated bool    `json:"truncated,omitempty"`
}

// File 是文件读取结果。
type File struct {
	Repo     string
	Path     string
	Revision string
	Content  string
}

// Client 是只读接口客户端。
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New 构建客户端，地址末尾的斜杠会被去掉。
func New(baseURL string, apiKey string) (*Client, error) {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return nil, fmt.Errorf("server is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("server must start with http:// or https://")
	}

	return &Client{
		baseURL: strings.TrimRight(trimmed, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Repositories 列出全部仓库。
func (c *Client) Repositories(ctx context.Context) ([]RepoMeta, error) {
	var payload struct {
		Repositories []RepoMeta `json:"repositories"`
	}
	if err := c.getJSON(ctx, "/v1/repos", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Repositories, nil
}

// Meta 读取单个仓库的元数据。
func (c *Client) Meta(ctx context.Context, name string) (RepoMeta, error) {
	var meta RepoMeta
	path := "/v1/repos/" + url.PathEscape(name)
	if err := c.getJSON(ctx, path, nil, &meta); err != nil {
		return RepoMeta{}, err
	}
	return meta, nil
}

// Tree 列目录。
func (c *Client) Tree(ctx context.Context, name string, target string) (Tree, error) {
	query := url.Values{}
	if target != "" {
		query.Set("path", target)
	}

	var tree Tree
	path := "/v1/repos/" + url.PathEscape(name) + "/tree"
	if err := c.getJSON(ctx, path, query, &tree); err != nil {
		return Tree{}, err
	}
	return tree, nil
}

// Search 在仓库内检索。
func (c *Client) Search(ctx context.Context, name string, query string, limit int) (SearchResult, error) {
	values := url.Values{}
	values.Set("q", query)
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}

	var result SearchResult
	path := "/v1/repos/" + url.PathEscape(name) + "/search"
	if err := c.getJSON(ctx, path, values, &result); err != nil {
		return SearchResult{}, err
	}
	return result, nil
}

// File 读取文件内容。
func (c *Client) File(ctx context.Context, name string, target string) (File, error) {
	query := url.Values{}
	query.Set("path", target)

	endpoint := c.baseURL + "/v1/repos/" + url.PathEscape(name) + "/file?" + query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return File{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)

	response, err := c.http.Do(request)
	if err != nil {
		return File{}, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return File{}, err
	}
	if response.StatusCode != http.StatusOK {
		return File{}, decodeError(response.StatusCode, body)
	}

	return File{
		Repo:     name,
		Path:     target,
		Revision: response.Header.Get("X-Repo-Revision"),
		Content:  string(body),
	}, nil
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return decodeError(response.StatusCode, body)
	}

	return json.Unmarshal(body, out)
}

func decodeError(statusCode int, body []byte) error {
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return &APIError{StatusCode: statusCode, Message: strings.TrimSpace(string(body))}
	}
	return &APIError{StatusCode: statusCode, Code: payload.Code, Message: payload.Message}
}
