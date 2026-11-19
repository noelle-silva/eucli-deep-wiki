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

	"eucli-deep-wiki-server/internal/gitops"
	"eucli-deep-wiki-server/internal/live"
	"eucli-deep-wiki-server/internal/repoindex"
	"eucli-deep-wiki-server/internal/state"
	"eucli-deep-wiki-server/internal/status"
)

// GitClient 是同步器依赖的 git 能力。
type GitClient interface {
	Sync(ctx context.Context, repo gitops.Repository) gitops.Result
	GarbageCollect(ctx context.Context, path string) error
	GitDirectorySize(path string) (int64, error)
	ProbeRemote(ctx context.Context, url string) error
}

// Source 提供运行期可替换的配置与仓库清单。
//
// 每轮同步只取一次快照：整轮都用同一份配置与清单，重载不会把一轮同步
// 撕成两半；下一轮自然用上新的一份。
type Source interface {
	// EnsureFresh 在配置文件或索引变化时重新加载。
	EnsureFresh()
	// Snapshot 返回当前生效的完整视图。
	Snapshot() live.Snapshot
	// Changed 返回一个在生效内容变化后关闭的通道。
	Changed() <-chan struct{}
}

// 远端连续失败到这一次数后，判定仓库的远端已经消失。
const remoteMissingThreshold = 3

type Runner struct {
	source Source
	git    GitClient
	store  *status.Store
	state  *state.Store
	gate   chan struct{}
}

func New(source Source, git GitClient, store *status.Store, guard *state.Store) *Runner {
	return &Runner{
		source: source,
		git:    git,
		store:  store,
		state:  guard,
		gate:   make(chan struct{}, 1),
	}
}

// Loop 先同步一轮，之后按当前同步周期循环。
//
// 周期来自运行期配置：配置改了会唤醒这里，按新周期重新计时；
// 与周期无关的变更不会打断已经排好的下一轮。
func (r *Runner) Loop(ctx context.Context) {
	if err := r.SyncOnce(ctx); err != nil {
		log.Printf("initial sync completed with errors: %v", err)
	}

	for {
		if !r.waitNextRun(ctx) {
			return
		}
		if err := r.SyncOnce(ctx); err != nil {
			log.Printf("scheduled sync completed with errors: %v", err)
		}
	}
}

// waitNextRun 等到下一轮同步的时间点；周期变化时按新周期重新计时。
// 返回 false 表示收到退出信号。
func (r *Runner) waitNextRun(ctx context.Context) bool {
	interval := r.interval()
	deadline := time.Now().Add(interval)
	r.store.SetNextRun(deadline)

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return true
		}

		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
			return true
		case <-r.source.Changed():
			timer.Stop()

			updated := r.interval()
			if updated == interval {
				// 与周期无关的变更：保持原定时间点，不打断已经排好的下一轮。
				continue
			}
			interval = updated
			deadline = time.Now().Add(interval)
			r.store.SetNextRun(deadline)
		}
	}
}

// interval 返回当前生效的同步周期。
func (r *Runner) interval() time.Duration {
	return r.source.Snapshot().Config.SyncIntervalDuration
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

// Exclusive 在同步闸门内执行一次操作。
//
// 正在跑的那一轮同步先跑完，之后不会再有同步开始，直到 action 返回：
// 破坏性操作（例如就地瘦身）期间，同步既不会插进来，也不会被它打断。
// 闸门已被占用时立即返回 false，不排队等待。
func (r *Runner) Exclusive(action func()) bool {
	if !r.acquire() {
		return false
	}
	defer r.release()

	action()
	return true
}

func (r *Runner) runCycle(ctx context.Context) error {
	// 每轮开始前先让配置与索引跟上磁盘：新增的仓库、新的阈值与周期都在这一轮生效。
	r.source.EnsureFresh()
	snapshot := r.source.Snapshot()

	startedAt := time.Now()
	r.reloadState()
	r.store.Begin(startedAt, len(snapshot.Repositories))
	defer func() { r.store.Finish(time.Now()) }()

	log.Printf("sync started for %d repositories", len(snapshot.Repositories))
	results := r.syncAll(ctx, snapshot)
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

func (r *Runner) syncAll(ctx context.Context, snapshot live.Snapshot) []gitops.Result {
	repositories := snapshot.Repositories

	workers := snapshot.Config.MaxParallelSyncs
	if workers > len(repositories) {
		workers = len(repositories)
	}

	jobs := make(chan repoindex.Repository)
	results := make(chan gitops.Result)
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range jobs {
				result := r.syncOne(ctx, snapshot, repo)
				logResult(result)
				r.store.AddResult(result)
				results <- result
			}
		}()
	}

	go func() {
		for _, repo := range repositories {
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
	// 按需同步同样先跟上磁盘：刚加进索引的仓库可以直接同步，不用等下一轮。
	r.source.EnsureFresh()
	snapshot := r.source.Snapshot()

	target, found := find(snapshot.Repositories, name)
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
	result := r.syncOne(ctx, snapshot, target)
	logResult(result)
	r.store.RecordSingle(result)
	return result, nil
}

// reloadState 重新读取状态文件。
//
// 状态文件也可能被本进程之外的操作改动（人工修盘、另一份程序），
// 只有重新加载才能让这些改动生效。
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
	r.source.EnsureFresh()
	snapshot := r.source.Snapshot()

	target, found := find(snapshot.Repositories, name)
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

// find 按名字在清单里查仓库。
func find(repositories []repoindex.Repository, name string) (repoindex.Repository, bool) {
	for _, repo := range repositories {
		if repo.Name == name {
			return repo, true
		}
	}
	return repoindex.Repository{}, false
}

// syncOne 处理单个仓库：先看是否被拦下，再同步；失败计入状态，成功清零并守卫体积。
func (r *Runner) syncOne(ctx context.Context, snapshot live.Snapshot, repo repoindex.Repository) gitops.Result {
	cfg := snapshot.Config
	repoPath := filepath.Join(snapshot.RepositoryDir, repo.Name)
	current := r.state.Get(repo.Name)

	if current.Blocked {
		return skipResult(repo, repoPath, "blocked", fmt.Sprintf(
			"体积已达 %s，超过上限 %d MB，已停止自动同步",
			humanMB(current.BlockedSize), cfg.SizeLimitMB,
		))
	}
	if current.RemoteMissing {
		return skipResult(repo, repoPath, "missing", fmt.Sprintf(
			"远端仓库不可用（%s 起连续 %d 次失败后判定），已跳过同步；确认远端恢复后执行 repo unblock 解除",
			current.RemoteMissingAt.Format(time.RFC3339), current.ConsecutiveFailures,
		))
	}

	history := repo.EffectiveHistory(cfg.DefaultHistory)
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

	r.settle(ctx, sizeThresholds{
		limitMB:       cfg.SizeLimitMB,
		gcThresholdMB: cfg.GCThresholdMB,
		gcGrowthMB:    cfg.GCGrowthMB,
	}, repo, repoPath, &result)
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

// sizeThresholds 是体积守卫的三个阈值，全部取自本轮配置快照。
type sizeThresholds struct {
	limitMB       int
	gcThresholdMB int
	gcGrowthMB    int
}

// settle 是同步成功后的收尾：测量一次体积，记录成功事实，再执行体积守卫。
//
// 体积只测一次，记录与守卫共用同一个测量值；测量失败只影响守卫，
// 「何时同步成功」这件事与体积无关，照常记下。
func (r *Runner) settle(
	ctx context.Context,
	thresholds sizeThresholds,
	repo repoindex.Repository,
	repoPath string,
	result *gitops.Result,
) {
	size, measureErr := r.git.GitDirectorySize(repoPath)
	if measureErr != nil {
		log.Printf("repo=%s measure size: %v", repo.Name, measureErr)
	}

	r.recordSuccess(repo, size, result.FinishedAt)
	if measureErr != nil {
		return
	}
	r.applySizeGuard(ctx, thresholds, repo, repoPath, size, result)
}

// recordSuccess 记录一次成功的同步：清零失败痕迹，并写下这次同步的时间与体积。
//
// 这两个事实是 repos 首屏的信息来源，因此每次成功都要写；
// 体积测量失败时保留上一次的记录，不用 0 覆盖掉真实值。
func (r *Runner) recordSuccess(repo repoindex.Repository, size int64, at time.Time) {
	err := r.state.Update(repo.Name, func(current state.RepoState) state.RepoState {
		current.ConsecutiveFailures = 0
		current.LastError = ""
		current.RemoteMissing = false
		current.RemoteMissingAt = time.Time{}
		current.LastSyncAt = at
		if size > 0 {
			current.GitBytes = size
		}
		return current
	})
	if err != nil {
		log.Printf("repo=%s persist success state: %v", repo.Name, err)
	}
}

// applySizeGuard 在同步完成后按体积决定是否停机保护或压缩一次。
//
// 三个阈值都来自本轮快照，因此改配置里的体积阈值不需要重启。
func (r *Runner) applySizeGuard(
	ctx context.Context,
	thresholds sizeThresholds,
	repo repoindex.Repository,
	repoPath string,
	size int64,
	result *gitops.Result,
) {
	limit := int64(thresholds.limitMB) << 20
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
		result.Error = fmt.Sprintf("体积已达 %s，超过上限 %d MB，已停止自动同步", humanMB(size), thresholds.limitMB)
		return
	}

	threshold := int64(thresholds.gcThresholdMB) << 20
	growth := int64(thresholds.gcGrowthMB) << 20
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
		current.GitBytes = after
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
