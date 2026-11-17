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
}

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
	r.store.Begin(startedAt)
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
	var target repoindex.Repository
	found := false
	for _, repo := range r.repos {
		if repo.Name == name {
			target = repo
			found = true
			break
		}
	}
	if !found {
		return gitops.Result{}, ErrRepoNotManaged
	}

	select {
	case r.gate <- struct{}{}:
		defer func() { <-r.gate }()
	default:
		return gitops.Result{}, ErrSyncBusy
	}

	result := r.syncOne(ctx, target)
	logResult(result)
	r.store.RecordSingle(result)
	return result, nil
}

// syncOne 处理单个仓库：先看是否被体积保护拦下，再同步，最后守卫体积。
func (r *Runner) syncOne(ctx context.Context, repo repoindex.Repository) gitops.Result {
	repoPath := filepath.Join(r.cfg.RepositoryDir(), repo.Name)

	if blocked := r.state.Get(repo.Name); blocked.Blocked {
		return gitops.Result{
			Name:       repo.Name,
			URL:        repo.URL,
			Path:       repoPath,
			Action:     "blocked",
			Error:      fmt.Sprintf("体积已达 %s，超过上限 %d MB，已停止自动同步", humanMB(blocked.BlockedSize), r.cfg.SizeLimitMB),
			StartedAt:  time.Now(),
			FinishedAt: time.Now(),
		}
	}

	history := repo.EffectiveHistory(r.cfg.DefaultHistory)
	result := r.git.Sync(ctx, gitops.Repository{
		Name:    repo.Name,
		URL:     repo.URL,
		Branch:  repo.Branch,
		Shallow: history != repoindex.HistoryFull,
	})
	if result.Error != "" {
		return result
	}

	r.applySizeGuard(ctx, repo, repoPath, &result)
	return result
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
