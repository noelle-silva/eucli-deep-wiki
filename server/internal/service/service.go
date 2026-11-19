// Package service 组装服务端进程：读配置、建立运行期状态、跑同步循环、
// 对外提供接口。
//
// 它是服务端程序唯一的入口逻辑，不承担任何命令行职责：没有子命令分发，
// 也不知道客户端怎么连上来。
package service

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"eucli-deep-wiki-server/internal/cache"
	"eucli-deep-wiki-server/internal/gitops"
	"eucli-deep-wiki-server/internal/httpapi"
	"eucli-deep-wiki-server/internal/live"
	"eucli-deep-wiki-server/internal/ops"
	"eucli-deep-wiki-server/internal/release"
	"eucli-deep-wiki-server/internal/runner"
	"eucli-deep-wiki-server/internal/state"
	"eucli-deep-wiki-server/internal/status"
)

const (
	// ProgramName 是服务端程序名。
	ProgramName = "eucli-deep-wiki-server"
	// DefaultConfigPath 是未指定 --config 时读取的配置文件位置。
	DefaultConfigPath = "configs/config.json"
)

// options 是启动参数。
type options struct {
	configPath  string
	httpAddress string
	once        bool
	version     bool
}

// Main 解析参数并运行服务，返回进程退出码。
func Main(args []string, stdout io.Writer, stderr io.Writer) int {
	settings, code, finished := parse(args, stderr)
	if finished {
		return code
	}
	if settings.version {
		return printVersion(stdout, stderr)
	}
	return run(settings)
}

// printVersion 打印版本事实。
//
// 版本号只认模块内的版本文件（release.json），代码里没有第二份。
func printVersion(stdout io.Writer, stderr io.Writer) int {
	info, err := release.Load()
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s %s\n", ProgramName, info.Version)
	return 0
}

// parse 解析启动参数。第二个返回值只在 finished 为真时有意义。
func parse(args []string, stderr io.Writer) (options, int, bool) {
	flags := flag.NewFlagSet(ProgramName, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "用法: %s [参数]\n", ProgramName)
		flags.PrintDefaults()
	}

	configPath := flags.String("config", DefaultConfigPath, "服务配置文件路径")
	httpAddress := flags.String("http-address", "", "覆盖配置文件里的监听地址")
	once := flags.Bool("once", false, "只同步一轮后退出，不启动接口")
	version := flags.Bool("version", false, "打印版本后退出")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return options{}, 0, true
		}
		return options{}, 2, true
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "%s 不接受位置参数: %v\n", ProgramName, flags.Args())
		return options{}, 2, true
	}

	return options{
		configPath:  *configPath,
		httpAddress: *httpAddress,
		once:        *once,
		version:     *version,
	}, 0, false
}

// run 启动服务：先建立运行期状态，再按是否需要接口分两条路走。
func run(settings options) int {
	// 运行期状态从这里开始负责配置文件与索引的生命周期：
	// 首次加载失败直接退出，之后的变化由重载接手。
	source, err := live.Open(live.Options{
		ConfigPath:  settings.configPath,
		HTTPAddress: settings.httpAddress,
	})
	if err != nil {
		log.Printf("load runtime state: %v", err)
		return 1
	}
	snapshot := source.Snapshot()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	guard, err := state.Open(state.Path(snapshot.Config.DataRoot))
	if err != nil {
		log.Printf("load sync state: %v", err)
		return 1
	}

	store := status.NewStore()
	git := gitops.New(snapshot.RepositoryDir, func() time.Duration {
		return source.Snapshot().Config.GitCommandTimeoutDuration
	})
	syncer := runner.New(source, git, store, guard)

	if settings.once {
		if err := syncer.SyncOnce(ctx); err != nil {
			log.Printf("sync repositories: %v", err)
			return 1
		}
		return 0
	}

	server := &http.Server{
		Addr: snapshot.Config.HTTPAddress,
		Handler: assemble(dependencies{
			source:   source,
			statuses: store,
			syncer:   syncer,
			states:   guard,
			git:      git,
			cacheDir: snapshot.Config.CacheDir(),
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// 先把端口绑上，绑上了才报就绪：就绪行的意思是「接口从这里开始接受请求」，
	// 不是「准备去监听」。绑不上就直接退出，绝不留下一条会被平台当成可用的假记录。
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		log.Printf("listen on %s: %v", server.Addr, err)
		return 1
	}
	// 端口已经在接受连接，服务循环紧接着跑在这条监听上。
	log.Print(readyNotice(listener.Addr().String()))

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

// readyNotice 拼出平台判定「服务可用」的那一行。
//
// 自我介绍卡（.fast-window-dev-protocol/fw-app.json）里的匹配词固定为 is ready，
// 因此这行文字是对外契约的一部分：改它等于改平台的就绪判定。
func readyNotice(address string) string {
	return fmt.Sprintf("%s is ready — listening on http://%s", ProgramName, address)
}

// dependencies 是装配对外接口所需的运行态：都由 run 建立并长期持有。
type dependencies struct {
	// source 是运行期可替换的配置、名单与只读视图。
	source *live.State
	// statuses 是同步进度与最近一轮结果。
	statuses *status.Store
	// syncer 是按需同步、批量同步与同步闸门的持有者。
	syncer *runner.Runner
	// states 是每个仓库的体积保护与远端状态，同步器写的就是它。
	states *state.Store
	// git 是版本管理能力，破坏性运维动作经由它执行。
	git *gitops.Client
	// cacheDir 是产物目录。
	cacheDir string
}

// assemble 把服务端持有的运行态装配成对外接口。
//
// 装配只在这一处发生。管理能力与同步拿到的是同一份状态、同一道同步闸门：
// 因此「解除停止同步」改的就是同步器读的那份运行态，下一轮写回不会把它变回去；
// 「就地瘦身」与同步天然互斥，忙的时候直接给出「正忙」。
func assemble(built dependencies) http.Handler {
	management := ops.New(ops.Options{
		Source:    built.source,
		States:    built.states,
		Git:       built.git,
		Exclusive: built.syncer.Exclusive,
	})
	return httpapi.NewRouter(httpapi.Options{
		Store:  built.statuses,
		Live:   built.source,
		Sync:   built.syncer,
		Remote: built.syncer,
		States: built.states,
		Cache:  cache.Open(built.cacheDir),
		Ops:    management,
	})
}
