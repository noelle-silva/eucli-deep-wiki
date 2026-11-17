package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"eucli-deep-wiki/internal/apiclient"
)

// runGet 读取一个文件。
func runGet(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("get", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw "+flags.Name()+" [参数]")
	options := &clientFlags{}
	options.register(flags, true)

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
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
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
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
	setupHelp(flags, "eucli-dw "+flags.Name()+" [参数]")
	options := &clientFlags{}
	options.register(flags, true)

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
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
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
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
	setupHelp(flags, "eucli-dw "+flags.Name()+" [参数]")
	options := &clientFlags{}
	options.register(flags, true)
	limit := flags.Int("limit", 0, "最多返回多少条命中")
	offset := flags.Int("offset", 0, "跳过前多少条命中，用于翻页")
	searchPath := flags.String("path", "", "只在指定目录内检索")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
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

	result, err := client.Search(context.Background(), repo, apiclient.SearchOptions{
		Query:  query,
		Path:   *searchPath,
		Limit:  *limit,
		Offset: *offset,
	})
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, result)
	}

	if len(result.Matches) == 0 {
		fmt.Fprintf(stdout, "没有命中（已扫描 %d 个文件，偏移 %d）\n", result.Scanned, result.Offset)
		return 0
	}
	for _, match := range result.Matches {
		fmt.Fprintf(stdout, "%s:%d: %s\n", match.Path, match.Line, match.Text)
	}
	if result.Truncated {
		fmt.Fprintf(stdout, "（还有更多命中，可用 --offset %d 继续；已扫描 %d 个文件）\n", result.Offset+len(result.Matches), result.Scanned)
	}
	return 0
}
