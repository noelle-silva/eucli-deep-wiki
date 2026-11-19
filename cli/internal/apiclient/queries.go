package apiclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// RepositoriesOptions 描述一次名单总览。
type RepositoriesOptions struct {
	// Measure 为真时让服务端现测每个已落盘仓库的元数据体积。
	Measure bool
}

// RepoMeta 是一个仓库的元数据。
type RepoMeta struct {
	Name     string `json:"name"`
	Branch   string `json:"branch,omitempty"`
	Revision string `json:"revision,omitempty"`
	Shallow  bool   `json:"shallow,omitempty"`

	// URL 是名单里声明的地址；History 是声明的历史档位（shallow / full），未声明时为空。
	URL     string `json:"url,omitempty"`
	History string `json:"history,omitempty"`
	// EffectiveHistory 是实际生效的档位：声明为空时由服务端按默认值补齐。
	EffectiveHistory string `json:"effectiveHistory,omitempty"`
	// OnDisk 表示这个仓库在服务端磁盘上是否已经落盘。
	OnDisk bool `json:"onDisk"`

	// LastSyncAt 是最近一次同步成功的时间，GitBytes 是当时测得的 .git 体积。
	LastSyncAt time.Time `json:"lastSyncAt,omitempty"`
	GitBytes   int64     `json:"gitBytes,omitempty"`
	// MeasuredBytes 只在请求 measure 时出现，是本次现测的体积。
	MeasuredBytes *int64 `json:"measuredBytes,omitempty"`

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

// SearchOptions 描述一次检索的边界。
type SearchOptions struct {
	Query  string
	Path   string
	Limit  int
	Offset int
	// Context 是每处命中带出的上下文行数（前后各这么多）。
	Context int
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

// ReloadResult 是一次重载的结果，与服务端重载接口的响应逐一对应。
type ReloadResult struct {
	At             time.Time `json:"at"`
	ConfigReloaded bool      `json:"configReloaded"`
	IndexReloaded  bool      `json:"indexReloaded"`
	Repositories   int       `json:"repositories"`
	NeedsRestart   []string  `json:"needsRestart"`
	Error          string    `json:"error,omitempty"`
}

// Repositories 列出全部仓库；options.Measure 为真时附带本次现测的体积。
func (c *Client) Repositories(ctx context.Context, options RepositoriesOptions) ([]RepoMeta, error) {
	query := url.Values{}
	if options.Measure {
		query.Set("measure", "true")
	}

	var payload struct {
		Repositories []RepoMeta `json:"repositories"`
	}
	spec := request{method: http.MethodGet, path: "/v1/repos", query: query}
	if err := c.call(ctx, spec, &payload); err != nil {
		return nil, err
	}
	return payload.Repositories, nil
}

// Meta 读取单个仓库的元数据。
func (c *Client) Meta(ctx context.Context, name string) (RepoMeta, error) {
	var meta RepoMeta
	spec := request{method: http.MethodGet, path: "/v1/repos/" + url.PathEscape(name)}
	if err := c.call(ctx, spec, &meta); err != nil {
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
	spec := request{method: http.MethodGet, path: "/v1/repos/" + url.PathEscape(name) + "/tree", query: query}
	if err := c.call(ctx, spec, &tree); err != nil {
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
	spec := request{method: http.MethodGet, path: "/v1/repos/" + url.PathEscape(name) + "/search", query: values}
	if err := c.call(ctx, spec, &result); err != nil {
		return SearchResult{}, err
	}
	return result, nil
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
	spec := request{method: http.MethodGet, path: "/v1/repos/" + url.PathEscape(name) + "/find", query: values}
	if err := c.call(ctx, spec, &result); err != nil {
		return FindResult{}, err
	}
	return result, nil
}

// File 读取文件内容，可按行切段、按字节截断。
//
// 正文是纯文本、附带信息在响应头里，因此这里不走 JSON 通道。
func (c *Client) File(ctx context.Context, name string, options FileOptions) (File, error) {
	query := url.Values{}
	query.Set("path", options.Path)
	if options.Lines != "" {
		query.Set("lines", options.Lines)
	}
	if options.MaxBytes > 0 {
		query.Set("maxBytes", strconv.FormatInt(options.MaxBytes, 10))
	}

	spec := request{method: http.MethodGet, path: "/v1/repos/" + url.PathEscape(name) + "/file", query: query}
	opened, err := c.open(ctx, spec)
	if err != nil {
		return File{}, err
	}
	defer opened.close()

	body, err := io.ReadAll(opened.Body)
	if err != nil {
		return File{}, err
	}

	return File{
		Repo:          name,
		Path:          options.Path,
		Revision:      opened.Header.Get("X-Repo-Revision"),
		Content:       string(body),
		Size:          headerInt64(opened.Response, "X-File-Size"),
		ReturnedBytes: headerInt64(opened.Response, "X-File-Bytes"),
		TotalLines:    headerInt(opened.Response, "X-File-Total-Lines"),
		FirstLine:     headerInt(opened.Response, "X-File-First-Line"),
		LastLine:      headerInt(opened.Response, "X-File-Last-Line"),
		Truncated:     opened.Header.Get("X-File-Truncated"),
	}, nil
}

// Archive 下载打包好的快照，includes 形如「仓库」或「仓库:路径」。
func (c *Client) Archive(ctx context.Context, includes []string, format string, w io.Writer) error {
	values := url.Values{}
	for _, include := range includes {
		values.Add("include", include)
	}
	if format != "" {
		values.Set("format", format)
	}
	return c.stream(ctx, request{method: http.MethodGet, path: "/v1/archive", query: values}, w)
}

// Bundle 下载带完整历史的 bundle 文件。
func (c *Client) Bundle(ctx context.Context, name string, w io.Writer) error {
	spec := request{method: http.MethodGet, path: "/v1/repos/" + url.PathEscape(name) + "/bundle"}
	return c.stream(ctx, spec, w)
}

// State 读取服务端记录的体积保护状态。
func (c *Client) State(ctx context.Context) (map[string]RepoState, error) {
	var payload struct {
		Repositories map[string]RepoState `json:"repositories"`
	}
	if err := c.call(ctx, request{method: http.MethodGet, path: "/v1/state"}, &payload); err != nil {
		return nil, err
	}
	return payload.Repositories, nil
}

// Status 读取服务端同步状态快照，原样返回 JSON。
func (c *Client) Status(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.call(ctx, request{method: http.MethodGet, path: "/status"}, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// SyncRepo 触发单个仓库的立即同步。
func (c *Client) SyncRepo(ctx context.Context, name string) (SyncResult, error) {
	var result SyncResult
	spec := request{
		method: http.MethodPost,
		path:   "/v1/repos/" + url.PathEscape(name) + "/sync",
		class:  classSync,
	}
	if err := c.call(ctx, spec, &result); err != nil {
		return SyncResult{}, err
	}
	return result, nil
}

// SyncAll 触发一轮全量同步，服务端立即返回。
func (c *Client) SyncAll(ctx context.Context) error {
	spec := request{method: http.MethodPost, path: "/v1/sync", class: classSync}
	return c.call(ctx, spec, nil, http.StatusOK, http.StatusAccepted)
}

// CheckRepo 让服务端探测一次远端可达性；探测不改动本地仓库。
func (c *Client) CheckRepo(ctx context.Context, name string) (RemoteStatus, error) {
	var status RemoteStatus
	spec := request{method: http.MethodPost, path: "/v1/repos/" + url.PathEscape(name) + "/check"}
	if err := c.call(ctx, spec, &status); err != nil {
		return RemoteStatus{}, err
	}
	return status, nil
}

// Reload 让服务立刻重载配置与索引。
// 重载失败时服务端仍然返回结果，失败原因在 Error 里，服务继续用旧配置运行。
func (c *Client) Reload(ctx context.Context) (ReloadResult, error) {
	var result ReloadResult
	if err := c.call(ctx, request{method: http.MethodPost, path: "/v1/reload"}, &result); err != nil {
		return ReloadResult{}, err
	}
	return result, nil
}
