package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"eucli-deep-wiki/internal/config"
	"eucli-deep-wiki/internal/gitops"
	"eucli-deep-wiki/internal/repoindex"
	"eucli-deep-wiki/internal/state"
)

// runRepo 处理 repo 子命令组。
func runRepo(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || wantsHelp(args[:1]) {
		printRepoUsage(stdout)
		if len(args) == 0 {
			return 2
		}
		return 0
	}

	switch args[0] {
	case "add":
		return runRepoAdd(args[1:], stdout, stderr)
	case "list":
		return runRepoList(args[1:], stdout, stderr)
	case "unblock":
		return runRepoUnblock(args[1:], stdout, stderr)
	case "compact":
		return runRepoCompact(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "未知 repo 子命令: %s\n", args[0])
		printRepoUsage(stderr)
		return 2
	}
}

func printRepoUsage(out io.Writer) {
	fmt.Fprint(out, `用法: eucli-dw repo <子命令> [参数]

子命令:
  add <git-url>...       把仓库加入索引
  list                   查看索引中的仓库
  unblock <仓库>          解除体积保护，恢复同步
  compact [仓库]...       就地转为浅克隆，不重新下载
`)
}

func runRepoAdd(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repo add", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw "+flags.Name()+" [参数]")
	serviceConfig := flags.String("service-config", defaultServiceConfig(), "服务配置文件路径")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	urls := collectURLs(flags.Args())
	if len(urls) == 0 {
		fmt.Fprintln(stderr, "用法: eucli-dw repo add [--service-config 路径] <git-url>...")
		return 2
	}

	cfg, err := config.Load(*serviceConfig)
	if err != nil {
		fmt.Fprintf(stderr, "读取服务配置失败: %v\n", err)
		return 1
	}

	indexPath := cfg.RepositoryIndexPath()
	index, err := repoindex.Load(indexPath)
	if err != nil {
		fmt.Fprintf(stderr, "读取仓库索引失败: %v\n", err)
		return 1
	}

	added, err := index.AddURLs(urls)
	if err != nil {
		fmt.Fprintf(stderr, "添加仓库失败: %v\n", err)
		return 1
	}
	if err := index.Save(indexPath); err != nil {
		fmt.Fprintf(stderr, "写入仓库索引失败: %v\n", err)
		return 1
	}

	for _, repository := range added {
		fmt.Fprintf(stdout, "added %s %s\n", repository.Name, repository.URL)
	}
	return 0
}

func runRepoList(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repo list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw "+flags.Name()+" [参数]")
	serviceConfig := flags.String("service-config", defaultServiceConfig(), "服务配置文件路径")
	asJSON := flags.Bool("json", false, "输出原始 JSON")
	sizes := flags.Bool("sizes", false, "附带 .git 体积（需要遍历磁盘，较慢）")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	cfg, err := config.Load(*serviceConfig)
	if err != nil {
		fmt.Fprintf(stderr, "读取服务配置失败: %v\n", err)
		return 1
	}

	index, err := repoindex.Load(cfg.RepositoryIndexPath())
	if err != nil {
		fmt.Fprintf(stderr, "读取仓库索引失败: %v\n", err)
		return 1
	}

	if *asJSON {
		return writeJSON(stdout, stderr, map[string]any{"repositories": index.Repositories})
	}

	if len(index.Repositories) == 0 {
		fmt.Fprintln(stdout, "索引为空")
		return 0
	}

	guard, err := state.Open(filepath.Join(cfg.DataRoot, state.FileName))
	if err != nil {
		fmt.Fprintf(stderr, "读取同步状态失败: %v\n", err)
		return 1
	}

	for _, repository := range index.Repositories {
		stateOfRepo := guard.Get(repository.Name)

		marker := ""
		if stateOfRepo.Blocked {
			marker = fmt.Sprintf("\tBLOCKED(%d MB)", stateOfRepo.BlockedSize>>20)
		}

		branch := repository.Branch
		if branch == "" {
			branch = readHeadBranch(filepath.Join(cfg.RepositoryDir(), repository.Name))
		}
		if branch == "" {
			branch = "-"
		}

		sizeText := ""
		if *sizes {
			size, err := directorySize(filepath.Join(cfg.RepositoryDir(), repository.Name, ".git"))
			if err == nil {
				sizeText = fmt.Sprintf("\t%d MB", size>>20)
			} else {
				sizeText = "\t-"
			}
		}

		fmt.Fprintf(stdout, "%s\t%s\t%s%s%s\n", repository.Name, branch, repository.URL, sizeText, marker)
	}
	return 0
}

// readHeadBranch 直接从 .git/HEAD 读取分支名，索引未声明时用它兜底。
func readHeadBranch(repoPath string) string {
	data, err := os.ReadFile(filepath.Join(repoPath, ".git", "HEAD"))
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "ref: refs/heads/") {
		return ""
	}
	return strings.TrimPrefix(line, "ref: refs/heads/")
}

// directorySize 统计目录占用的字节数。
func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// runRepoUnblock 解除某个仓库的体积保护。
func runRepoUnblock(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repo unblock", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw "+flags.Name()+" [参数]")
	serviceConfig := flags.String("service-config", defaultServiceConfig(), "服务配置文件路径")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "用法: eucli-dw repo unblock [--service-config 路径] <仓库>")
		return 2
	}

	name := flags.Arg(0)

	cfg, err := config.Load(*serviceConfig)
	if err != nil {
		fmt.Fprintf(stderr, "读取服务配置失败: %v\n", err)
		return 1
	}

	guard, err := state.Open(filepath.Join(cfg.DataRoot, state.FileName))
	if err != nil {
		fmt.Fprintf(stderr, "读取同步状态失败: %v\n", err)
		return 1
	}

	if !guard.Get(name).Blocked {
		fmt.Fprintf(stdout, "%s 未处于停止同步状态\n", name)
		return 0
	}

	if err := guard.Put(name, state.RepoState{}); err != nil {
		fmt.Fprintf(stderr, "写入状态失败: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "已解除 %s 的同步限制，下一轮同步会重新尝试\n", name)
	return 0
}

// runRepoCompact 把仓库就地转为浅克隆，避免重新下载工作树。
func runRepoCompact(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repo compact", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw repo compact [--dry-run] [仓库]...")
	serviceConfig := flags.String("service-config", defaultServiceConfig(), "服务配置文件路径")
	dryRun := flags.Bool("dry-run", false, "只列出将要处理的仓库，不实际转换")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	cfg, err := config.Load(*serviceConfig)
	if err != nil {
		fmt.Fprintf(stderr, "读取服务配置失败: %v\n", err)
		return 1
	}
	index, err := repoindex.Load(cfg.RepositoryIndexPath())
	if err != nil {
		fmt.Fprintf(stderr, "读取仓库索引失败: %v\n", err)
		return 1
	}

	wanted := map[string]struct{}{}
	for _, name := range flags.Args() {
		wanted[name] = struct{}{}
	}

	client := gitops.New(cfg.RepositoryDir(), cfg.GitCommandTimeoutDuration)
	compacted := 0

	for _, repository := range index.Repositories {
		if len(wanted) > 0 {
			if _, ok := wanted[repository.Name]; !ok {
				continue
			}
		}

		if repository.EffectiveHistory(cfg.DefaultHistory) != repoindex.HistoryShallow {
			fmt.Fprintf(stdout, "%s\t跳过（索引要求完整历史）\n", repository.Name)
			continue
		}

		path := filepath.Join(cfg.RepositoryDir(), repository.Name)
		if *dryRun {
			fmt.Fprintf(stdout, "%s\t将会就地转为浅克隆\n", repository.Name)
			compacted++
			continue
		}
		if err := client.Compact(context.Background(), path); err != nil {
			fmt.Fprintf(stderr, "%s\t失败: %v\n", repository.Name, err)
			return 1
		}
		fmt.Fprintf(stdout, "%s\t已就地转为浅克隆\n", repository.Name)
		compacted++
	}

	if *dryRun {
		fmt.Fprintf(stdout, "预演完成，共 %d 个仓库待转换（未执行）\n", compacted)
		return 0
	}
	fmt.Fprintf(stdout, "完成，共处理 %d 个仓库\n", compacted)
	return 0
}

func collectURLs(args []string) []string {
	var urls []string
	for _, arg := range args {
		urls = append(urls, strings.Fields(arg)...)
	}
	return urls
}

func writeJSON(stdout io.Writer, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(stderr, "输出失败: %v\n", err)
		return 1
	}
	return 0
}
