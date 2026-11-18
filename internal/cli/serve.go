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

	"eucli-deep-wiki/internal/gitops"
	"eucli-deep-wiki/internal/httpapi"
	"eucli-deep-wiki/internal/live"
	"eucli-deep-wiki/internal/runner"
	"eucli-deep-wiki/internal/state"
	"eucli-deep-wiki/internal/status"
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

	// 运行期状态从这里开始负责配置文件与索引的生命周期：
	// 首次加载失败直接退出，之后的变化由重载接手。
	source, err := live.Open(live.Options{ConfigPath: *serviceConfig, HTTPAddress: *httpAddress})
	if err != nil {
		log.Printf("load config: %v", err)
		return 1
	}
	snapshot := source.Snapshot()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	guard, err := state.Open(filepath.Join(snapshot.Config.DataRoot, state.FileName))
	if err != nil {
		log.Printf("load sync state: %v", err)
		return 1
	}

	store := status.NewStore()
	git := gitops.New(snapshot.RepositoryDir, func() time.Duration {
		return source.Snapshot().Config.GitCommandTimeoutDuration
	})
	syncer := runner.New(source, git, store, guard)

	if *once {
		if err := syncer.SyncOnce(ctx); err != nil {
			log.Printf("sync repositories: %v", err)
			return 1
		}
		return 0
	}

	server := &http.Server{
		Addr: snapshot.Config.HTTPAddress,
		Handler: httpapi.NewRouter(httpapi.Options{
			Store:    store,
			Live:     source,
			Sync:     syncer,
			Remote:   syncer,
			States:   guard,
			CacheDir: filepath.Join(snapshot.Config.DataRoot, "cache"),
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("server listening on http://%s", server.Addr)
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
