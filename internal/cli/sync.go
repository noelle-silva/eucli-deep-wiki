package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"
)

// runSync 立刻同步指定仓库或全部仓库。
func runSync(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw "+flags.Name()+" [参数]")
	options := &clientFlags{}
	options.register(flags, false)
	all := flags.Bool("all", false, "同步全部仓库")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	if !*all && flags.NArg() != 1 {
		fmt.Fprintln(stderr, "用法: eucli-dw sync <仓库>   或   eucli-dw sync --all")
		return 2
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if *all {
		if err := client.SyncAll(context.Background()); err != nil {
			fmt.Fprintf(stderr, "触发同步失败: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "已触发一轮全量同步，用 status 查看进度")
		return 0
	}

	repository := flags.Arg(0)
	result, err := client.SyncRepo(context.Background(), repository)
	if err != nil {
		fmt.Fprintf(stderr, "同步失败: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "%s\t%s\t%s\n", result.Name, result.Action, formatDuration(result.Duration))
	if result.Error != "" {
		fmt.Fprintf(stderr, "%s\n", result.Error)
		return 1
	}
	return 0
}

func formatDuration(nanoseconds int64) string {
	if nanoseconds <= 0 {
		return "-"
	}
	return time.Duration(nanoseconds).Round(10 * time.Millisecond).String()
}
