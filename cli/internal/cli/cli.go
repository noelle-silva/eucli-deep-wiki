// Package cli 实现 eucli-dw 的全部子命令。
//
// 每条命令只做两件事：读自己的连接配置、通过网络向服务端要东西。
// 命令不读取服务端机器上的任何文件，也不知道服务端的目录布局——
// 地址与 key 是它唯一的外部依赖。
package cli

import (
	"fmt"
	"io"
)

const usageText = `eucli-dw —— 参考仓库的查询与管理工具

用法:
  eucli-dw <命令> [参数]

查询命令（需要先配置服务地址与 key）:
  repos [--sort name|time|size]    仓库清单：分支、提交、上次同步、体积、状态
  overview [参数] <仓库>           仓库概览：目录骨架 + README 开头 + 清单文件
  tree [参数] <仓库> [目录]        列出目录（--depth N 一次展开多层；--dirs-only 只看骨架）
  get [参数] <仓库> <路径>...      读取文件（可多路径；--lines 切段；--max-bytes 限长）
  search [参数] <仓库> <关键词>    按内容检索（--path 限目录；--context 上下文行；--limit 条数）
  find [参数] <仓库> <文件名>      按文件名查找（默认精确；--substring 改为包含；支持 * 通配）
  status [参数]                    查看同步状态
  reload [参数]                    让服务立刻重载配置与索引
  sync [--json] <仓库> | sync --all [--json]
                                   立刻同步，不等定时轮
  pull <仓库>[:路径] ... [-o 文件]  下载快照包（zip / tar.gz）
  bundle <仓库> [-o 文件]          下载带历史的 bundle

管理命令（同样只走网络，动作全部由服务端执行）:
  repo add <地址>...               加入名单，整批原子
                                   --branch 分支；--history shallow|full；--no-reload 只写名单
  repo list [--sizes]              名单总览：地址、分支、历史档位、是否落盘、体积
  repo rm <仓库> [--drop-data]     从名单摘除；不加只摘名单，加了连磁盘数据一起删
  repo unblock <仓库>              解除停止同步，让下一轮同步重新尝试
  repo compact [--dry-run] [仓库]...
                                   就地转为浅克隆（分钟级操作；不给仓库名时处理名单全集）
  repo check [仓库]...             探测远端是否还在（不给仓库名时探测名单全集）
  cache list [--json]              列出服务端缓存产物
  cache clean [--yes] [仓库]       清理服务端缓存产物（不加 --yes 时服务端会拒绝）

配置命令:
  config set server <url>          设置服务地址（明文或加密地址都照用）
  config set key <key>             设置 key
  config show [--json]             查看当前配置（key 打码）
  config path                      打印配置文件位置

其它:
  version                          打印构建信息
  help                             打印本帮助

连接与输出参数（查询与管理命令通用）:
  --config 路径   指定客户端配置文件
  --server 地址   临时覆盖服务地址
  --key key       临时覆盖 key
  --json          输出原始 JSON
  --timeout 秒    这次调用的等待上限；0 表示按命令自己的预算

每个子命令都支持 -h 看该命令的完整参数。

示例:
  eucli-dw config set server https://wiki.example.com
  eucli-dw config set key 0123456789abcdef
  eucli-dw repo add https://github.com/xxxx/xxxx.git --branch main
  eucli-dw repo list --sizes
  eucli-dw repo compact --dry-run
  eucli-dw overview claw-code
  eucli-dw tree claw-code --depth 2 --dirs-only
  eucli-dw get claw-code cli.js --lines 362-520
  eucli-dw search claw-code subagent --context 2 --limit 20
  eucli-dw find claw-code "*.md" --path docs --substring

退出码:
  0 成功           1 其它失败        2 用法错误
  3 未找到         4 鉴权失败        5 网络或超时
  6 任务冲突       7 被拒绝（过大 / 非文本）
`

// Run 执行一次命令行调用，返回进程退出码。
func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return exitUsage
	}

	command := args[0]
	rest := args[1:]

	switch command {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText)
		return exitOK
	case "get":
		return runGet(rest, stdout, stderr)
	case "tree":
		return runTree(rest, stdout, stderr)
	case "search":
		return runSearch(rest, stdout, stderr)
	case "find":
		return runFind(rest, stdout, stderr)
	case "pull":
		return runPull(rest, stdout, stderr)
	case "bundle":
		return runBundle(rest, stdout, stderr)
	case "sync":
		return runSync(rest, stdout, stderr)
	case "repos":
		return runRepos(rest, stdout, stderr)
	case "repo":
		return runRepo(rest, stdout, stderr)
	case "cache":
		return runCache(rest, stdout, stderr)
	case "overview":
		return runOverview(rest, stdout, stderr)
	case "status":
		return runStatus(rest, stdout, stderr)
	case "reload":
		return runReload(rest, stdout, stderr)
	case "version":
		return runVersion(rest, stdout, stderr)
	case "config":
		return runConfig(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "未知命令: %s\n\n", command)
		fmt.Fprint(stderr, usageText)
		return exitUsage
	}
}
