package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"eucli-deep-wiki-server/internal/cache"
	"eucli-deep-wiki-server/internal/gitops"
	"eucli-deep-wiki-server/internal/live"
	"eucli-deep-wiki-server/internal/ops"
	"eucli-deep-wiki-server/internal/repoindex"
	"eucli-deep-wiki-server/internal/state"
)

// listedItem 是名单总览里的一条：原有字段 + 新增的名单事实。
type listedItem struct {
	Name             string `json:"name"`
	Branch           string `json:"branch"`
	Revision         string `json:"revision"`
	Shallow          bool   `json:"shallow"`
	LastSyncAt       string `json:"lastSyncAt"`
	GitBytes         int64  `json:"gitBytes"`
	Blocked          bool   `json:"blocked"`
	URL              string `json:"url"`
	History          string `json:"history"`
	EffectiveHistory string `json:"effectiveHistory"`
	OnDisk           bool   `json:"onDisk"`
	MeasuredBytes    *int64 `json:"measuredBytes"`
}

type listedPayload struct {
	Repositories []listedItem `json:"repositories"`
}

// listRepositoriesIn 发一次名单总览请求并解回列表。
func listRepositoriesIn(t *testing.T, environment *env, target string) []listedItem {
	t.Helper()

	recorder := environment.do(t, http.MethodGet, target, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	return decodeJSON[listedPayload](t, recorder).Repositories
}

// 名单总览在原有字段之上增补四个事实；原字段（含上次同步时记录的体积）保持原样。
func TestListRepositoriesReportsIndexFacts(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)

	repositories := listRepositoriesIn(t, environment, "/v1/repos")
	if len(repositories) != 1 {
		t.Fatalf("repositories = %#v", repositories)
	}

	item := repositories[0]
	if item.Name != "example" || item.Revision != testRevision || item.Branch != "main" {
		t.Fatalf("原有字段被改动: %#v", item)
	}
	if item.URL != "https://example.com/example.git" {
		t.Fatalf("url = %q", item.URL)
	}
	if item.History != "" {
		t.Fatalf("未声明档位时 history 应为空: %q", item.History)
	}
	if item.EffectiveHistory != repoindex.HistoryShallow {
		t.Fatalf("effectiveHistory = %q", item.EffectiveHistory)
	}
	if !item.OnDisk {
		t.Fatal("example 应已落盘")
	}
	if item.MeasuredBytes != nil {
		t.Fatalf("没要求测量时不该给体积: %d", *item.MeasuredBytes)
	}
}

// 现测体积只在被要求时测量，而且不改写「上次同步时记录的体积」。
func TestListRepositoriesMeasuresOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	environment := newEnv(t, envOptions{Git: &stubGit{size: 4096}})
	if err := environment.states.Put("example", state.RepoState{GitBytes: 1024}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	withoutMeasure := listRepositoriesIn(t, environment, "/v1/repos")
	if withoutMeasure[0].MeasuredBytes != nil {
		t.Fatalf("没要求测量时不该给体积: %#v", withoutMeasure[0].MeasuredBytes)
	}

	measured := listRepositoriesIn(t, environment, "/v1/repos?measure=1")
	if measured[0].MeasuredBytes == nil || *measured[0].MeasuredBytes != 4096 {
		t.Fatalf("measuredBytes = %#v", measured[0].MeasuredBytes)
	}
	if measured[0].GitBytes != 1024 {
		t.Fatalf("gitBytes 被改写了: %d", measured[0].GitBytes)
	}
}

// 名单里有、磁盘上还没有的仓库也要出现在总览里，只是没有体积可测。
func TestListRepositoriesReportsRepositoryWithoutCopy(t *testing.T) {
	t.Parallel()

	environment := newEnv(t, envOptions{
		Live: newLive(t, []string{testAPIKey}, []repoindex.Repository{
			{Name: "example", URL: "https://example.com/example.git"},
			{Name: "pending", URL: "https://example.com/pending.git", History: repoindex.HistoryFull},
		}, "example"),
		Git: &stubGit{size: 4096},
	})

	repositories := listRepositoriesIn(t, environment, "/v1/repos?measure=1")
	if len(repositories) != 2 {
		t.Fatalf("repositories = %#v", repositories)
	}

	pending := repositories[1]
	if pending.Name != "pending" || pending.OnDisk {
		t.Fatalf("pending = %#v", pending)
	}
	if pending.History != repoindex.HistoryFull || pending.EffectiveHistory != repoindex.HistoryFull {
		t.Fatalf("pending 档位 = %q / %q", pending.History, pending.EffectiveHistory)
	}
	if pending.MeasuredBytes != nil {
		t.Fatalf("没有副本就没有体积可测: %#v", pending.MeasuredBytes)
	}
}

func TestListRepositoriesRejectsInvalidMeasureFlag(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)

	recorder := environment.do(t, http.MethodGet, "/v1/repos?measure=maybe", nil)
	requireFailure(t, recorder, http.StatusBadRequest, "invalid_flag")
}

// addedItem 是一条新增记录。
type addedItem struct {
	Name             string `json:"name"`
	URL              string `json:"url"`
	Branch           string `json:"branch"`
	History          string `json:"history"`
	EffectiveHistory string `json:"effectiveHistory"`
}

type addPayload struct {
	Added        []addedItem  `json:"added"`
	Repositories int          `json:"repositories"`
	Reloaded     *live.Result `json:"reloaded"`
}

func TestAddRepositoriesAddsBatchAndReloads(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)
	environment.source.reload = live.Result{ConfigReloaded: true, IndexReloaded: true, Repositories: 3}

	recorder := environment.do(t, http.MethodPost, "/v1/repos", map[string]any{
		"urls":    []string{"https://example.com/first.git", "https://example.com/second.git"},
		"branch":  "main",
		"history": "full",
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	payload := decodeJSON[addPayload](t, recorder)
	if len(payload.Added) != 2 || payload.Added[0].Name != "first" || payload.Added[1].Name != "second" {
		t.Fatalf("added = %#v", payload.Added)
	}
	if payload.Added[0].URL != "https://example.com/first.git" || payload.Added[0].Branch != "main" {
		t.Fatalf("added[0] = %#v", payload.Added[0])
	}
	if payload.Added[0].History != repoindex.HistoryFull || payload.Added[0].EffectiveHistory != repoindex.HistoryFull {
		t.Fatalf("added[0] 档位 = %q / %q", payload.Added[0].History, payload.Added[0].EffectiveHistory)
	}
	if payload.Repositories != 3 {
		t.Fatalf("repositories = %d, want 3", payload.Repositories)
	}
	if payload.Reloaded == nil || !payload.Reloaded.IndexReloaded {
		t.Fatalf("reloaded = %#v", payload.Reloaded)
	}

	stored := environment.storedIndex(t)
	if len(stored) != 3 || stored[1] != "first" || stored[2] != "second" {
		t.Fatalf("落盘的名单 = %v", stored)
	}
}

// 不要求立即生效时，响应里不出现 reloaded 这一项。
func TestAddRepositoriesSkipsReloadWhenAsked(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)

	recorder := environment.do(t, http.MethodPost, "/v1/repos", map[string]any{
		"urls":   []string{"https://example.com/first.git"},
		"reload": false,
	})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "reloaded") {
		t.Fatalf("没要求立即生效时不该有 reloaded: %s", recorder.Body.String())
	}
	if len(environment.storedIndex(t)) != 2 {
		t.Fatal("名单应已写入")
	}
}

// 每一种「加不进去」都要能单独区分：命令行据此向人解释问题。
func TestAddRepositoriesReportsEveryFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{
			name:   "地址带查询串",
			body:   map[string]any{"urls": []string{"https://example.com/first.git?ref=main"}},
			status: http.StatusBadRequest,
			code:   "invalid_url",
		},
		{
			name:   "地址里取不出名字",
			body:   map[string]any{"urls": []string{"https://example.com/.git"}},
			status: http.StatusBadRequest,
			code:   "invalid_url",
		},
		{
			name:   "一个地址都没有",
			body:   map[string]any{"urls": []string{}},
			status: http.StatusBadRequest,
			code:   "invalid_url",
		},
		{
			name:   "推导出的名字不合法",
			body:   map[string]any{"urls": []string{"https://example.com/.."}},
			status: http.StatusBadRequest,
			code:   "invalid_name",
		},
		{
			name:   "档位不认识",
			body:   map[string]any{"urls": []string{"https://example.com/first.git"}, "history": "deep"},
			status: http.StatusBadRequest,
			code:   "invalid_history",
		},
		{
			name:   "分支带空白",
			body:   map[string]any{"urls": []string{"https://example.com/first.git"}, "branch": " main "},
			status: http.StatusBadRequest,
			code:   "invalid_branch",
		},
		{
			name:   "与名单已有项重名",
			body:   map[string]any{"urls": []string{"https://example.com/first.git", "https://other.com/example.git"}},
			status: http.StatusConflict,
			code:   "duplicate_name",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			environment := newManagementEnv(t)

			recorder := environment.do(t, http.MethodPost, "/v1/repos", testCase.body)
			requireFailure(t, recorder, testCase.status, testCase.code)

			// 整批原子：磁盘上的名单一个字都没动。
			if stored := environment.storedIndex(t); len(stored) != 1 || stored[0] != "example" {
				t.Fatalf("失败的批次不该改动名单: %v", stored)
			}
		})
	}
}

func TestAddRepositoriesReportsIndexWriteFailure(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)
	// 占住写盘用的临时文件名，让落盘失败；读盘不受影响。
	if err := os.MkdirAll(environment.source.snapshot.IndexPath+".tmp", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	recorder := environment.do(t, http.MethodPost, "/v1/repos", map[string]any{
		"urls": []string{"https://example.com/first.git"},
	})
	requireFailure(t, recorder, http.StatusInternalServerError, "index_write_failed")
}

func TestAddRepositoriesRejectsMalformedBody(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)

	recorder := environment.doRaw(t, http.MethodPost, "/v1/repos", "{urls:")
	requireFailure(t, recorder, http.StatusBadRequest, "invalid_body")
}

type removePayload struct {
	Name     string `json:"name"`
	Removed  bool   `json:"removed"`
	DataKept bool   `json:"dataKept"`
}

// 默认只摘名单：磁盘副本留着，响应里如实说「数据保留」。
func TestRemoveRepositoryKeepsDataByDefault(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)

	recorder := environment.do(t, http.MethodDelete, "/v1/repos/example", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	payload := decodeJSON[removePayload](t, recorder)
	if payload.Name != "example" || !payload.Removed || !payload.DataKept {
		t.Fatalf("payload = %#v", payload)
	}

	if stored := environment.storedIndex(t); len(stored) != 0 {
		t.Fatalf("名单应已摘除: %v", stored)
	}
	if _, err := os.Stat(filepath.Join(environment.source.snapshot.RepositoryDir, "example")); err != nil {
		t.Fatalf("数据应保留: %v", err)
	}
}

func TestRemoveRepositoryDeletesDataWhenAsked(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)

	recorder := environment.do(t, http.MethodDelete, "/v1/repos/example?keepData=false", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	payload := decodeJSON[removePayload](t, recorder)
	if !payload.Removed || payload.DataKept {
		t.Fatalf("payload = %#v", payload)
	}
	if _, err := os.Stat(filepath.Join(environment.source.snapshot.RepositoryDir, "example")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("数据应已删除: %v", err)
	}
}

func TestRemoveRepositoryReportsEveryFailure(t *testing.T) {
	t.Parallel()

	t.Run("不在名单", func(t *testing.T) {
		t.Parallel()

		environment := newManagementEnv(t)

		recorder := environment.do(t, http.MethodDelete, "/v1/repos/ghost", nil)
		requireFailure(t, recorder, http.StatusNotFound, "repo_not_found")
	})

	t.Run("名单写盘失败", func(t *testing.T) {
		t.Parallel()

		environment := newManagementEnv(t)
		if err := os.MkdirAll(environment.source.snapshot.IndexPath+".tmp", 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		recorder := environment.do(t, http.MethodDelete, "/v1/repos/example", nil)
		requireFailure(t, recorder, http.StatusInternalServerError, "index_write_failed")

		if stored := environment.storedIndex(t); len(stored) != 1 {
			t.Fatalf("写盘失败时名单不该变: %v", stored)
		}
	})

	t.Run("参数不合法", func(t *testing.T) {
		t.Parallel()

		environment := newManagementEnv(t)

		recorder := environment.do(t, http.MethodDelete, "/v1/repos/example?keepData=maybe", nil)
		requireFailure(t, recorder, http.StatusBadRequest, "invalid_flag")
	})
}

type unblockPayload struct {
	Name     string `json:"name"`
	Cleared  bool   `json:"cleared"`
	Previous struct {
		Blocked             bool   `json:"blocked"`
		BlockedSize         int64  `json:"blockedSize"`
		RemoteMissing       bool   `json:"remoteMissing"`
		ConsecutiveFailures int    `json:"consecutiveFailures"`
		LastError           string `json:"lastError"`
	} `json:"previous"`
}

func TestUnblockRepositoryClearsMarkers(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)
	if err := environment.states.Put("example", state.RepoState{
		Blocked:             true,
		BlockedSize:         3 << 30,
		RemoteMissing:       true,
		ConsecutiveFailures: 4,
		LastError:           "Repository not found",
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	recorder := environment.do(t, http.MethodPost, "/v1/repos/example/unblock", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	payload := decodeJSON[unblockPayload](t, recorder)
	if payload.Name != "example" || !payload.Cleared {
		t.Fatalf("payload = %#v", payload)
	}
	if !payload.Previous.Blocked || !payload.Previous.RemoteMissing {
		t.Fatalf("previous 应带上变更前的事实: %#v", payload.Previous)
	}
	if payload.Previous.BlockedSize != 3<<30 || payload.Previous.ConsecutiveFailures != 4 {
		t.Fatalf("previous 数字不对: %#v", payload.Previous)
	}
	if payload.Previous.LastError != "Repository not found" {
		t.Fatalf("previous 原因不对: %q", payload.Previous.LastError)
	}

	got := environment.states.Get("example")
	if got.Blocked || got.RemoteMissing || got.ConsecutiveFailures != 0 || got.LastError != "" {
		t.Fatalf("状态应已解除并清零失败记录: %#v", got)
	}
}

// 本来就没被拦是成功：状态码 200、cleared 为假。
func TestUnblockRepositoryWithoutMarkersIsSuccess(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)

	recorder := environment.do(t, http.MethodPost, "/v1/repos/example/unblock", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if decodeJSON[unblockPayload](t, recorder).Cleared {
		t.Fatal("没有标记时 cleared 应为假")
	}
}

// 明确要求保留失败记录时，只解除拦截，不动失败计数。
func TestUnblockRepositoryKeepsFailureRecordWhenAsked(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)
	if err := environment.states.Put("example", state.RepoState{
		Blocked:             true,
		ConsecutiveFailures: 4,
		LastError:           "Repository not found",
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	recorder := environment.do(t, http.MethodPost, "/v1/repos/example/unblock", map[string]any{"clearFailures": false})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	got := environment.states.Get("example")
	if got.Blocked {
		t.Fatal("拦截应已解除")
	}
	if got.ConsecutiveFailures != 4 || got.LastError == "" {
		t.Fatalf("没要求清失败记录时不该动它: %#v", got)
	}
}

func TestUnblockRepositoryReportsEveryFailure(t *testing.T) {
	t.Parallel()

	t.Run("不在名单", func(t *testing.T) {
		t.Parallel()

		environment := newManagementEnv(t)

		recorder := environment.do(t, http.MethodPost, "/v1/repos/ghost/unblock", nil)
		requireFailure(t, recorder, http.StatusNotFound, "repo_not_found")
	})

	t.Run("状态写盘失败", func(t *testing.T) {
		t.Parallel()

		environment := newManagementEnv(t)
		if err := environment.states.Put("example", state.RepoState{Blocked: true}); err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		// 占住写盘用的临时文件名，让落盘失败。
		if err := os.MkdirAll(environment.statePath+".tmp", 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		recorder := environment.do(t, http.MethodPost, "/v1/repos/example/unblock", nil)
		requireFailure(t, recorder, http.StatusInternalServerError, "state_write_failed")
	})

	t.Run("正文不合法", func(t *testing.T) {
		t.Parallel()

		environment := newManagementEnv(t)

		recorder := environment.doRaw(t, http.MethodPost, "/v1/repos/example/unblock", "{clearFailures:")
		requireFailure(t, recorder, http.StatusBadRequest, "invalid_body")
	})
}

type compactPayload struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	Branch string `json:"branch"`
	Before struct {
		Shallow  bool  `json:"shallow"`
		GitBytes int64 `json:"gitBytes"`
	} `json:"before"`
	After struct {
		Shallow  bool  `json:"shallow"`
		GitBytes int64 `json:"gitBytes"`
	} `json:"after"`
	DurationMs *int64 `json:"durationMs"`
}

// 四种「成功」的结论都要能表达出来：两种做了什么，两种什么都不做。
func TestCompactRepositoryReportsOutcomes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		environment *env
		target      string
		action      string
	}{
		{
			name:        "已转换",
			environment: newEnv(t, envOptions{Git: &stubGit{shallow: false, shallowAfter: true, size: 2048, sizeAfter: 512}}),
			target:      "/v1/repos/example/compact",
			action:      "compacted",
		},
		{
			name:        "预演认为会转换",
			environment: newEnv(t, envOptions{Git: &stubGit{shallow: false, size: 2048}}),
			target:      "/v1/repos/example/compact?dryRun=1",
			action:      "would_compact",
		},
		{
			name:        "本来就是浅克隆",
			environment: newEnv(t, envOptions{Git: &stubGit{shallow: true, size: 512}}),
			target:      "/v1/repos/example/compact",
			action:      "already_shallow",
		},
		{
			name: "名单要求完整历史",
			environment: newEnv(t, envOptions{
				Live: newLive(t, []string{testAPIKey}, []repoindex.Repository{
					{Name: "example", URL: "https://example.com/example.git", History: repoindex.HistoryFull},
				}, "example"),
				Git: &stubGit{shallow: false, size: 2048},
			}),
			target: "/v1/repos/example/compact",
			action: "skipped_full_history",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := testCase.environment.do(t, http.MethodPost, testCase.target, nil)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}

			payload := decodeJSON[compactPayload](t, recorder)
			if payload.Name != "example" || payload.Action != testCase.action {
				t.Fatalf("payload = %#v", payload)
			}
			if payload.Branch != "main" {
				t.Fatalf("branch = %q", payload.Branch)
			}
			if payload.DurationMs == nil {
				t.Fatal("缺少耗时")
			}
			if payload.Before.GitBytes == 0 {
				t.Fatalf("before = %#v", payload.Before)
			}
		})
	}
}

// 预演与跳过的情形都不许动仓库。
func TestCompactRepositoryLeavesRepositoryAloneWhenNotConverting(t *testing.T) {
	t.Parallel()

	environment := newEnv(t, envOptions{Git: &stubGit{shallow: false}})

	recorder := environment.do(t, http.MethodPost, "/v1/repos/example/compact?dryRun=true", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if len(environment.git.compacted) != 0 {
		t.Fatalf("预演不该动仓库: %#v", environment.git.compacted)
	}
}

func TestCompactRepositoryReportsEveryFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		environment *env
		target      string
		status      int
		code        string
	}{
		{
			name:        "不在名单",
			environment: newManagementEnv(t),
			target:      "/v1/repos/ghost/compact",
			status:      http.StatusNotFound,
			code:        "repo_not_found",
		},
		{
			name: "磁盘上没有副本",
			environment: newEnv(t, envOptions{
				Live: newLive(t, []string{testAPIKey}, []repoindex.Repository{
					{Name: "example", URL: "https://example.com/example.git"},
				}),
			}),
			target: "/v1/repos/example/compact",
			status: http.StatusNotFound,
			code:   "repo_not_found",
		},
		{
			name:        "同步正忙",
			environment: newEnv(t, envOptions{Exclusive: func(func()) bool { return false }}),
			target:      "/v1/repos/example/compact",
			status:      http.StatusConflict,
			code:        "sync_busy",
		},
		{
			name:        "游离头指针",
			environment: newEnv(t, envOptions{Git: &stubGit{compactErr: gitops.ErrDetachedHead}}),
			target:      "/v1/repos/example/compact",
			status:      http.StatusBadRequest,
			code:        "detached_head",
		},
		{
			name:        "远端不可达",
			environment: newEnv(t, envOptions{Git: &stubGit{compactErr: fmt.Errorf("%w: fetch failed", gitops.ErrRemoteUnavailable)}}),
			target:      "/v1/repos/example/compact",
			status:      http.StatusFailedDependency,
			code:        "remote_unreachable",
		},
		{
			name:        "版本管理命令失败",
			environment: newEnv(t, envOptions{Git: &stubGit{isShallowErr: errors.New("git rev-parse: boom")}}),
			target:      "/v1/repos/example/compact",
			status:      http.StatusInternalServerError,
			code:        "git_failed",
		},
		{
			name:        "预演参数不合法",
			environment: newManagementEnv(t),
			target:      "/v1/repos/example/compact?dryRun=maybe",
			status:      http.StatusBadRequest,
			code:        "invalid_flag",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := testCase.environment.do(t, http.MethodPost, testCase.target, nil)
			requireFailure(t, recorder, testCase.status, testCase.code)
		})
	}
}

// 失败结论的映射只在一处定义：同一种底层失败在哪个能力里都给出同一个结论。
func TestManagementFailureMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"地址不合法", fmt.Errorf("%w: bad", repoindex.ErrInvalidURL), http.StatusBadRequest, "invalid_url"},
		{"一个地址都没有", ops.ErrNoURLs, http.StatusBadRequest, "invalid_url"},
		{"名字不合法", repoindex.ErrInvalidName, http.StatusBadRequest, "invalid_name"},
		{"分支不合法", repoindex.ErrInvalidBranch, http.StatusBadRequest, "invalid_branch"},
		{"档位不合法", repoindex.ErrInvalidHistory, http.StatusBadRequest, "invalid_history"},
		{"重名", repoindex.ErrDuplicateName, http.StatusConflict, "duplicate_name"},
		{"不在名单", ops.ErrRepositoryNotFound, http.StatusNotFound, "repo_not_found"},
		{"不在磁盘", ops.ErrRepositoryNotOnDisk, http.StatusNotFound, "repo_not_found"},
		{"同步正忙", ops.ErrBusy, http.StatusConflict, "sync_busy"},
		{"删数据失败", ops.ErrDataRemoval, http.StatusInternalServerError, "remove_failed"},
		{"游离头指针", gitops.ErrDetachedHead, http.StatusBadRequest, "detached_head"},
		{"远端不可达", gitops.ErrRemoteUnavailable, http.StatusFailedDependency, "remote_unreachable"},
		{"清理未确认", cache.ErrNotConfirmed, http.StatusBadRequest, "confirm_required"},
		{"缓存正忙", cache.ErrBusy, http.StatusConflict, "cache_busy"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			status, code, message, ok := classifyManagement(testCase.err)
			if !ok {
				t.Fatalf("认不出 %v", testCase.err)
			}
			if status != testCase.status || code != testCase.code {
				t.Fatalf("classifyManagement() = %d, %q, want %d, %q", status, code, testCase.status, testCase.code)
			}
			if message == "" {
				t.Fatal("缺少给人看的原因")
			}
		})
	}

	// 认不出来的失败交给调用点决定说哪一件事，因为同一种底层失败在不同
	// 能力里的含义不同（写名单 / 写状态 / 跑版本管理命令）。
	if _, _, _, ok := classifyManagement(errors.New("boom")); ok {
		t.Fatal("不认识的失败不该被硬塞进某个结论")
	}
}

// 增补只发生在名单总览上：单仓详情的字段与改动前完全一致。
func TestShowRepositoryKeepsItsOwnShape(t *testing.T) {
	t.Parallel()

	environment := newManagementEnv(t)

	recorder := environment.do(t, http.MethodGet, "/v1/repos/example", nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	for _, field := range []string{"url", "history", "effectiveHistory", "onDisk", "measuredBytes"} {
		if strings.Contains(recorder.Body.String(), `"`+field+`"`) {
			t.Fatalf("单仓详情不该出现名单总览的新增字段 %q: %s", field, recorder.Body.String())
		}
	}
	if !strings.Contains(recorder.Body.String(), `"revision":"`+testRevision+`"`) {
		t.Fatalf("原有字段缺失: %s", recorder.Body.String())
	}
}
