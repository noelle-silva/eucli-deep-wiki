package apiclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// 历史档位：契约里只有这两个取值。
const (
	// HistoryShallow 表示只保留浅克隆的历史。
	HistoryShallow = "shallow"
	// HistoryFull 表示保留完整历史。
	HistoryFull = "full"
)

// AddOptions 描述一次「加入名单」。
type AddOptions struct {
	// URLs 是名单里要写入的地址，至少一项；整批原子，任一项不合法则整批不生效。
	URLs []string
	// Branch 为空时由服务端决定分支。
	Branch string
	// History 为空时由服务端按默认档位落定。
	History string
	// Reload 为真时让新名单立即生效。
	Reload bool
}

// AddedRepository 是这次新加进名单的仓库。
type AddedRepository struct {
	Name             string `json:"name"`
	URL              string `json:"url"`
	Branch           string `json:"branch,omitempty"`
	History          string `json:"history,omitempty"`
	EffectiveHistory string `json:"effectiveHistory,omitempty"`
}

// ReloadOutcome 是「让新名单立即生效」这一步的结果。
type ReloadOutcome struct {
	ConfigReloaded bool   `json:"configReloaded"`
	IndexReloaded  bool   `json:"indexReloaded"`
	Repositories   int    `json:"repositories"`
	Error          string `json:"error,omitempty"`
}

// AddResult 是加入名单的结果；Reload 为假时 Reloaded 不出现。
type AddResult struct {
	Added        []AddedRepository `json:"added"`
	Repositories int               `json:"repositories"`
	Reloaded     *ReloadOutcome    `json:"reloaded,omitempty"`
}

// AddRepositories 把一批地址加入名单。
func (c *Client) AddRepositories(ctx context.Context, options AddOptions) (AddResult, error) {
	body := struct {
		URLs    []string `json:"urls"`
		Branch  string   `json:"branch,omitempty"`
		History string   `json:"history,omitempty"`
		Reload  bool     `json:"reload"`
	}{
		URLs:    options.URLs,
		Branch:  options.Branch,
		History: options.History,
		Reload:  options.Reload,
	}

	var result AddResult
	spec := request{method: http.MethodPost, path: "/v1/repos", body: body}
	if err := c.call(ctx, spec, &result); err != nil {
		return AddResult{}, err
	}
	return result, nil
}

// RemoveResult 是移出名单的结果。
//
// 两个磁盘事实分开：DataExisted 是操作前磁盘上有没有副本，DataKept 是操作后还留着没有。
// 只有两者都看，才能把「本来就没有副本」和「副本被删掉了」说成两件不同的事。
type RemoveResult struct {
	Name string `json:"name"`
	// Removed 表示这次确实摘掉了；本来就不在名单里时为假，但仍是成功。
	Removed bool `json:"removed"`
	// DataExisted 表示操作之前磁盘上是否已有副本。
	DataExisted bool `json:"dataExisted"`
	// DataKept 表示这次操作之后磁盘上是否还留着副本。
	DataKept bool `json:"dataKept"`
}

// RemoveRepository 把仓库从名单摘除；keepData 为真时保留磁盘数据。
func (c *Client) RemoveRepository(ctx context.Context, name string, keepData bool) (RemoveResult, error) {
	query := url.Values{}
	query.Set("keepData", strconv.FormatBool(keepData))

	var result RemoveResult
	spec := request{
		method: http.MethodDelete,
		path:   "/v1/repos/" + url.PathEscape(name),
		query:  query,
	}
	if err := c.call(ctx, spec, &result); err != nil {
		return RemoveResult{}, err
	}
	return result, nil
}

// PreviousState 是解除停止同步之前的状态。
type PreviousState struct {
	Blocked             bool   `json:"blocked"`
	BlockedSize         int64  `json:"blockedSize,omitempty"`
	RemoteMissing       bool   `json:"remoteMissing"`
	ConsecutiveFailures int    `json:"consecutiveFailures,omitempty"`
	LastError           string `json:"lastError,omitempty"`
}

// UnblockResult 是解除停止同步的结果；本来没被拦时 Cleared 为假，仍是成功。
type UnblockResult struct {
	Name     string        `json:"name"`
	Cleared  bool          `json:"cleared"`
	Previous PreviousState `json:"previous"`
}

// UnblockRepository 解除仓库的停止同步状态。
//
// 解除的是服务端自己的运行态，因此不会被下一次状态写回覆盖掉。
func (c *Client) UnblockRepository(ctx context.Context, name string, clearFailures bool) (UnblockResult, error) {
	body := struct {
		ClearFailures bool `json:"clearFailures"`
	}{ClearFailures: clearFailures}

	var result UnblockResult
	spec := request{
		method: http.MethodPost,
		path:   "/v1/repos/" + url.PathEscape(name) + "/unblock",
		body:   body,
	}
	if err := c.call(ctx, spec, &result); err != nil {
		return UnblockResult{}, err
	}
	return result, nil
}

// 就地瘦身的动作结论，四种全部是成功。
const (
	// Compacted 表示已经执行了转换。
	Compacted = "compacted"
	// WouldCompact 表示预演认为会转换。
	WouldCompact = "would_compact"
	// AlreadyShallow 表示本来就是浅克隆。
	AlreadyShallow = "already_shallow"
	// SkippedFullHistory 表示名单要求完整历史，跳过。
	SkippedFullHistory = "skipped_full_history"
)

// CompactState 是瘦身前后的一份状态：是否浅克隆 + 元数据体积。
type CompactState struct {
	Shallow  bool  `json:"shallow"`
	GitBytes int64 `json:"gitBytes"`
}

// CompactResult 是一次瘦身的结果。
type CompactResult struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	Branch string `json:"branch,omitempty"`
	// Before 与 After 是转换前后的两份状态；动作没有实际执行时 After 可能为空。
	Before     CompactState `json:"before"`
	After      CompactState `json:"after"`
	DurationMs int64        `json:"durationMs"`
}

// CompactRepository 让服务端就地转为浅克隆；dryRun 为真时只预演。
//
// 分钟级操作，用自己的等待预算，不沿用普通查询的等待上限。
func (c *Client) CompactRepository(ctx context.Context, name string, dryRun bool) (CompactResult, error) {
	query := url.Values{}
	if dryRun {
		query.Set("dryRun", "true")
	}

	var result CompactResult
	spec := request{
		method: http.MethodPost,
		path:   "/v1/repos/" + url.PathEscape(name) + "/compact",
		query:  query,
		class:  classCompact,
	}
	if err := c.call(ctx, spec, &result); err != nil {
		return CompactResult{}, err
	}
	return result, nil
}

// CacheFile 是服务端缓存里的一个产物文件。
type CacheFile struct {
	Name       string    `json:"name"`
	Repo       string    `json:"repo"`
	Revision   string    `json:"revision"`
	Bytes      int64     `json:"bytes"`
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`
}

// CacheListing 是缓存清单；目录不存在就是空清单。
type CacheListing struct {
	Files      []CacheFile `json:"files"`
	TotalBytes int64       `json:"totalBytes"`
}

// CacheEntries 列出缓存产物，可按仓库过滤。
func (c *Client) CacheEntries(ctx context.Context, repo string) (CacheListing, error) {
	query := url.Values{}
	if repo != "" {
		query.Set("repo", repo)
	}

	var listing CacheListing
	spec := request{method: http.MethodGet, path: "/v1/cache", query: query}
	if err := c.call(ctx, spec, &listing); err != nil {
		return CacheListing{}, err
	}
	return listing, nil
}

// SkippedFile 是一个没有被删除的缓存产物及其原因。
type SkippedFile struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// CacheClearResult 是一次缓存清理的结果。
type CacheClearResult struct {
	Removed int           `json:"removed"`
	Bytes   int64         `json:"bytes"`
	Skipped []SkippedFile `json:"skipped,omitempty"`
}

// ClearCache 删除缓存产物；confirm 必须为真，否则服务端拒绝。
//
// 只删缓存产物文件，目录里其他东西不会被碰；正在生成的会进 Skipped。
func (c *Client) ClearCache(ctx context.Context, repo string, confirm bool) (CacheClearResult, error) {
	query := url.Values{}
	if repo != "" {
		query.Set("repo", repo)
	}
	if confirm {
		query.Set("confirm", "1")
	}

	var result CacheClearResult
	spec := request{method: http.MethodDelete, path: "/v1/cache", query: query}
	if err := c.call(ctx, spec, &result); err != nil {
		return CacheClearResult{}, err
	}
	return result, nil
}
