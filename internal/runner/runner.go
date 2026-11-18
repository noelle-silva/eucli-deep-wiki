package runner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"eucli-deep-wiki/internal/config"
	"eucli-deep-wiki/internal/gitops"
	"eucli-deep-wiki/internal/repoindex"
	"eucli-deep-wiki/internal/state"
	"eucli-deep-wiki/internal/status"
)

// GitClient 是同步器依赖的 git 能力。
type GitClient interface {
	Sync(ctx context.Context, repo gitops.Repository) gitops.Result
	GarbageCollect(ctx context.Context, path string) error
	GitDirectorySize(path string) (int64, error)
	ProbeRemote(ctx context.Context, url string) error
}

// 远端连续失败到这一次数后，判定仓库的远端已经消失。
const remoteMissingThreshold = 3

type Runner struct {
	cfg   *config.Config
	repos []repoindex.Repository
	git   GitClient
	store *status.Store
	state *state.Store
	gate  chan struct{}
}

func New(cfg *config.Config, repos []repoindex.Repository, git GitClient, store *status.Store, guard *state.Store) *Runner {
	return &Runner{
		cfg:   cfg,
		repos: append([]repoindex.Repository(nil), repos...),
		git:   git,
		store: store,
		state: guard,
		gate:  make(chan struct{}, 1),
	}
}

func (r *Runner) Loop(ctx context.Context) {
	if err := r.SyncOnce(ctx); err != nil {
		log.Printf("initial sync completed with errors: %v", err)
	}

	ticker := time.NewTicker(r.cfg.SyncIntervalDuration)
	defer ticker.Stop()
	r.store.SetNextRun(time.Now().Add(r.cfg.SyncIntervalDuration))

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.store.SetNextRun(time.Now().Add(r.cfg.SyncIntervalDuration))
			if err := r.SyncOnce(ctx); err != nil {
				log.Printf("scheduled sync completed with errors: %v", err)
			}
		}
	}
}

func (r *Runner) SyncOnce(ctx context.Context) error {
	if !r.acquire() {
		return ErrSyncBusy
	}
	defer r.release()

	return r.runCycle(ctx)
}

// StartSyncAll 在后台跑一轮全量同步；已有任务在跑时返回 ErrSyncBusy。
func (r *Runner) StartSyncAll(ctx context.Context) error {
	if !r.acquire() {
		return ErrSyncBusy
	}

	go func() {
		defer r.release()
		if err := r.runCycle(ctx); err != nil {
			log.Printf("manual sync completed with errors: %v", err)
		}
	}()
	return nil
}

func (r *Runner) acquire() bool {
	select {
	case r.gate <- struct{}{}:
		return true
	default:
		return false
	}
}

func (r *Runner) release() {
	<-r.gate
}

func (r *Runner) runCycle(ctx context.Context) error {
	startedAt := time.Now()
	r.reloadState()
	r.store.Begin(startedAt, len(r.repos))
	defer func() { r.store.Finish(time.Now()) }()

	log.Printf("sync started for %d repositories", len(r.repos))
	results := r.syncAll(ctx)
	log.Printf("sync finished for %d repositories", len(results))

	var failures []string
	for _, result := range results {
		if result.Error != "" {
			failures = append(failures, fmt.Sprintf("%s: %s", result.Name, result.Error))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d repository sync(s) failed: %s", len(failures), strings.Join(failures, "; "))
	}
	return nil
}

func (r *Runner) syncAll(ctx context.Context) []gitops.Result {
	workers := r.cfg.MaxParallelSyncs
	if workers > len(r.repos) {
		workers = len(r.repos)
	}

	jobs := make(chan repoindex.Repository)
	results := make(chan gitops.Result)
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range jobs {
				result := r.syncOne(ctx, repo)
				logResult(result)
				r.store.AddResult(result)
				results <- result
			}
		}()
	}

	go func() {
		for _, repo := range r.repos {
			select {
			case <-ctx.Done():
				close(jobs)
				wg.Wait()
				close(results)
				return
			case jobs <- repo:
			}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	var collected []gitops.Result
	for result := range results {
		collected = append(collected, result)
	}
	return collected
}

// 同步触发相关的错误。
var (
	ErrRepoNotManaged = errors.New("repository is not managed")
	ErrSyncBusy       = errors.New("sync already running")
)

// SyncOne 立刻同步指定仓库，供按需拉取使用。
func (r *Runner) SyncOne(ctx context.Context, name string) (gitops.Result, error) {
	target, found := r.find(name)
	if !found {
		return gitops.Result{}, ErrRepoNotManaged
	}

	select {
	case r.gate <- struct{}{}:
		defer func() { <-r.gate }()
	default:
		return gitops.Result{}, ErrSyncBusy
	}

	r.reloadState()
	result := r.syncOne(ctx, target)
	logResult(result)
	r.store.RecordSingle(result)
	return result, nil
}

// reloadState 重新读取状态文件。
//
// repo unblock 由命令行直接改盘，服务这边只有重新加载才能让「已解除」生效。
func (r *Runner) reloadState() {
	if err := r.state.Reload(); err != nil {
		log.Printf("reload sync state: %v", err)
	}
}

// RemoteStatus 是一次远端可达性探测的结果。
type RemoteStatus struct {
	Repo      string `json:"repo"`
	Reachable bool   `json:"reachable"`
	Error     string `json:"error,omitempty"`
}

// CheckRemote 只探测远端是否可达，不改动本地仓库，也不改动同步状态。
func (r *Runner) CheckRemote(ctx context.Context, name string) (RemoteStatus, error) {
	target, found := r.find(name)
	if !found {
		return RemoteStatus{}, ErrRepoNotManaged
	}

	status := RemoteStatus{Repo: target.Name}
	if err := r.git.ProbeRemote(ctx, target.URL); err != nil {
		status.Error = err.Error()
		return status, nil
	}
	status.Reachable = true
	return status, nil
}

// find 按名字在索引里查仓库。
func (r *Runner) find(name string) (repoindex.Repository, bool) {
	for _, repo := range r.repos {
		if repo.Name == name {
			return repo, true
		}
	}
	return repoindex.Repository{}, false
}

// syncOne 处理单个仓库：先看是否被拦下，再同步；失败计入状态，成功清零并守卫体积。
func (r *Runner) syncOne(ctx context.Context, repo repoindex.Repository) gitops.Result {
	repoPath := filepath.Join(r.cfg.RepositoryDir(), repo.Name)
	current := r.state.Get(repo.Name)

	if current.Blocked {
		return skipResult(repo, repoPath, "blocked", fmt.Sprintf(
			"体积已达 %s，超过上限 %d MB，已停止自动同步",
			humanMB(current.BlockedSize), r.cfg.SizeLimitMB,
		))
	}
	if current.RemoteMissing {
		return skipResult(repo, repoPath, "missing", fmt.Sprintf(
			"远端仓库不可用（%s 起连续 %d 次失败后判定），已跳过同步；确认远端恢复后执行 repo unblock 解除",
			current.RemoteMissingAt.Format(time.RFC3339), current.ConsecutiveFailures,
		))
	}

	history := repo.EffectiveHistory(r.cfg.DefaultHistory)
	result := r.git.Sync(ctx, gitops.Repository{
		Name:    repo.Name,
		URL:     repo.URL,
		Branch:  repo.Branch,
		Shallow: history != repoindex.HistoryFull,
	})
	if result.Error != "" {
		r.recordFailure(repo, result)
		return result
	}

	r.recordSuccess(repo)
	r.applySizeGuard(ctx, repo, repoPath, &result)
	return result
}

// skipResult 构造一个「本轮没有真正同步」的结果。
func skipResult(repo repoindex.Repository, repoPath string, action string, reason string) gitops.Result {
	now := time.Now()
	return gitops.Result{
		Name:       repo.Name,
		URL:        repo.URL,
		Path:       repoPath,
		Action:     action,
		Error:      reason,
		StartedAt:  now,
		FinishedAt: now,
	}
}

// recordFailure 累加连续失败次数并记下最近一次错误；
// 远端不可用的失败连续攒够次数后，判定仓库的远端已经消失。
func (r *Runner) recordFailure(repo repoindex.Repository, result gitops.Result) {
	err := r.state.Update(repo.Name, func(current state.RepoState) state.RepoState {
		current.ConsecutiveFailures++
		current.LastError = result.Error
		if result.RemoteUnavailable && current.ConsecutiveFailures >= remoteMissingThreshold {
			current.RemoteMissing = true
			current.RemoteMissingAt = time.Now()
		}
		return current
	})
	if err != nil {
		log.Printf("repo=%s persist failure state: %v", repo.Name, err)
	}
}

// recordSuccess 清零连续失败次数并解除远端消失标记。
// 没有可清的状态时不写盘，避免每轮同步都为每个仓库重写状态文件。
func (r *Runner) recordSuccess(repo repoindex.Repository) {
	current := r.state.Get(repo.Name)
	if current.ConsecutiveFailures == 0 && current.LastError == "" && !current.RemoteMissing && current.RemoteMissingAt.IsZero() {
		return
	}

	err := r.state.Update(repo.Name, func(current state.RepoState) state.RepoState {
		current.ConsecutiveFailures = 0
		current.LastError = ""
		current.RemoteMissing = false
		current.RemoteMissingAt = time.Time{}
		return current
	})
	if err != nil {
		log.Printf("repo=%s persist success state: %v", repo.Name, err)
	}
}

// applySizeGuard 在同步完成后检查 .git 体积：
// 超过上限就停机保护，超过压缩线且增长足够则压缩一次并记录基准。
func (r *Runner) applySizeGuard(ctx context.Context, repo repoindex.Repository, repoPath string, result *gitops.Result) {
	size, err := r.git.GitDirectorySize(repoPath)
	if err != nil {
		log.Printf("repo=%s measure size: %v", repo.Name, err)
		return
	}

	limit := int64(r.cfg.SizeLimitMB) << 20
	if size >= limit {
		if err := r.state.Update(repo.Name, func(current state.RepoState) state.RepoState {
			current.Blocked = true
			current.BlockedAt = time.Now()
			current.BlockedSize = size
			return current
		}); err != nil {
			log.Printf("repo=%s persist block state: %v", repo.Name, err)
		}
		result.Action = "blocked"
		result.Error = fmt.Sprintf("体积已达 %s，超过上限 %d MB，已停止自动同步", humanMB(size), r.cfg.SizeLimitMB)
		return
	}

	threshold := int64(r.cfg.GCThresholdMB) << 20
	growth := int64(r.cfg.GCGrowthMB) << 20
	current := r.state.Get(repo.Name)
	if size < threshold || size-current.LastGCSize < growth {
		return
	}

	log.Printf("repo=%s gc start size=%s", repo.Name, humanMB(size))
	if err := r.git.GarbageCollect(ctx, repoPath); err != nil {
		log.Printf("repo=%s gc failed: %v", repo.Name, err)
		return
	}

	after, err := r.git.GitDirectorySize(repoPath)
	if err != nil {
		after = size
	}
	if err := r.state.Update(repo.Name, func(current state.RepoState) state.RepoState {
		current.LastGCSize = after
		current.LastGCAt = time.Now()
		return current
	}); err != nil {
		log.Printf("repo=%s persist gc state: %v", repo.Name, err)
	}
	log.Printf("repo=%s gc done before=%s after=%s", repo.Name, humanMB(size), humanMB(after))
}

func humanMB(size int64) string {
	return fmt.Sprintf("%.0f MB", float64(size)/(1<<20))
}

func logResult(result gitops.Result) {
	if result.Error != "" {
		log.Printf("repo=%s action=%s error=%s", result.Name, result.Action, result.Error)
		return
	}
	log.Printf("repo=%s action=%s branch=%s duration=%s", result.Name, result.Action, result.Branch, result.Duration)
}
