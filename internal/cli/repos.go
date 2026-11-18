package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"eucli-deep-wiki/internal/apiclient"
)

// repoOrder 是 repos 支持的排序方式。
type repoOrder string

const (
	repoOrderName repoOrder = "name"
	repoOrderTime repoOrder = "time"
	repoOrderSize repoOrder = "size"
)

// parseRepoOrder 解析排序参数；缺省按名字。
func parseRepoOrder(raw string) (repoOrder, error) {
	switch repoOrder(strings.ToLower(strings.TrimSpace(raw))) {
	case "", repoOrderName:
		return repoOrderName, nil
	case repoOrderTime:
		return repoOrderTime, nil
	case repoOrderSize:
		return repoOrderSize, nil
	default:
		return repoOrderName, fmt.Errorf("--sort 只支持 name、time 或 size: %q", raw)
	}
}

// runRepos 列出服务器上的仓库。
//
// 首屏就把「这个仓库是什么、多久没同步、多大、有没有被保护」摆出来：
// 这些事实由服务端合并进同一份响应，不必再单独查一次状态接口。
func runRepos(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repos", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw repos [参数]")
	options := &clientFlags{}
	options.register(flags, true)
	order := flags.String("sort", string(repoOrderName),
		"排序方式：name、time（最近同步优先）或 size（体积从大到小）")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}
	sortBy, err := parseRepoOrder(*order)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	repositories, err := client.Repositories(context.Background())
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}
	sortRepositories(repositories, sortBy)

	if options.asJSON {
		return writeJSON(stdout, stderr, map[string]any{"repositories": repositories})
	}

	if len(repositories) == 0 {
		fmt.Fprintln(stdout, "服务器上没有仓库")
		return 0
	}

	for _, repository := range repositories {
		line := fmt.Sprintf("%s\t%s\t%s\t%s\t%s",
			repository.Name,
			branchOf(repository),
			revisionOf(repository),
			formatTime(repository.LastSyncAt),
			sizeOf(repository.GitBytes),
		)
		if markers := markersOf(repository); markers != "" {
			line += "\t" + markers
		}
		fmt.Fprintln(stdout, line)
	}
	return 0
}

// sortRepositories 按名字、同步时间或体积排序；同值一律按名字升序，保证输出可复现。
func sortRepositories(repositories []apiclient.RepoMeta, order repoOrder) {
	sort.SliceStable(repositories, func(left int, right int) bool {
		a, b := repositories[left], repositories[right]
		switch order {
		case repoOrderTime:
			if !a.LastSyncAt.Equal(b.LastSyncAt) {
				return a.LastSyncAt.After(b.LastSyncAt)
			}
		case repoOrderSize:
			if a.GitBytes != b.GitBytes {
				return a.GitBytes > b.GitBytes
			}
		}
		return a.Name < b.Name
	})
}

// markersOf 汇总影响「能不能用」的状态；没有标记时返回空串。
func markersOf(repository apiclient.RepoMeta) string {
	markers := blockingMarkers(repository)
	if !repository.Shallow {
		return markers
	}
	if markers != "" {
		return markers + " SHALLOW"
	}
	return "SHALLOW"
}

// blockingMarkers 只列出「影响能不能用」的标记：体积保护与远端消失。
// 概览里历史深度已经单独写成一行，不需要再挤进状态。
func blockingMarkers(repository apiclient.RepoMeta) string {
	markers := make([]string, 0, 2)
	if repository.Blocked {
		markers = append(markers, fmt.Sprintf("BLOCKED(%s)", formatBytes(repository.BlockedSize)))
	}
	if repository.RemoteMissing {
		markers = append(markers, "MISSING")
	}
	return strings.Join(markers, " ")
}

func branchOf(repository apiclient.RepoMeta) string {
	if repository.Branch == "" {
		return "-"
	}
	return repository.Branch
}

// revisionOf 只取提交号前 12 位：够区分，又不至于挤占一行。
func revisionOf(repository apiclient.RepoMeta) string {
	revision := repository.Revision
	if revision == "" {
		return "-"
	}
	const short = 12
	if len(revision) > short {
		return revision[:short]
	}
	return revision
}

func sizeOf(bytes int64) string {
	if bytes <= 0 {
		return "-"
	}
	return formatBytes(bytes)
}
