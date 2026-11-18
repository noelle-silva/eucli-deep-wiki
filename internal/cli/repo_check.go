package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"eucli-deep-wiki/internal/apiclient"
	"eucli-deep-wiki/internal/config"
	"eucli-deep-wiki/internal/repoindex"
)

// checkWorkerCount 控制并发探测数量，避免一次打满远端连接。
const checkWorkerCount = 8

// checkResult 是单个仓库的探测结果。
type checkResult struct {
	Repo      string `json:"repo"`
	Reachable bool   `json:"reachable"`
	Unknown   bool   `json:"unknown,omitempty"`
	Error     string `json:"error,omitempty"`
}

// runRepoCheck 逐个探测仓库的远端是否还在。
//
// 探测走服务的 /v1/repos/{name}/check，只读取远端引用，不改本地仓库。
// 退出码只有两种：全部可达返回 0，否则返回 1；配置或网络问题沿用统一分级。
func runRepoCheck(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repo check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw repo check [参数] [仓库]...")
	serviceConfig := flags.String("service-config", defaultServiceConfig(), "服务配置文件路径")
	asJSON := flags.Bool("json", false, "输出原始 JSON")
	timeout := flags.Int("timeout", 0, "单个探测的超时秒数，0 表示使用默认值")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	cfg, err := config.Load(*serviceConfig)
	if err != nil {
		fmt.Fprintf(stderr, "读取服务配置失败: %v\n", err)
		return exitFailure
	}

	names, err := checkTargets(cfg, flags.Args())
	if err != nil {
		fmt.Fprintf(stderr, "读取仓库索引失败: %v\n", err)
		return exitFailure
	}
	if len(names) == 0 {
		fmt.Fprintln(stdout, "索引为空")
		return exitOK
	}

	client, err := apiclient.New("http://"+serviceAddress(cfg.HTTPAddress), cfg.APIKeys[0])
	if err != nil {
		fmt.Fprintf(stderr, "构建服务客户端失败: %v\n", err)
		return exitFailure
	}
	if *timeout > 0 {
		client = client.WithTimeout(time.Duration(*timeout) * time.Second)
	}

	results, err := probeRepositories(client, names)
	if err != nil {
		reportError(stderr, err, *asJSON)
		return exitCodeFor(err)
	}

	var reachable, unreachable, unknown int
	for _, result := range results {
		switch {
		case result.Unknown:
			unknown++
		case result.Reachable:
			reachable++
		default:
			unreachable++
		}
	}

	if *asJSON {
		code := writeJSON(stdout, stderr, map[string]any{
			"checked":     len(results),
			"reachable":   reachable,
			"unreachable": unreachable,
			"unknown":     unknown,
			"results":     results,
		})
		if code != 0 {
			return code
		}
	} else {
		for _, result := range results {
			switch {
			case result.Unknown:
				fmt.Fprintf(stdout, "%s\t未知仓库\t%s\n", result.Repo, result.Error)
			case result.Reachable:
				fmt.Fprintf(stdout, "%s\t可达\n", result.Repo)
			default:
				fmt.Fprintf(stdout, "%s\t不可达\t%s\n", result.Repo, result.Error)
			}
		}
		fmt.Fprintf(stdout, "检查 %d 个仓库：可达 %d，不可达 %d，未知 %d\n", len(results), reachable, unreachable, unknown)
	}

	if unreachable > 0 || unknown > 0 {
		return exitFailure
	}
	return exitOK
}

// probeRepositories 并发探测，结果顺序与输入一致。
func probeRepositories(client *apiclient.Client, names []string) ([]checkResult, error) {
	results := make([]checkResult, len(names))
	var (
		mu       sync.Mutex
		firstErr error
		workers  sync.WaitGroup
	)

	jobs := make(chan int)
	workerCount := checkWorkerCount
	if workerCount > len(names) {
		workerCount = len(names)
	}

	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				name := names[index]
				status, err := client.CheckRepo(context.Background(), name)
				if err != nil {
					var apiErr *apiclient.APIError
					if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
						results[index] = checkResult{Repo: name, Unknown: true, Error: "仓库不存在或尚未同步"}
						continue
					}
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					continue
				}
				results[index] = checkResult{
					Repo:      name,
					Reachable: status.Reachable,
					Error:     status.Error,
				}
			}
		}()
	}

	for index := range names {
		jobs <- index
	}
	close(jobs)
	workers.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

// checkTargets 决定这一轮要检查哪些仓库：指定了就按指定的来，否则用索引全集。
func checkTargets(cfg *config.Config, requested []string) ([]string, error) {
	if len(requested) > 0 {
		names := make([]string, 0, len(requested))
		for _, name := range requested {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" {
				continue
			}
			names = append(names, trimmed)
		}
		return names, nil
	}

	index, err := repoindex.Load(cfg.RepositoryIndexPath())
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(index.Repositories))
	for _, repository := range index.Repositories {
		names = append(names, repository.Name)
	}
	sort.Strings(names)
	return names, nil
}

// serviceAddress 把监听地址换算成可直连的地址：
// 监听在 0.0.0.0 或 :: 时，本机访问要走回环地址。
func serviceAddress(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
