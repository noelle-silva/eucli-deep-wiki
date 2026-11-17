package runner

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"eucli-deep-wiki/internal/config"
	"eucli-deep-wiki/internal/gitops"
	"eucli-deep-wiki/internal/repoindex"
	"eucli-deep-wiki/internal/status"
)

type GitClient interface {
	Sync(ctx context.Context, repo gitops.Repository) gitops.Result
}

type Runner struct {
	cfg   *config.Config
	repos []repoindex.Repository
	git   GitClient
	store *status.Store
	gate  chan struct{}
}

func New(cfg *config.Config, repos []repoindex.Repository, git GitClient, store *status.Store) *Runner {
	return &Runner{
		cfg:   cfg,
		repos: append([]repoindex.Repository(nil), repos...),
		git:   git,
		store: store,
		gate:  make(chan struct{}, 1),
	}
}

func (r *Runner) Loop(ctx context.Context) {
	if err := r.SyncOnce(ctx); err != nil {
		log.Printf("initial sync completed with errors: %v", err)
	}

	ticker := time.NewTicker(r.cfg.SyncIntervalDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.SyncOnce(ctx); err != nil {
				log.Printf("scheduled sync completed with errors: %v", err)
			}
		}
	}
}

func (r *Runner) SyncOnce(ctx context.Context) error {
	select {
	case r.gate <- struct{}{}:
		defer func() { <-r.gate }()
	default:
		return errors.New("sync already running")
	}

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
				result := r.git.Sync(ctx, gitops.Repository{
					Name:   repo.Name,
					URL:    repo.URL,
					Branch: repo.Branch,
				})
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

func logResult(result gitops.Result) {
	if result.Error != "" {
		log.Printf("repo=%s action=%s error=%s", result.Name, result.Action, result.Error)
		return
	}
	log.Printf("repo=%s action=%s branch=%s duration=%s", result.Name, result.Action, result.Branch, result.Duration)
}
