package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
)

// runCacheList 列出服务端缓存产物。
//
// 目录不存在等于空清单，不是错误：服务端没生成过产物时「缓存为空」就是正确答案。
func runCacheList(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("cache list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw cache list [参数]")
	options := &clientFlags{}
	options.register(flags, true)

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	listing, err := client.CacheEntries(context.Background(), "")
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, listing)
	}

	if len(listing.Files) == 0 {
		fmt.Fprintln(stdout, "缓存为空")
		return exitOK
	}

	for _, file := range listing.Files {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n",
			file.Name,
			orDash(file.Repo),
			shortRevision(file.Revision),
			formatBytes(file.Bytes),
			formatTime(file.ModifiedAt),
		)
	}
	fmt.Fprintf(stdout, "合计 %d 个文件，%s\n", len(listing.Files), formatBytes(listing.TotalBytes))
	return exitOK
}
