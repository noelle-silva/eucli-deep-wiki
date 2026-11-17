package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"eucli-deep-wiki/internal/config"
	"eucli-deep-wiki/internal/repoindex"
)

// runRepo 处理 repo 子命令组。
func runRepo(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "用法: eucli-dw repo <add|list> [参数]")
		return 2
	}

	switch args[0] {
	case "add":
		return runRepoAdd(args[1:], stdout, stderr)
	case "list":
		return runRepoList(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "未知 repo 子命令: %s\n", args[0])
		return 2
	}
}

func runRepoAdd(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repo add", flag.ContinueOnError)
	flags.SetOutput(stderr)
	serviceConfig := flags.String("service-config", "configs/config.json", "服务配置文件路径")

	if err := parseArgs(flags, args); err != nil {
		return 2
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
	serviceConfig := flags.String("service-config", "configs/config.json", "服务配置文件路径")
	asJSON := flags.Bool("json", false, "输出原始 JSON")

	if err := parseArgs(flags, args); err != nil {
		return 2
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
	for _, repository := range index.Repositories {
		if repository.Branch != "" {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", repository.Name, repository.Branch, repository.URL)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%s\n", repository.Name, repository.URL)
	}
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
