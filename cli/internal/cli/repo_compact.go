package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"eucli-dw/internal/apiclient"
)

// compactOutcome 是一个仓库的瘦身结果；成功与失败都留着，一个都不许悄悄丢掉。
type compactOutcome struct {
	name   string
	result apiclient.CompactResult
	err    error
}

// runRepoCompact 让服务端就地转为浅克隆；--dry-run 只预演。
//
// 这是分钟级操作：单个请求的等待预算由客户端按类给出，不沿用普通查询的 30 秒；
// 多个仓库串行处理——同时开几个转换只会互相拖慢，还容易撞上服务端的同步闸门。
func runRepoCompact(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repo compact", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw repo compact [参数] [仓库]...")
	options := &clientFlags{}
	options.register(flags, true)
	dryRun := flags.Bool("dry-run", false, "只预演，不实际转换")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}
	requested := trimmedNames(flags.Args())

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	ctx := context.Background()
	names, err := targetNames(ctx, client, requested)
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}
	if len(names) == 0 && !options.asJSON {
		fmt.Fprintln(stdout, "名单里没有仓库，无需转换")
		return exitOK
	}

	outcomes := make([]compactOutcome, 0, len(names))
	for _, name := range names {
		result, compactErr := client.CompactRepository(ctx, name, *dryRun)
		outcomes = append(outcomes, compactOutcome{name: name, result: result, err: compactErr})
	}

	if options.asJSON {
		return writeCompactJSON(stdout, stderr, *dryRun, outcomes)
	}
	return renderCompact(stdout, stderr, *dryRun, outcomes)
}

// renderCompact 逐条说明每个仓库的结论，最后给一行汇总。
//
// 失败的行写 stderr，退出码取第一个失败的契约映射；已经成功的仓库照常回显，
// 不会因为后面一个失败而变成「什么都没发生」。
func renderCompact(stdout io.Writer, stderr io.Writer, dryRun bool, outcomes []compactOutcome) int {
	counts := map[string]int{}
	failures := 0
	code := exitOK

	for _, outcome := range outcomes {
		if outcome.err != nil {
			failures++
			if code == exitOK {
				code = exitCodeFor(outcome.err)
			}
			fmt.Fprintf(stderr, "%s\t失败: %s\n", outcome.name, describeFailure(outcome.err))
			continue
		}

		result := outcome.result
		counts[result.Action]++
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n",
			displayCompactName(result.Name, outcome.name),
			compactActionText(result.Action),
			compactSizeText(result),
			"耗时 "+formatMilliseconds(result.DurationMs),
		)
	}

	fmt.Fprintln(stdout, compactSummary(dryRun, counts, failures))
	return code
}

// writeCompactJSON 输出结构化结果；部分失败时把失败原因一并放进 payload。
func writeCompactJSON(stdout io.Writer, stderr io.Writer, dryRun bool, outcomes []compactOutcome) int {
	results := make([]apiclient.CompactResult, 0, len(outcomes))
	failures := make([]string, 0, len(outcomes))
	var firstErr error

	for _, outcome := range outcomes {
		if outcome.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %s", outcome.name, describeFailure(outcome.err)))
			if firstErr == nil {
				firstErr = outcome.err
			}
			continue
		}
		results = append(results, outcome.result)
	}

	payload := map[string]any{"dryRun": dryRun, "results": results}
	if len(failures) > 0 {
		payload["failures"] = failures
	}
	if code := writeJSON(stdout, stderr, payload); code != exitOK {
		return code
	}
	if firstErr != nil {
		return exitCodeFor(firstErr)
	}
	return exitOK
}

// compactActionText 把契约里的四种动作结论写成一句人话；没见过的结论原样输出。
func compactActionText(action string) string {
	switch action {
	case apiclient.Compacted:
		return "已就地转为浅克隆"
	case apiclient.WouldCompact:
		return "预演：将会就地转为浅克隆"
	case apiclient.AlreadyShallow:
		return "已是浅克隆，无需转换"
	case apiclient.SkippedFullHistory:
		return "跳过（名单要求完整历史）"
	default:
		return action
	}
}

// compactSizeText 写出体积变化；没有实际转换时后一段是短横线。
func compactSizeText(result apiclient.CompactResult) string {
	return "体积 " + sizeOf(result.Before.GitBytes) + " → " + sizeOf(result.After.GitBytes)
}

// displayCompactName 优先用服务端回的仓库名，缺失时用请求里的名字。
func displayCompactName(reported string, requested string) string {
	if reported != "" {
		return reported
	}
	return requested
}

// compactSummary 汇总这一轮做了什么；失败数量单独说，不混进「跳过」。
func compactSummary(dryRun bool, counts map[string]int, failures int) string {
	shallow := counts[apiclient.AlreadyShallow]
	skipped := counts[apiclient.SkippedFullHistory]

	if dryRun {
		line := fmt.Sprintf("预演完成：待转换 %d 个，已是浅克隆 %d 个，要求完整历史 %d 个（未执行）",
			counts[apiclient.WouldCompact], shallow, skipped)
		return appendFailureCount(line, failures)
	}
	line := fmt.Sprintf("完成：转换 %d 个，已是浅克隆 %d 个，要求完整历史 %d 个",
		counts[apiclient.Compacted], shallow, skipped)
	return appendFailureCount(line, failures)
}

func appendFailureCount(line string, failures int) string {
	if failures == 0 {
		return line
	}
	return fmt.Sprintf("%s，失败 %d 个", line, failures)
}

// formatMilliseconds 把毫秒写成便于阅读的时长。
func formatMilliseconds(milliseconds int64) string {
	if milliseconds <= 0 {
		return "-"
	}
	return (time.Duration(milliseconds) * time.Millisecond).Round(10 * time.Millisecond).String()
}
