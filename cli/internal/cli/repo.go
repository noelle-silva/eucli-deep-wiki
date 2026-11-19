package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"eucli-dw/internal/apiclient"
)

// runRepo 处理 repo 子命令组。
//
// 这一组命令改的是服务端的名单与仓库状态，但动作全部由服务端执行：
// 命令行只发请求，不读服务端配置、不碰服务端机器上的任何文件。
func runRepo(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || wantsHelp(args[:1]) {
		printRepoUsage(stdout)
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}

	switch args[0] {
	case "add":
		return runRepoAdd(args[1:], stdout, stderr)
	case "list":
		return runRepoList(args[1:], stdout, stderr)
	case "rm":
		return runRepoRemove(args[1:], stdout, stderr)
	case "unblock":
		return runRepoUnblock(args[1:], stdout, stderr)
	case "compact":
		return runRepoCompact(args[1:], stdout, stderr)
	case "check":
		return runRepoCheck(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "未知 repo 子命令: %s\n", args[0])
		printRepoUsage(stderr)
		return exitUsage
	}
}

func printRepoUsage(out io.Writer) {
	fmt.Fprint(out, `用法: eucli-dw repo <子命令> [参数]

子命令:
  add <地址>...                 加入名单，整批原子
                                --branch 分支；--history shallow|full；--no-reload 只写名单
  list [--sizes]                名单总览：地址、分支、历史档位、是否落盘、体积
  rm <仓库> [--drop-data]       从名单摘除；不加只摘名单，加了连磁盘数据一起删
  unblock <仓库>                解除停止同步，让下一轮同步重新尝试
  compact [--dry-run] [仓库]... 就地转为浅克隆；不给仓库名时处理名单全集
  check [仓库]...               探测远端是否还在；不给仓库名时探测名单全集

这些子命令都经服务端执行，需要先配置服务地址与 key。
`)
}

// trimmedNames 清掉仓库名两侧的空白，空项直接丢掉。
func trimmedNames(values []string) []string {
	names := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	return names
}

// targetNames 决定这一轮要处理哪些仓库。
//
// 给了名字就按给的顺序处理：同一批请求的结果顺序因此稳定，与哪个先返回无关。
// 没给名字就取名单全集并按名字排序——名单顺序会随改动变化，排序后的输出才可复现。
func targetNames(ctx context.Context, client *apiclient.Client, requested []string) ([]string, error) {
	names := trimmedNames(requested)
	if len(names) > 0 {
		return names, nil
	}

	repositories, err := client.Repositories(ctx, apiclient.RepositoriesOptions{})
	if err != nil {
		return nil, err
	}
	names = make([]string, 0, len(repositories))
	for _, repository := range repositories {
		names = append(names, repository.Name)
	}
	sort.Strings(names)
	return names, nil
}
