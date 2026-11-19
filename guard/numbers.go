package guard

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"eucli-deep-wiki-contract-guard/contract"
	"eucli-deep-wiki-contract-guard/harness"
)

// NumberChecks 守「数字一族」：两侧各持一份的同名数字必须逐一等于契约值。
//
// 数字只能从外面观察：服务端按它真收敛、命令行在帮助与「已按上限处理」里
// 说同一个数。因此每个数都在两侧各找一处可观察的落点，而不是抄一遍常量。
func NumberChecks() []Check {
	return []Check{
		{
			ID: "number.tree.depth", Family: contract.FamilyNumbers,
			Title: "目录层数缺省 1、超限收敛到 8，帮助文本写的是同一个数",
			World: harness.KindStandard,
			Run:   checkTreeDepth,
			Guards: []contract.Guardian{
				{Number: "DefaultTreeDepth", Side: contract.Server},
				{Number: "DefaultTreeDepth", Side: contract.Client},
				{Number: "MaxTreeDepth", Side: contract.Server},
				{Number: "MaxTreeDepth", Side: contract.Client},
			},
		},
		{
			ID: "number.find.limit", Family: contract.FamilyNumbers,
			Title: "文件名查找缺省 200、上限 10000，帮助与截断提示写的是同一个数",
			World: harness.KindBulk,
			Run:   checkFindLimit,
			Guards: []contract.Guardian{
				{Number: "DefaultFindLimit", Side: contract.Server},
				{Number: "DefaultFindLimit", Side: contract.Client},
				{Number: "MaxFindLimit", Side: contract.Client},
			},
		},
		{
			ID: "number.search.limits", Family: contract.FamilyNumbers,
			Title: "检索条数上限 100、上下文上限 10，帮助与提示写的是同一个数",
			World: harness.KindBulk,
			Run:   checkSearchLimits,
			Guards: []contract.Guardian{
				{Number: "MaxSearchLimit", Side: contract.Server},
				{Number: "MaxSearchLimit", Side: contract.Client},
				{Number: "MaxSearchContext", Side: contract.Server},
				{Number: "MaxSearchContext", Side: contract.Client},
			},
		},
		{
			ID: "number.lines.behavior", Family: contract.FamilyNumbers,
			Title: "行区间写法两侧解析出同一结果、归一成同一写法",
			World: harness.KindStandard,
			Run:   checkLineRangeBehavior,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}/file", Side: contract.Server},
				{Endpoint: "GET /v1/repos/{name}/file", Side: contract.Client},
			},
		},
		{
			ID: "number.duration.units", Family: contract.FamilyNumbers,
			Title: "耗时字段的名字与单位自洽：接口用纳秒，命令行输出用毫秒",
			World: harness.KindGit,
			Run:   checkDurationUnits,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/repos/{name}/compact", Side: contract.Server},
				{Endpoint: "POST /v1/repos/{name}/compact", Side: contract.Client},
			},
		},
	}
}

// checkTreeDepth 层数：服务端真收敛，命令行在帮助与提示里说同一个数。
func checkTreeDepth(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos/{name}/tree")
	path := endpoint.Route(contract.RepoOnDisk)
	defaultDepth := contract.NumberByKey("DefaultTreeDepth").Value
	maxDepth := contract.NumberByKey("MaxTreeDepth").Value

	object, ok := requireJSON(reporter, endpoint.ID+"（不指定层数）", world.Get(path, nil), endpoint.Status)
	if ok {
		depth, _ := numberOf(object, "depth")
		reporter.Equal(contract.Server, endpoint.ID+" 缺省层数", float64(defaultDepth), depth)
		for _, entry := range arrayField(reporter, endpoint.ID+" 响应字段 entries", object, "entries") {
			item, _ := entry.(map[string]any)
			if level, _ := numberOf(item, "depth"); level != float64(defaultDepth) {
				reporter.Mismatch(contract.Server, endpoint.ID+" 缺省时条目的层级", "全部是 "+strconv.Itoa(defaultDepth), fmt.Sprint(level))
				break
			}
		}
	}

	object, ok = requireJSON(reporter, endpoint.ID+"（层数超限）", world.Get(path, harness.Query("depth", "99")), endpoint.Status)
	if ok {
		depth, _ := numberOf(object, "depth")
		reporter.Equal(contract.Server, endpoint.ID+" 超限时收敛到的层数", float64(maxDepth), depth)
		deepest := 0.0
		deepestPath := ""
		for _, entry := range arrayField(reporter, endpoint.ID+"（层数超限）响应字段 entries", object, "entries") {
			item, _ := entry.(map[string]any)
			if level, _ := numberOf(item, "depth"); level > deepest {
				deepest = level
				deepestPath = textOf(item, "path")
			}
		}
		if deepestPath == "" {
			reporter.Mismatch(contract.Server, endpoint.ID+" 超限时的最深条目", "能展开到嵌套目录", "只有一层")
		}
		if deepest > float64(maxDepth) {
			reporter.Mismatch(contract.Server, endpoint.ID+" 超限时最深层级", "不超过 "+strconv.Itoa(maxDepth), fmt.Sprint(deepest))
		}
	}

	help := world.Command.Help("tree")
	helpText := help.Stdout + help.Stderr
	reporter.Equal(contract.Client, "tree 帮助里写的缺省层数", "(default "+strconv.Itoa(defaultDepth)+")", flagDefaultOf(helpText, "depth"))
	reporter.Equal(contract.Client, "tree 帮助里写的层数上限", "最多 "+strconv.Itoa(maxDepth), substringOrMissing(helpText, "最多 "+strconv.Itoa(maxDepth)))

	outcome := world.Command.Run([]string{"tree", contract.RepoOnDisk, "--depth", "99"})
	reporter.Equal(contract.Client, "tree --depth 99 的退出码", contract.ExitOK, outcome.Exit)
	reporter.Equal(contract.Client, "tree --depth 99 的上限提示",
		"--depth 已按上限 "+strconv.Itoa(maxDepth)+" 处理", substringOrMissing(outcome.Stderr, "--depth 已按上限 "+strconv.Itoa(maxDepth)+" 处理"))
}

// checkFindLimit 文件名查找：服务端缺省 200；命令行的帮助与提示写 200 / 10000。
func checkFindLimit(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos/{name}/find")
	defaultLimit := contract.NumberByKey("DefaultFindLimit").Value
	maxLimit := contract.NumberByKey("MaxFindLimit").Value

	object, ok := requireJSON(reporter, endpoint.ID, world.Get(endpoint.Route(contract.RepoOnDisk), harness.Query("name", "*")), endpoint.Status)
	if ok {
		entries := arrayField(reporter, endpoint.ID+" 响应字段 entries", object, "entries")
		reporter.Equal(contract.Server, endpoint.ID+" 缺省条数", defaultLimit, len(entries))
		truncated, _ := boolOf(object, "truncated")
		reporter.Equal(contract.Server, endpoint.ID+" 缺省时是否被截断", true, truncated)
	}

	help := world.Command.Help("find")
	helpText := help.Stdout + help.Stderr
	reporter.Equal(contract.Client, "find 帮助里写的缺省条数", "(default "+strconv.Itoa(defaultLimit)+")", flagDefaultOf(helpText, "limit"))
	reporter.Equal(contract.Client, "find 帮助里写的条数上限", "上限 "+strconv.Itoa(maxLimit), substringOrMissing(helpText, "上限 "+strconv.Itoa(maxLimit)))

	outcome := world.Command.Run([]string{"find", contract.RepoOnDisk, "*"})
	reporter.Equal(contract.Client, "find 缺省调用的退出码", contract.ExitOK, outcome.Exit)
	notice := "本次最多返回 " + strconv.Itoa(defaultLimit) + " 条，单次最多返回 " + strconv.Itoa(maxLimit) + " 条"
	reporter.Equal(contract.Client, "find 截断提示里的两个数", notice, substringOrMissing(outcome.Stderr, notice))
}

// checkSearchLimits 检索：服务端真收敛；命令行的帮助与提示写同一组数。
func checkSearchLimits(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos/{name}/search")
	maxLimit := contract.NumberByKey("MaxSearchLimit").Value
	maxContext := contract.NumberByKey("MaxSearchContext").Value

	object, ok := requireJSON(reporter, endpoint.ID+"（limit 超限）", world.Get(
		endpoint.Route(contract.RepoOnDisk),
		harness.Query("q", "needle", "path", "matches", "limit", "1000"),
	), endpoint.Status)
	if ok {
		matches := arrayField(reporter, endpoint.ID+" 响应字段 matches", object, "matches")
		reporter.Equal(contract.Server, endpoint.ID+" 超限时的命中条数", maxLimit, len(matches))
		truncated, _ := boolOf(object, "truncated")
		reporter.Equal(contract.Server, endpoint.ID+" 超限时是否被截断", true, truncated)
	}

	object, ok = requireJSON(reporter, endpoint.ID+"（context 超限）", world.Get(
		endpoint.Route(contract.RepoOnDisk),
		harness.Query("q", "needle", "path", "context", "context", "99"),
	), endpoint.Status)
	if ok {
		matches := arrayField(reporter, endpoint.ID+"（context 超限）响应字段 matches", object, "matches")
		if len(matches) != 1 {
			reporter.Mismatch(contract.Server, endpoint.ID+" 在 context 目录里的命中条数", "1", fmt.Sprint(len(matches)))
		} else {
			match, _ := matches[0].(map[string]any)
			before := arrayField(reporter, endpoint.ID+" 命中的 before", match, "before")
			after := arrayField(reporter, endpoint.ID+" 命中的 after", match, "after")
			reporter.Equal(contract.Server, endpoint.ID+" 超限时 before 行数", maxContext, len(before))
			reporter.Equal(contract.Server, endpoint.ID+" 超限时 after 行数", maxContext, len(after))
		}
	}

	help := world.Command.Help("search")
	helpText := help.Stdout + help.Stderr
	reporter.Equal(contract.Client, "search 帮助里写的条数上限", "单次上限 "+strconv.Itoa(maxLimit), substringOrMissing(helpText, "单次上限 "+strconv.Itoa(maxLimit)))
	reporter.Equal(contract.Client, "search 帮助里写的上下文上限", "最多 "+strconv.Itoa(maxContext), substringOrMissing(helpText, "最多 "+strconv.Itoa(maxContext)))

	outcome := world.Command.Run([]string{"search", contract.RepoOnDisk, "needle", "--limit", "1000", "--context", "99"})
	reporter.Equal(contract.Client, "search 超限调用的退出码", contract.ExitOK, outcome.Exit)
	reporter.Equal(contract.Client, "search 的条数上限提示",
		"--limit 已按上限 "+strconv.Itoa(maxLimit)+" 处理", substringOrMissing(outcome.Stderr, "--limit 已按上限 "+strconv.Itoa(maxLimit)+" 处理"))
	reporter.Equal(contract.Client, "search 的上下文上限提示",
		"--context 已按上限 "+strconv.Itoa(maxContext)+" 处理", substringOrMissing(outcome.Stderr, "--context 已按上限 "+strconv.Itoa(maxContext)+" 处理"))
}

// checkLineRangeBehavior 行区间：同一张写法表在两侧得到同一结果。
func checkLineRangeBehavior(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos/{name}/file")
	path := endpoint.Route(contract.RepoOnDisk)

	for _, lineCase := range contract.LineRangeCases {
		fact := fmt.Sprintf("行区间写法 %q", lineCase.Input)
		if !lineCase.Valid {
			message := world.Get(path, harness.Query("path", "README.md", "lines", lineCase.Input))
			reporter.Outcome(fact+" 在服务端", message, 400, "invalid_lines")

			outcome := world.Command.Run([]string{"get", contract.RepoOnDisk, "README.md", "--lines", lineCase.Input})
			reporter.Equal(contract.Client, fact+" 在命令行的退出码", contract.ExitUsage, outcome.Exit)
			if len(outcome.Requests) != 0 {
				reporter.Mismatch(contract.Client, fact+" 在命令行被就地拒绝", "一个请求都不发", fmt.Sprint(len(outcome.Requests))+" 个请求")
			}
			continue
		}

		first, last := expectedSpan(lineCase, harness.ReadmeLines)
		message := world.Get(path, harness.Query("path", "README.md", "lines", lineCase.Input))
		reporter.Equal(contract.Server, fact+" 在服务端的首行", strconv.Itoa(first), message.Header.Get(contract.HeaderFirstLine))
		reporter.Equal(contract.Server, fact+" 在服务端的末行", strconv.Itoa(last), message.Header.Get(contract.HeaderLastLine))

		outcome := world.Command.Run([]string{"get", contract.RepoOnDisk, "README.md", "--lines", lineCase.Input})
		reporter.Equal(contract.Client, fact+" 在命令行的退出码", contract.ExitOK, outcome.Exit)
		matched := requestsFor(outcome.Requests, endpoint)
		if len(matched) == 0 {
			reporter.Mismatch(contract.Client, fact+" 在命令行发出的请求", endpoint.ID, "没有发出")
			continue
		}
		if lineCase.Normalized == "" {
			if matched[0].HasQuery("lines") {
				reporter.Mismatch(contract.Client, fact+" 归一后写进查询串的写法", "不出现 lines", matched[0].Query.Get("lines"))
			}
			continue
		}
		reporter.Equal(contract.Client, fact+" 归一后写进查询串的写法", lineCase.Normalized, matched[0].Query.Get("lines"))
	}
}

// checkDurationUnits 耗时字段的名字与单位必须自洽。
func checkDurationUnits(world *harness.World, reporter *Reporter) {
	// 服务端：瘦身结果的耗时叫 durationMs（毫秒）。
	endpoint := contract.MustEndpoint("POST /v1/repos/{name}/compact")
	message := world.PostJSON(endpoint.Route(contract.RepoGitFullHistory), nil, nil)
	object, ok := requireJSON(reporter, endpoint.ID, message, endpoint.Status)
	if ok {
		reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)
		if _, present := object[contract.FieldCompactDuration]; !present {
			reporter.Mismatch(contract.Server, endpoint.ID+" 的耗时字段", contract.FieldCompactDuration+"（毫秒）", "缺失")
		}
		reporter.FieldAbsent(contract.Server, endpoint.ID, object, "duration")
	}

	// 命令行：瘦身的 JSON 输出沿用同一个字段名。
	outcome := world.Command.Run([]string{"repo", "compact", "--dry-run", contract.RepoGitFullHistory, "--json"})
	reporter.Equal(contract.Client, "repo compact --json 的退出码", contract.ExitOK, outcome.Exit)
	var compactPayload struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal([]byte(outcome.Stdout), &compactPayload); err != nil {
		reporter.Mismatch(contract.Client, "repo compact --json 的输出", "JSON 对象", outcome.Stdout)
	} else if len(compactPayload.Results) > 0 {
		if _, present := compactPayload.Results[0][contract.FieldCompactDuration]; !present {
			reporter.Mismatch(contract.Client, "repo compact --json 的耗时字段", contract.FieldCompactDuration+"（毫秒）", "缺失")
		}
	}

	// 命令行：接口给的是纳秒，命令行输出必须换算成毫秒再起名。
	stub := harness.NewStub(200, map[string]any{
		"name": "synthetic", "action": "checked", "duration": 1500000000,
	})
	defer stub.Close()

	syncOutcome := world.Command.Run([]string{"sync", "synthetic", "--json"}, "--server", stub.URL())
	reporter.Equal(contract.Client, "sync --json 的退出码", contract.ExitOK, syncOutcome.Exit)
	var syncPayload map[string]any
	if err := json.Unmarshal([]byte(syncOutcome.Stdout), &syncPayload); err != nil {
		reporter.Mismatch(contract.Client, "sync --json 的输出", "JSON 对象", syncOutcome.Stdout)
		return
	}
	if _, present := syncPayload[contract.FieldSyncDuration]; present {
		reporter.Mismatch(contract.Client, "sync --json 的耗时字段", contract.FieldCLIDuration+"（毫秒）", "沿用了接口的 "+contract.FieldSyncDuration)
	}
	milliseconds, _ := syncPayload[contract.FieldCLIDuration].(float64)
	reporter.Equal(contract.Client, "sync --json 的耗时数值（1.5 秒 → 毫秒）", float64(1500), milliseconds)
}

// expectedSpan 由契约里的行区间算出文件响应里应有的首末行。
func expectedSpan(lineCase contract.LineCase, totalLines int) (int, int) {
	first := 1
	if lineCase.Start > 0 {
		first = lineCase.Start
	}
	last := totalLines
	if lineCase.End > 0 && lineCase.End < totalLines {
		last = lineCase.End
	}
	if first > totalLines {
		return totalLines + 1, totalLines
	}
	if last < first {
		return first, first - 1
	}
	return first, last
}

// flagDefaultOf 从帮助文本里取一个参数声明的缺省值，形如「(default 1)」。
//
// 标准库把参数声明与它的说明拆成两行：说明（含缺省值）紧随在声明行之后，
// 因此这里从声明行开始往下读，直到遇到下一处缺省值或读完全文。
func flagDefaultOf(helpText string, name string) string {
	lines := strings.Split(helpText, "\n")
	declaration := "-" + name
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, declaration) {
			continue
		}
		if rest := trimmed[len(declaration):]; rest != "" && rest[0] != ' ' && rest[0] != '\t' {
			continue
		}
		for _, following := range lines[index:] {
			position := strings.Index(following, "(default ")
			if position < 0 {
				continue
			}
			closing := strings.Index(following[position:], ")")
			if closing > 0 {
				return following[position : position+closing+1]
			}
		}
		return "（这个参数没有缺省值）"
	}
	return "（帮助里没有这个参数）"
}

// substringOrMissing 断言帮助或提示里出现了这段文字。
func substringOrMissing(text string, want string) string {
	if strings.Contains(text, want) {
		return want
	}
	return "（没有出现「" + want + "」）"
}
