package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"eucli-dw/internal/apiclient"
)

// checkConcurrency 限制同时进行的探测数量。
//
// 每次探测都要服务端去问一次远端：一次全放出去只会把出口打满，
// 也容易让服务端同时起一堆版本管理工具。
const checkConcurrency = 8

// probeState 是一个仓库的探测结论。
type probeState string

const (
	// probeReachable 表示远端可达。
	probeReachable probeState = "reachable"
	// probeUnreachable 表示仓库在名单里，但远端连不上。
	probeUnreachable probeState = "unreachable"
	// probeNotListed 表示这个名字根本不在名单里。
	probeNotListed probeState = "not_listed"
	// probeFailed 表示这次探测本身没做成（鉴权、网络、服务端错误）。
	probeFailed probeState = "failed"
)

// probeOutcome 是一个仓库的探测结论；err 留着原始错误，退出码按契约映射。
type probeOutcome struct {
	repo   string
	state  probeState
	detail string
	err    error
}

// probeResult 是结构化输出里的一条。
type probeResult struct {
	Repo  string `json:"repo"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// runRepoCheck 探测远端是否还在。
//
// 目标集合来自名单总览：不带仓库名时就是名单全集的排序结果。探测并发进行，
// 结果按目标顺序输出——顺序与哪个先返回无关。
//
// 「不在名单」与「不可达」是两件事：前者说明名字写错或还没加进名单，
// 后者说明名字对但远端确实连不上，必须分开表达。
func runRepoCheck(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repo check", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw repo check [参数] [仓库]...")
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

	ctx := context.Background()
	names, err := targetNames(ctx, client, flags.Args())
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}
	if len(names) == 0 && !options.asJSON {
		fmt.Fprintln(stdout, "名单里没有仓库，无需探测")
		return exitOK
	}

	outcomes := gather(len(names), checkConcurrency, func(index int) (apiclient.RemoteStatus, error) {
		return client.CheckRepo(ctx, names[index])
	})

	probes := make([]probeOutcome, len(names))
	for index, result := range outcomes {
		probes[index] = classifyProbe(names[index], result)
	}

	if options.asJSON {
		return writeProbeJSON(stdout, stderr, probes)
	}

	for _, probe := range probes {
		line := fmt.Sprintf("%s\t%s", probe.repo, probeStateText(probe.state))
		if probe.detail != "" {
			line += "\t" + probe.detail
		}
		fmt.Fprintln(stdout, line)
	}
	fmt.Fprintln(stdout, probeSummary(probes))
	return probeExitCode(probes)
}

// classifyProbe 把一个探测结果分成四类，不给「不在名单」与「不可达」留混写的余地。
func classifyProbe(name string, result outcome[apiclient.RemoteStatus]) probeOutcome {
	if result.err != nil {
		var apiErr *apiclient.APIError
		if errors.As(result.err, &apiErr) && apiErr.Code == "repo_not_found" {
			// 结论本身已经说清「不在名单」，不必再回显服务端那句同义的话。
			return probeOutcome{repo: name, state: probeNotListed, err: result.err}
		}
		return probeOutcome{repo: name, state: probeFailed, detail: describeFailure(result.err), err: result.err}
	}
	if result.value.Reachable {
		return probeOutcome{repo: name, state: probeReachable}
	}
	return probeOutcome{repo: name, state: probeUnreachable, detail: result.value.Error}
}

// probeStateText 是四种结论在屏幕上的写法。
func probeStateText(state probeState) string {
	switch state {
	case probeReachable:
		return "可达"
	case probeUnreachable:
		return "不可达"
	case probeNotListed:
		return "不在名单"
	default:
		return "探测失败"
	}
}

// probeExitCode 取第一个非零的退出码：全部可达为 0。
//
// 「不可达」不是接口错误，按其它失败记 1；「不在名单」按未找到记 3；
// 探测本身没做成时用契约映射（鉴权、网络、超时）。
func probeExitCode(probes []probeOutcome) int {
	for _, probe := range probes {
		if probe.err != nil {
			if mapped := exitCodeFor(probe.err); mapped != exitOK {
				return mapped
			}
			continue
		}
		if probe.state == probeUnreachable {
			return exitFailure
		}
	}
	return exitOK
}

// probeSummary 汇总这一轮的结论。
func probeSummary(probes []probeOutcome) string {
	counts := map[probeState]int{}
	for _, probe := range probes {
		counts[probe.state]++
	}
	return fmt.Sprintf("检查 %d 个仓库：可达 %d，不可达 %d，不在名单 %d，探测失败 %d",
		len(probes), counts[probeReachable], counts[probeUnreachable], counts[probeNotListed], counts[probeFailed])
}

// writeProbeJSON 输出结构化结果，四种结论各占一个计数，便于脚本判断。
func writeProbeJSON(stdout io.Writer, stderr io.Writer, probes []probeOutcome) int {
	results := make([]probeResult, 0, len(probes))
	counts := map[probeState]int{}
	for _, probe := range probes {
		counts[probe.state]++
		results = append(results, probeResult{Repo: probe.repo, State: string(probe.state), Error: probe.detail})
	}

	payload := map[string]any{
		"checked":     len(probes),
		"reachable":   counts[probeReachable],
		"unreachable": counts[probeUnreachable],
		"notListed":   counts[probeNotListed],
		"failed":      counts[probeFailed],
		"results":     results,
	}
	if code := writeJSON(stdout, stderr, payload); code != exitOK {
		return code
	}
	return probeExitCode(probes)
}
