package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"eucli-dw/internal/apiclient"
)

// runRepoUnblock 解除仓库的停止同步状态。
//
// 本来没被拦也是成功：服务端会如实回答没有发生变化，退出码与「解除了」一样是 0。
func runRepoUnblock(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repo unblock", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw repo unblock [参数] <仓库>")
	options := &clientFlags{}
	options.register(flags, true)

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}
	if flags.NArg() != 1 || strings.TrimSpace(flags.Arg(0)) == "" {
		fmt.Fprintln(stderr, "用法: eucli-dw repo unblock [参数] <仓库>")
		return exitUsage
	}
	name := strings.TrimSpace(flags.Arg(0))

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	// 解除「停止同步」这一整套状态，连带清零失败计数与最近错误，与既有口径一致。
	result, err := client.UnblockRepository(context.Background(), name, true)
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, result)
	}

	if !result.Cleared {
		fmt.Fprintf(stdout, "%s 未处于停止同步状态，无需解除\n", result.Name)
		return exitOK
	}
	fmt.Fprintf(stdout, "已解除 %s 的同步限制：%s；下一轮同步会重新尝试\n",
		result.Name, describePreviousState(result.Previous))
	return exitOK
}

// describePreviousState 把解除前的状态原样说出来，便于确认解除的是哪一种拦截。
func describePreviousState(previous apiclient.PreviousState) string {
	reasons := make([]string, 0, 4)
	if previous.Blocked {
		reasons = append(reasons, "体积超限 "+formatBytes(previous.BlockedSize))
	}
	if previous.RemoteMissing {
		reasons = append(reasons, "远端消失")
	}
	if previous.ConsecutiveFailures > 0 {
		reasons = append(reasons, fmt.Sprintf("连续失败 %d 次", previous.ConsecutiveFailures))
	}
	if previous.LastError != "" {
		reasons = append(reasons, "最近错误 "+previous.LastError)
	}
	if len(reasons) == 0 {
		return "没有拦截标记"
	}
	return strings.Join(reasons, "；")
}
