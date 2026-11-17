package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
)

// runGet 读取一个文件。
func runGet(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("get", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := &clientFlags{}
	options.register(flags, true)

	if err := parseArgs(flags, args); err != nil {
		return 2
	}
	if flags.NArg() < 2 {
		fmt.Fprintln(stderr, "用法: eucli-dw get [参数] <仓库> <路径>")
		return 2
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	repo := flags.Arg(0)
	target := flags.Arg(1)

	file, err := client.File(context.Background(), repo, target)
	if err != nil {
		fmt.Fprintf(stderr, "读取失败: %v\n", err)
		return 1
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, map[string]string{
			"repo":     file.Repo,
			"path":     file.Path,
			"revision": file.Revision,
			"content":  file.Content,
		})
	}

	fmt.Fprint(stdout, file.Content)
	return 0
}

// runTree 列出目录。
func runTree(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("tree", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := &clientFlags{}
	options.register(flags, true)

	if err := parseArgs(flags, args); err != nil {
		return 2
	}
	if flags.NArg() < 1 {
		fmt.Fprintln(stderr, "用法: eucli-dw tree [参数] <仓库> [目录]")
		return 2
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	repo := flags.Arg(0)
	target := ""
	if flags.NArg() > 1 {
		target = flags.Arg(1)
	}

	tree, err := client.Tree(context.Background(), repo, target)
	if err != nil {
		fmt.Fprintf(stderr, "读取失败: %v\n", err)
		return 1
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, tree)
	}

	for _, entry := range tree.Entries {
		if entry.Type == "dir" {
			fmt.Fprintf(stdout, "%s/\n", entry.Path)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%d\n", entry.Path, entry.Size)
	}
	if tree.Truncated {
		fmt.Fprintln(stdout, "（结果已截断）")
	}
	return 0
}

// runSearch 在仓库内检索。
func runSearch(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("search", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := &clientFlags{}
	options.register(flags, true)
	limit := flags.Int("limit", 0, "最多返回多少条命中")

	if err := parseArgs(flags, args); err != nil {
		return 2
	}
	if flags.NArg() < 2 {
		fmt.Fprintln(stderr, "用法: eucli-dw search [参数] <仓库> <关键词>")
		return 2
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	repo := flags.Arg(0)
	query := strings.Join(flags.Args()[1:], " ")

	result, err := client.Search(context.Background(), repo, query, *limit)
	if err != nil {
		fmt.Fprintf(stderr, "检索失败: %v\n", err)
		return 1
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, result)
	}

	if len(result.Matches) == 0 {
		fmt.Fprintln(stdout, "没有命中")
		return 0
	}
	for _, match := range result.Matches {
		fmt.Fprintf(stdout, "%s:%d: %s\n", match.Path, match.Line, match.Text)
	}
	if result.Truncated {
		fmt.Fprintln(stdout, "（结果已截断）")
	}
	return 0
}
