package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"eucli-deep-wiki/internal/apiclient"
)

// runReload 让服务立刻按磁盘内容重载配置与索引。
//
// 重载失败不是命令执行失败：服务端会返回完整结果，这里照样把它打印出来，
// 只是退出码非零，让脚本能发现问题。
func runReload(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("reload", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw reload [参数]")
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

	result, err := client.Reload(context.Background())
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		if code := writeJSON(stdout, stderr, result); code != 0 {
			return code
		}
	} else {
		printReloadResult(stdout, result)
	}

	if result.Error != "" {
		if options.asJSON {
			reportError(stderr, errors.New(result.Error), true)
		}
		return exitFailure
	}
	return exitOK
}

// printReloadResult 用文本描述一次重载到底生效了什么。
func printReloadResult(stdout io.Writer, result apiclient.ReloadResult) {
	fmt.Fprintf(stdout, "配置:     %s\n", reloadState(result.ConfigReloaded))
	fmt.Fprintf(stdout, "索引:     %s\n", reloadState(result.IndexReloaded))
	fmt.Fprintf(stdout, "仓库数:   %d\n", result.Repositories)
	fmt.Fprintf(stdout, "需要重启: %s\n", restartFields(result.NeedsRestart))
	if result.Error != "" {
		fmt.Fprintf(stdout, "错误:     %s\n", result.Error)
	}
}

func reloadState(reloaded bool) string {
	if reloaded {
		return "已重载"
	}
	return "未重载"
}

func restartFields(fields []string) string {
	if len(fields) == 0 {
		return "无"
	}
	return strings.Join(fields, ", ")
}
