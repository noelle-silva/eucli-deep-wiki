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

// SearchOptions 描述一次检索的边界。
type SearchOptions struct {
	Query  string
	Path   string
	Limit  int
	Offset int
}

// FoundEntry 是按文件名命中的文件。
type FoundEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// FindResult 是文件名检索结果。
type FindResult struct {
	Repo      string       `json:"repo"`
	Revision  string       `json:"revision"`
	Pattern   string       `json:"pattern"`
	Path      string       `json:"path,omitempty"`
	Offset    int          `json:"offset"`
	Entries   []FoundEntry `json:"entries"`
	Scanned   int          `json:"scannedFiles"`
	Truncated bool         `json:"truncated"`
}

// Find 按文件名检索，支持通配符与子串。
func (c *Client) Find(ctx context.Context, name string, pattern string, target string, limit int, offset int) (FindResult, error) {
	values := url.Values{}
	values.Set("name", pattern)
	if target != "" {
		values.Set("path", target)
	}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		values.Set("offset", strconv.Itoa(offset))
	}

	var result FindResult
	endpoint := "/v1/repos/" + url.PathEscape(name) + "/find"
	if err := c.getJSON(ctx, endpoint, values, &result); err != nil {
		return FindResult{}, err
	}
	return result, nil
}

// RepoMeta 是一个仓库的元数据。
type RepoMeta struct {
	Name     string `json:"name"`
	Branch   string `json:"branch,omitempty"`
	Revision string `json:"revision,omitempty"`
}

// Entry 是目录项。
type Entry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`
}

// Tree 是目录树结果。
type Tree struct {
	Repo      string  `json:"repo"`
	Revision  string  `json:"revision"`
	Path      string  `json:"path"`
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

// Match 是一处检索命中。
type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// SearchResult 是检索结果。
type SearchResult struct {
	Repo         string  `json:"repo"`
	Revision     string  `json:"revision"`
	Query        string  `json:"query"`
	Path         string  `json:"path,omitempty"`
	Offset       int     `json:"offset"`
	Matches      []Match `json:"matches"`
	Scanned      int     `json:"scannedFiles"`
	ScannedBytes int64   `json:"scannedBytes"`
	Truncated    bool    `json:"truncated"`
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

// WithTimeout 返回一份使用指定超时的副本。
func (c *Client) WithTimeout(d time.Duration) *Client {
	copied := *c
	copied.http = &http.Client{Timeout: d}
	return &copied
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

// Search 在仓库内检索，可限定目录并用 offset 翻页。
func (c *Client) Search(ctx context.Context, name string, options SearchOptions) (SearchResult, error) {
	values := url.Values{}
	values.Set("q", options.Query)
	if options.Path != "" {
		values.Set("path", options.Path)
	}
	if options.Limit > 0 {
		values.Set("limit", strconv.Itoa(options.Limit))
	}
	if options.Offset > 0 {
		values.Set("offset", strconv.Itoa(options.Offset))
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

// Archive 下载打包好的快照，includes 形如 "仓库" 或 "仓库:路径"。
func (c *Client) Archive(ctx context.Context, includes []string, format string, w io.Writer) error {
	values := url.Values{}
	for _, include := range includes {
		values.Add("include", include)
	}
	if format != "" {
		values.Set("format", format)
	}
	return c.stream(ctx, "/v1/archive", values, w)
}

// Bundle 下载带完整历史的 bundle 文件。
func (c *Client) Bundle(ctx context.Context, name string, w io.Writer) error {
	return c.stream(ctx, "/v1/repos/"+url.PathEscape(name)+"/bundle", nil, w)
}

func (c *Client) stream(ctx context.Context, path string, query url.Values, w io.Writer) error {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)

	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		return decodeError(response.StatusCode, body)
	}

	_, err = io.Copy(w, response.Body)
	return err
}

// State 读取服务端记录的体积保护状态。
func (c *Client) State(ctx context.Context) (map[string]RepoState, error) {
	var payload struct {
		Repositories map[string]RepoState `json:"repositories"`
	}
	if err := c.getJSON(ctx, "/v1/state", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Repositories, nil
}

// RepoState 是服务端记录的单个仓库状态。
type RepoState struct {
	LastGCSize  int64  `json:"lastGCSize,omitempty"`
	LastGCAt    string `json:"lastGCAt,omitempty"`
	Blocked     bool   `json:"blocked,omitempty"`
	BlockedAt   string `json:"blockedAt,omitempty"`
	BlockedSize int64  `json:"blockedSize,omitempty"`

	ConsecutiveFailures int    `json:"consecutiveFailures,omitempty"`
	LastError           string `json:"lastError,omitempty"`
	RemoteMissing       bool   `json:"remoteMissing,omitempty"`
	RemoteMissingAt     string `json:"remoteMissingAt,omitempty"`
}

// Status 读取服务端同步状态快照，原样返回 JSON。
func (c *Client) Status(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.getJSON(ctx, "/status", nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// SyncResult 是一次按需同步的结果。
type SyncResult struct {
	Name     string `json:"name"`
	Action   string `json:"action"`
	Branch   string `json:"branch,omitempty"`
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"duration"`
}

// RemoteStatus 是一次远端可达性探测的结果。
type RemoteStatus struct {
	Repo      string `json:"repo"`
	Reachable bool   `json:"reachable"`
	Error     string `json:"error,omitempty"`
}

// CheckRepo 让服务端探测一次远端可达性；探测不改动本地仓库。
func (c *Client) CheckRepo(ctx context.Context, name string) (RemoteStatus, error) {
	var status RemoteStatus
	path := "/v1/repos/" + url.PathEscape(name) + "/check"
	if err := c.post(ctx, c.http, path, &status); err != nil {
		return RemoteStatus{}, err
	}
	return status, nil
}

// SyncRepo 触发单个仓库的立即同步。
func (c *Client) SyncRepo(ctx context.Context, name string) (SyncResult, error) {
	var result SyncResult
	path := "/v1/repos/" + url.PathEscape(name) + "/sync"
	if err := c.post(ctx, syncClient(), path, &result); err != nil {
		return SyncResult{}, err
	}
	return result, nil
}

// SyncAll 触发一轮全量同步，服务端立即返回。
func (c *Client) SyncAll(ctx context.Context) error {
	return c.post(ctx, syncClient(), "/v1/sync", nil)
}

// ReloadResult 是一次重载的结果，与服务端 /v1/reload 的响应逐一对应。
type ReloadResult struct {
	At             time.Time `json:"at"`
	ConfigReloaded bool      `json:"configReloaded"`
	IndexReloaded  bool      `json:"indexReloaded"`
	Repositories   int       `json:"repositories"`
	NeedsRestart   []string  `json:"needsRestart"`
	Error          string    `json:"error,omitempty"`
}

// Reload 让服务立刻重载配置与索引。
// 重载失败时服务端仍然返回结果，失败原因在 Error 里，服务继续用旧配置运行。
func (c *Client) Reload(ctx context.Context) (ReloadResult, error) {
	var result ReloadResult
	if err := c.post(ctx, c.http, "/v1/reload", &result); err != nil {
		return ReloadResult{}, err
	}
	return result, nil
}

// syncClient 是同步类请求专用的客户端：单个大仓库的同步可能持续几分钟。
func syncClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Minute}
}

func (c *Client) post(ctx context.Context, httpClient *http.Client, path string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Accept", "application/json")

	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
		return decodeError(response.StatusCode, body)
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, out)
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
