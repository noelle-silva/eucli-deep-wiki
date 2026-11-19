package cli

import (
	"fmt"
	"io"
)

// runCache 处理 cache 子命令组。
//
// 缓存产物由服务端生成与删除：命令行只发请求，不碰服务端机器上的目录。
func runCache(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || wantsHelp(args[:1]) {
		printCacheUsage(stdout)
		if len(args) == 0 {
			return exitUsage
		}
		return exitOK
	}

	switch args[0] {
	case "list":
		return runCacheList(args[1:], stdout, stderr)
	case "clean":
		return runCacheClean(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "未知 cache 子命令: %s\n", args[0])
		printCacheUsage(stderr)
		return exitUsage
	}
}

func printCacheUsage(out io.Writer) {
	fmt.Fprint(out, `用法: eucli-dw cache <子命令> [参数]

子命令:
  list [--json]         列出服务端缓存产物：文件名、归属仓库、提交、字节数、修改时间
  clean [--yes] [仓库]  清理服务端缓存产物；不加 --yes 时服务端会拒绝

缓存由服务端维护，命令行只发请求。
`)
}
