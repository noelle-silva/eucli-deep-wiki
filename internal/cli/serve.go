package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"eucli-deep-wiki/internal/config"
	"eucli-deep-wiki/internal/gitops"
	"eucli-deep-wiki/internal/httpapi"
	"eucli-deep-wiki/internal/repoindex"
	"eucli-deep-wiki/internal/runner"
	"eucli-deep-wiki/internal/state"
	"eucli-deep-wiki/internal/status"
	"eucli-deep-wiki/internal/vault"
)

// runServe 启动同步与查询服务。
func runServe(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw "+flags.Name()+" [参数]")

	serviceConfig := flags.String("service-config", defaultServiceConfig(), "服务配置文件路径")
	httpAddress := flags.String("http-address", "", "覆盖监听地址")
	once := flags.Bool("once", false, "只同步一轮后退出")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	cfg, err := config.Load(*serviceConfig)
	if err != nil {
		log.Printf("load config: %v", err)
		return 1
	}
	if *httpAddress != "" {
		if err := cfg.SetHTTPAddress(*httpAddress); err != nil {
			log.Printf("set http address: %v", err)
			return 1
		}
	}

	index, err := repoindex.Load(cfg.RepositoryIndexPath())
	if err != nil {
		log.Printf("load repository index: %v", err)
		return 1
	}

	names := make([]string, 0, len(index.Repositories))
	for _, repository := range index.Repositories {
		names = append(names, repository.Name)
	}
	repoVault, err := vault.New(cfg.RepositoryDir(), names, vault.DefaultLimits())
	if err != nil {
		log.Printf("build read vault: %v", err)
		return 1
	}

	auth, err := httpapi.NewAuthenticator(cfg.APIKeys)
	if err != nil {
		log.Printf("build authenticator: %v", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	guard, err := state.Open(filepath.Join(cfg.DataRoot, state.FileName))
	if err != nil {
		log.Printf("load sync state: %v", err)
		return 1
	}

	store := status.NewStore()
	git := gitops.New(cfg.RepositoryDir(), cfg.GitCommandTimeoutDuration)
	syncer := runner.New(cfg, index.Repositories, git, store, guard)

	if *once {
		if err := syncer.SyncOnce(ctx); err != nil {
			log.Printf("sync repositories: %v", err)
			return 1
		}
		return 0
	}

	server := &http.Server{
		Addr: cfg.HTTPAddress,
		Handler: httpapi.NewRouter(httpapi.Options{
			Store:      store,
			Vault:      repoVault,
			Auth:       auth,
			Sync:       syncer,
			CacheDir:   filepath.Join(cfg.DataRoot, "cache"),
			GitTimeout: cfg.GitCommandTimeoutDuration,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("server listening on http://%s", cfg.HTTPAddress)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server stopped: %v", err)
			stop()
		}
	}()

	go syncer.Loop(ctx)

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown: %v", err)
	}
	return 0
}
