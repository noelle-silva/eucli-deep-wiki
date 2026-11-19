package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
)

// runCacheClean 清理服务端缓存产物。
//
// 确认是服务端的要求：不加 --yes 时请求照发，服务端拒绝，命令行把「加 --yes」
// 这句话翻给用户听。正在生成的产物会由服务端跳过并说明原因。
func runCacheClean(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("cache clean", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw cache clean [参数] [仓库]")
	options := &clientFlags{}
	options.register(flags, true)
	confirm := flags.Bool("yes", false, "确认清理；不加时服务端会拒绝")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(stderr, "用法: eucli-dw cache clean [参数] [仓库]")
		return exitUsage
	}

	repository := ""
	if flags.NArg() == 1 {
		repository = strings.TrimSpace(flags.Arg(0))
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	result, err := client.ClearCache(context.Background(), repository, *confirm)
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, result)
	}

	label := "全部缓存"
	if repository != "" {
		label = "仓库 " + repository + " 的缓存"
	}
	if result.Removed == 0 {
		fmt.Fprintf(stdout, "%s 没有可清理的产物\n", label)
	} else {
		fmt.Fprintf(stdout, "已清理 %s：%d 个文件，%s\n", label, result.Removed, formatBytes(result.Bytes))
	}

	for _, skipped := range result.Skipped {
		fmt.Fprintf(stderr, "跳过 %s：%s\n", skipped.Name, skipped.Reason)
	}
	return exitOK
}
