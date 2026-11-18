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
	// Context 是每处命中带出的上下文行数（前后各这么多）。
	Context int
}

// FindOptions 描述一次文件名检索。
type FindOptions struct {
	Pattern string
	// Path 是检索范围：服务端从该目录开始遍历，而不是在结果里过滤。
	Path string
	// Substring 为真时按「包含」比较，否则要求文件名与模式一致。
	Substring bool
	Limit     int
	Offset    int
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

// Find 按文件名检索，支持通配符、精确与子串三种口径。
func (c *Client) Find(ctx context.Context, name string, options FindOptions) (FindResult, error) {
	values := url.Values{}
	values.Set("name", options.Pattern)
	if options.Path != "" {
		values.Set("path", options.Path)
	}
	if options.Substring {
		values.Set("match", "substring")
	}
	if options.Limit > 0 {
		values.Set("limit", strconv.Itoa(options.Limit))
	}
	if options.Offset > 0 {
		values.Set("offset", strconv.Itoa(options.Offset))
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
	Shallow  bool   `json:"shallow,omitempty"`

	// LastSyncAt 是最近一次同步成功的时间，GitBytes 是当时测得的 .git 体积。
	LastSyncAt time.Time `json:"lastSyncAt,omitempty"`
	GitBytes   int64     `json:"gitBytes,omitempty"`

	// Blocked 表示体积超限已停止自动同步；RemoteMissing 表示远端已判定消失。
	Blocked       bool  `json:"blocked,omitempty"`
	BlockedSize   int64 `json:"blockedSize,omitempty"`
	RemoteMissing bool  `json:"remoteMissing,omitempty"`
}

// Entry 是目录项。
type Entry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Type       string    `json:"type"`
	Size       int64     `json:"size"`
	Depth      int       `json:"depth"`
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`
}

// Tree 是目录树结果。
type Tree struct {
	Repo      string  `json:"repo"`
	Revision  string  `json:"revision"`
	Path      string  `json:"path"`
	Depth     int     `json:"depth"`
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

// TreeOptions 描述一次目录展开。
type TreeOptions struct {
	Path string
	// Depth 是要展开的层数；1 表示只列直接子项。
	Depth int
	// DirsOnly 只要目录，用于快速看骨架。
	DirsOnly bool
}

// Match 是一处检索命中。
type Match struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
	// Before 与 After 是紧邻命中行的上下文。
	Before []ContextLine `json:"before,omitempty"`
	After  []ContextLine `json:"after,omitempty"`
}

// ContextLine 是命中行前后的一行原文。
type ContextLine struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// OversizedFile 是一个因为超过单文件上限而没有参与检索的文件。
type OversizedFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// SearchResult 是检索结果。
type SearchResult struct {
	Repo           string          `json:"repo"`
	Revision       string          `json:"revision"`
	Query          string          `json:"query"`
	Path           string          `json:"path,omitempty"`
	Offset         int             `json:"offset"`
	Matches        []Match         `json:"matches"`
	Scanned        int             `json:"scannedFiles"`
	ScannedBytes   int64           `json:"scannedBytes"`
	Oversized      []OversizedFile `json:"oversizedFiles,omitempty"`
	OversizedCount int             `json:"oversizedFilesCount,omitempty"`
	Truncated      bool            `json:"truncated"`
}

// FileOptions 描述一次文件读取。
type FileOptions struct {
	Path string
	// Lines 是行区间写法（如 362-520、362-、-520），语法由服务端解析。
	Lines string
	// MaxBytes 是返回内容的字节上限；0 表示不额外限制。
	MaxBytes int64
}

// TruncatedByBytes 是「正文被 MaxBytes 截断」的标记值，与服务端响应头一致。
const TruncatedByBytes = "bytes"

// File 是文件读取结果：正文之外还带「文件多长、这次给了哪一段」。
type File struct {
	Repo     string `json:"repo"`
	Path     string `json:"path"`
	Revision string `json:"revision"`
	Content  string `json:"content"`
	// Size 是文件总字节数，ReturnedBytes 是本次返回正文的字节数。
	Size          int64 `json:"size"`
	ReturnedBytes int64 `json:"returnedBytes"`
	TotalLines    int   `json:"totalLines"`
	FirstLine     int   `json:"firstLine"`
	LastLine      int   `json:"lastLine"`
	// Truncated 说明正文为何不完整：目前只有 TruncatedByBytes。
	Truncated string `json:"truncated,omitempty"`
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

// Tree 列目录；Depth 大于 1 时服务端一次展开多层。
func (c *Client) Tree(ctx context.Context, name string, options TreeOptions) (Tree, error) {
	query := url.Values{}
	if options.Path != "" {
		query.Set("path", options.Path)
	}
	if options.Depth > 0 {
		query.Set("depth", strconv.Itoa(options.Depth))
	}
	if options.DirsOnly {
		query.Set("dirsOnly", "true")
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
	if options.Context > 0 {
		values.Set("context", strconv.Itoa(options.Context))
	}

	var result SearchResult
	path := "/v1/repos/" + url.PathEscape(name) + "/search"
	if err := c.getJSON(ctx, path, values, &result); err != nil {
		return SearchResult{}, err
	}
	return result, nil
}

// File 读取文件内容，可按行切段、按字节截断。
func (c *Client) File(ctx context.Context, name string, options FileOptions) (File, error) {
	query := url.Values{}
	query.Set("path", options.Path)
	if options.Lines != "" {
		query.Set("lines", options.Lines)
	}
	if options.MaxBytes > 0 {
		query.Set("maxBytes", strconv.FormatInt(options.MaxBytes, 10))
	}

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

	file := File{
		Repo:          name,
		Path:          options.Path,
		Revision:      response.Header.Get("X-Repo-Revision"),
		Content:       string(body),
		Size:          headerInt64(response, "X-File-Size"),
		ReturnedBytes: headerInt64(response, "X-File-Bytes"),
		TotalLines:    headerInt(response, "X-File-Total-Lines"),
		FirstLine:     headerInt(response, "X-File-First-Line"),
		LastLine:      headerInt(response, "X-File-Last-Line"),
		Truncated:     response.Header.Get("X-File-Truncated"),
	}
	return file, nil
}

// headerInt 读一个整数响应头；缺失或非法都按 0 处理。
func headerInt(response *http.Response, key string) int {
	value, err := strconv.Atoi(response.Header.Get(key))
	if err != nil {
		return 0
	}
	return value
}

// headerInt64 读一个 64 位整数响应头；缺失或非法都按 0 处理。
func headerInt64(response *http.Response, key string) int64 {
	value, err := strconv.ParseInt(response.Header.Get(key), 10, 64)
	if err != nil {
		return 0
	}
	return value
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
