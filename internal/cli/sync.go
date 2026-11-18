package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

// syncJSON 是单个仓库按需同步的 JSON 结果。
// durationMs 以毫秒为单位，便于脚本直接读取耗时。
type syncJSON struct {
	Name       string `json:"name"`
	Action     string `json:"action"`
	Branch     string `json:"branch,omitempty"`
	DurationMs int64  `json:"durationMs"`
	Error      string `json:"error,omitempty"`
}

// runSync 立刻同步指定仓库或全部仓库。
func runSync(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("sync", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw sync [参数] <仓库>   或   eucli-dw sync --all [参数]")
	options := &clientFlags{}
	options.register(flags, true)
	all := flags.Bool("all", false, "同步全部仓库")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	if !*all && flags.NArg() != 1 {
		fmt.Fprintln(stderr, "用法: eucli-dw sync <仓库>   或   eucli-dw sync --all")
		return 2
	}
	if *all && flags.NArg() > 0 {
		fmt.Fprintln(stderr, "sync --all 与仓库名不能同时使用：要么同步一个，要么同步全部")
		return 2
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if *all {
		if err := client.SyncAll(context.Background()); err != nil {
			reportError(stderr, err, options.asJSON)
			return exitCodeFor(err)
		}
		if options.asJSON {
			return writeJSON(stdout, stderr, map[string]string{"status": "started"})
		}
		fmt.Fprintln(stdout, "已触发一轮全量同步，用 `eucli-dw status` 查看进度")
		return 0
	}

	repository := flags.Arg(0)
	result, err := client.SyncRepo(context.Background(), repository)
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		// 单个仓库的同步失败仍然返回结果对象，错误同时按 --json 的通用格式写入 stderr。
		if code := writeJSON(stdout, stderr, syncJSON{
			Name:       result.Name,
			Action:     result.Action,
			Branch:     result.Branch,
			DurationMs: time.Duration(result.Duration).Milliseconds(),
			Error:      result.Error,
		}); code != 0 {
			return code
		}
		if result.Error != "" {
			reportError(stderr, errors.New(result.Error), true)
			return 1
		}
		return 0
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
