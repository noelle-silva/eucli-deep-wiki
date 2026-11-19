package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"eucli-dw/internal/apiclient"
)

// runRepoList 打印服务端的名单总览。
//
// 与 repos 的分工：repos 看「仓库现在是什么样」，这里看「名单里声明了什么、
// 磁盘上有没有」——地址、历史档位、是否落盘是名单这一侧才有的事实。
func runRepoList(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repo list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw repo list [参数]")
	options := &clientFlags{}
	options.register(flags, true)
	sizes := flags.Bool("sizes", false, "现测每个已落盘仓库的体积（要遍历服务端目录，较慢）")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	repositories, err := client.Repositories(context.Background(), apiclient.RepositoriesOptions{Measure: *sizes})
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, map[string]any{"repositories": repositories})
	}

	if len(repositories) == 0 {
		fmt.Fprintln(stdout, "名单为空")
		return exitOK
	}

	for _, repository := range repositories {
		line := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s",
			repository.Name,
			branchOf(repository),
			urlOf(repository),
			historyOf(repository),
			onDiskOf(repository),
			listedSize(repository, *sizes),
		)
		if markers := markersOf(repository); markers != "" {
			line += "\t" + markers
		}
		fmt.Fprintln(stdout, line)
	}
	return exitOK
}

// urlOf 输出名单里声明的地址。
func urlOf(repository apiclient.RepoMeta) string {
	if repository.URL == "" {
		return "-"
	}
	return repository.URL
}

// historyOf 输出历史档位；声明与生效不一致时把差别一并写出来。
func historyOf(repository apiclient.RepoMeta) string {
	declared := repository.History
	if declared == "" {
		declared = "默认"
	}
	effective := repository.EffectiveHistory
	if effective == "" || effective == declared {
		return declared
	}
	return declared + "→" + effective
}

// onDiskOf 说明这个仓库在服务端磁盘上有没有落盘。
func onDiskOf(repository apiclient.RepoMeta) string {
	if repository.OnDisk {
		return "已落盘"
	}
	return "未落盘"
}

// listedSize 是名单视图里的体积：--sizes 时用本次现测值，否则用上次同步记录的体积。
// 两者语义不同，所以现测只用 measuredBytes，绝不顶替 gitBytes。
func listedSize(repository apiclient.RepoMeta, measured bool) string {
	if !measured {
		return sizeOf(repository.GitBytes)
	}
	if repository.MeasuredBytes == nil {
		return "-"
	}
	return formatBytes(*repository.MeasuredBytes)
}
