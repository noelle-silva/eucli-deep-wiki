// Package cli 实现 eucli-dw 的全部子命令。
//
// 命令分两类：本地命令（serve、repo）直接操作部署目录；
// 查询命令（get、tree、search）只走网络，不读取任何本地仓库。
package cli

import (
	"fmt"
	"io"
)

const usageText = `eucli-dw —— 参考仓库的同步与只读查询工具

用法:
  eucli-dw <命令> [参数]

本地命令:
  serve [--service-config 路径] [--http-address 地址] [--once]
      启动同步与查询服务；--once 只同步一轮后退出
  repo add [--service-config 路径] [--no-reload] <git-url>...
      把仓库加入索引；默认顺便让运行中的服务立刻加载
  repo list [--service-config 路径] [--json] [--sizes]
      查看索引中的仓库；--sizes 附带 .git 体积（需要遍历磁盘）
  repo unblock <仓库>              解除体积保护或远端消失标记，恢复同步
  repo check [仓库]...             探测远端是否还在，默认检查全部仓库
  repo compact [仓库]...           就地转为浅克隆

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

配置命令:
  config set server <url>          设置服务地址
  config set key <key>             设置 key
  config show [--json]             查看当前配置（key 打码）
  config path                      打印配置文件位置
  cache list | cache clean         查看或清空 bundle 缓存

其它:
  version                          打印构建信息
  help                             打印本帮助

查询命令通用参数:
  --config 路径   指定客户端配置文件
  --server 地址   临时覆盖服务地址
  --key key       临时覆盖 key
  --json          输出原始 JSON

每个子命令都支持 -h 看该命令的完整参数。

示例:
  eucli-dw config set server http://127.0.0.1:9997
  eucli-dw config set key 0123456789abcdef
  eucli-dw overview claw-code
  eucli-dw tree claw-code --depth 2 --dirs-only
  eucli-dw get claw-code cli.js --lines 362-520
  eucli-dw get claw-code README.md src/index.ts --max-bytes 4096
  eucli-dw search claw-code subagent --context 2 --limit 20
  eucli-dw find claw-code "index.ts" --path src
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
		return 2
	}

	command := args[0]
	rest := args[1:]

	switch command {
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText)
		return 0
	case "serve":
		return runServe(rest, stderr)
	case "repo":
		return runRepo(rest, stdout, stderr)
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
	case "cache":
		return runCache(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "未知命令: %s\n\n", command)
		fmt.Fprint(stderr, usageText)
		return 2
	}
}
