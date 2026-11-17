package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"eucli-deep-wiki/internal/apiclient"
)

// runFind 按文件名检索，不读文件内容，因此比内容检索快得多。
func runFind(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("find", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw find [参数] <仓库> <文件名或通配符>（含 * 按通配匹配，否则按子串；不区分大小写）")
	options := &clientFlags{}
	options.register(flags, true)
	searchPath := flags.String("path", "", "只在指定目录内查找")
	limit := flags.Int("limit", 0, "最多返回多少条结果")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}
	if flags.NArg() < 2 {
		fmt.Fprintln(stderr, "用法: eucli-dw find [参数] <仓库> <文件名或通配符>")
		return 2
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	result, err := client.Find(context.Background(), flags.Arg(0), flags.Arg(1), *searchPath, *limit)
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, result)
	}

	if len(result.Entries) == 0 {
		fmt.Fprintf(stdout, "没有匹配的文件（已检查 %d 个文件）\n", result.Scanned)
		return 0
	}

	var entries []apiclient.FoundEntry = result.Entries
	for _, entry := range entries {
		fmt.Fprintf(stdout, "%s\t%d\n", entry.Path, entry.Size)
	}
	if result.Truncated {
		fmt.Fprintf(stdout, "（结果已达上限，共检查 %d 个文件，可用 --limit 调整）\n", result.Scanned)
	}
	return 0
}
