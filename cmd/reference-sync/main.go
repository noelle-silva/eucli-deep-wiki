package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"eucli-deep-wiki/internal/config"
	"eucli-deep-wiki/internal/gitops"
	"eucli-deep-wiki/internal/httpapi"
	"eucli-deep-wiki/internal/repoindex"
	"eucli-deep-wiki/internal/runner"
	"eucli-deep-wiki/internal/status"
	"eucli-deep-wiki/internal/vault"
)

func main() {
	configPath := flag.String("config", "configs/config.json", "path to the server config")
	httpAddress := flag.String("http-address", "", "override the status server listen address")
	once := flag.Bool("once", false, "run one sync and exit")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *httpAddress != "" {
		if err := cfg.SetHTTPAddress(*httpAddress); err != nil {
			log.Fatalf("set http address: %v", err)
		}
	}

	index, err := repoindex.Load(cfg.RepositoryIndexPath())
	if err != nil {
		log.Fatalf("load repository index: %v", err)
	}

	names := make([]string, 0, len(index.Repositories))
	for _, repo := range index.Repositories {
		names = append(names, repo.Name)
	}
	repoVault, err := vault.New(cfg.RepositoryDir(), names, vault.DefaultLimits())
	if err != nil {
		log.Fatalf("build read vault: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := status.NewStore()
	git := gitops.New(cfg.RepositoryDir(), cfg.GitCommandTimeoutDuration)
	run := runner.New(cfg, index.Repositories, git, store)

	if *once {
		if err := run.SyncOnce(ctx); err != nil {
			log.Fatalf("sync repositories: %v", err)
		}
		return
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           httpapi.NewRouter(store, repoVault),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("status server listening on http://%s", cfg.HTTPAddress)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("status server stopped: %v", err)
			stop()
		}
	}()

	go run.Loop(ctx)

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("status server shutdown: %v", err)
	}
}
