package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"eucli-dw/internal/apiclient"
)

// runRepoAdd 把一批地址加入服务端的仓库名单。
//
// 整批原子由服务端保证：任何一条不合法，整批都不生效。命令行只把用户写的
// 地址原样送去，并把「加了哪些、名单现在几个、有没有立即生效」讲清楚。
func runRepoAdd(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repo add", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw repo add [参数] <地址>...")
	options := &clientFlags{}
	options.register(flags, true)
	branch := flags.String("branch", "", "只跟这一个分支，留空由服务端决定")
	history := flags.String("history", "", "历史档位：shallow 或 full，留空由服务端决定")
	noReload := flags.Bool("no-reload", false, "只写名单，不让服务立刻加载")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	urls := collectURLs(flags.Args())
	if len(urls) == 0 {
		fmt.Fprintln(stderr, "用法: eucli-dw repo add [参数] <地址>...")
		return exitUsage
	}
	level, err := parseHistory(*history)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	result, err := client.AddRepositories(context.Background(), apiclient.AddOptions{
		URLs:    urls,
		Branch:  strings.TrimSpace(*branch),
		History: level,
		Reload:  !*noReload,
	})
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, result)
	}

	for _, added := range result.Added {
		fmt.Fprintf(stdout, "已加入 %s\t%s\n", added.Name, added.URL)
		if detail := addedDetail(added); detail != "" {
			fmt.Fprintf(stdout, "  %s\n", detail)
		}
	}
	fmt.Fprintf(stdout, "名单共 %d 个仓库\n", result.Repositories)
	printReloadOutcome(stdout, stderr, result.Reloaded)
	return exitOK
}

// parseHistory 校验 --history：两个取值就是契约里定的两个档位。
func parseHistory(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	switch trimmed {
	case "":
		return "", nil
	case apiclient.HistoryShallow, apiclient.HistoryFull:
		return trimmed, nil
	default:
		return "", fmt.Errorf("--history 只支持 %s 或 %s: %q", apiclient.HistoryShallow, apiclient.HistoryFull, raw)
	}
}

// addedDetail 说明这个仓库按什么分支、什么档位落定；三项都缺省时返回空串。
func addedDetail(added apiclient.AddedRepository) string {
	facts := make([]string, 0, 2)
	if added.Branch != "" {
		facts = append(facts, "分支 "+added.Branch)
	}
	if added.History != "" {
		facts = append(facts, "声明档位 "+added.History)
	}
	if added.EffectiveHistory != "" && added.EffectiveHistory != added.History {
		facts = append(facts, "生效档位 "+added.EffectiveHistory)
	}
	return strings.Join(facts, "  ")
}

// printReloadOutcome 说明新名单有没有立即生效。
//
// 没生效不等于命令失败：名单已经写进服务端，服务在下一轮同步前会自己读。
// 事实必须说出来，否则用户会以为这就已经生效了。
func printReloadOutcome(stdout io.Writer, stderr io.Writer, reloaded *apiclient.ReloadOutcome) {
	if reloaded == nil {
		fmt.Fprintln(stdout, "未通知服务立即加载，新名单在服务下一轮同步前生效")
		return
	}
	if reloaded.Error != "" {
		fmt.Fprintf(stderr, "提示: 名单已写入，但服务未接受重载（%s），将在下一轮同步前自动加载\n", reloaded.Error)
		return
	}
	if !reloaded.IndexReloaded {
		fmt.Fprintln(stderr, "提示: 名单已写入，但服务没有重新加载名单，将在下一轮同步前自动加载")
		return
	}
	fmt.Fprintf(stdout, "已立即生效: 配置=%s 索引=%s（%d 个仓库）\n",
		reloadState(reloaded.ConfigReloaded), reloadState(reloaded.IndexReloaded), reloaded.Repositories)
}

// collectURLs 把参数里的地址展开成一串：允许把几个地址用空白连在一个参数里。
func collectURLs(args []string) []string {
	urls := make([]string, 0, len(args))
	for _, arg := range args {
		urls = append(urls, strings.Fields(arg)...)
	}
	return urls
}
