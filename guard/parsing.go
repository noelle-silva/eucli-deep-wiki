package guard

import (
	"fmt"
	"os"
	"strings"

	"eucli-deep-wiki-contract-guard/contract"
	"eucli-deep-wiki-contract-guard/harness"
)

// ParseChecks 守「命令行真的解析了字段」：用命令行自己的命令跑真实请求，
// 再看它输出的结构化结果。字段名改歪时，这里会因为「取不到值」而变红。
func ParseChecks() []Check {
	return []Check{
		{
			ID: "cli.parse.repos", Family: contract.FamilyCommands,
			Title: "名单总览的每个字段都被命令行解析成可用事实",
			World: harness.KindStandard,
			Run:   checkParseRepos,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.repo_list", Family: contract.FamilyCommands,
			Title: "名单视图把地址、档位、落盘与现测体积都读出来",
			World: harness.KindStandard,
			Run:   checkParseRepoList,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.file", Family: contract.FamilyCommands,
			Title: "文件接口的正文与响应头都被命令行解析成同名字段",
			World: harness.KindStandard,
			Run:   checkParseFile,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}/file", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.tree", Family: contract.FamilyCommands,
			Title: "目录结果的字段被命令行解析成同名字段",
			World: harness.KindStandard,
			Run:   checkParseTree,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}/tree", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.search", Family: contract.FamilyCommands,
			Title: "检索结果的字段被命令行解析成同名字段",
			World: harness.KindStandard,
			Run:   checkParseSearch,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}/search", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.find", Family: contract.FamilyCommands,
			Title: "文件名查找的字段被命令行解析成同名字段",
			World: harness.KindStandard,
			Run:   checkParseFind,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}/find", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.add", Family: contract.FamilyCommands,
			Title: "加入名单的结果被命令行解析成同名字段",
			World: harness.KindStandard,
			Run:   checkParseAdd,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/repos", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.remove", Family: contract.FamilyCommands,
			Title: "移出名单的两个磁盘事实被命令行分开表达",
			World: harness.KindStandard,
			Run:   checkParseRemove,
			Guards: []contract.Guardian{
				{Endpoint: "DELETE /v1/repos/{name}", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.unblock", Family: contract.FamilyCommands,
			Title: "解封结果与解除前的状态都被命令行解析出来",
			World: harness.KindStandard,
			Run:   checkParseUnblock,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/repos/{name}/unblock", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.compact", Family: contract.FamilyCommands,
			Title: "瘦身结果的字段与两种动作结论都被命令行解析出来",
			World: harness.KindGit,
			Run:   checkParseCompact,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/repos/{name}/compact", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.cache", Family: contract.FamilyCommands,
			Title: "缓存清单与清理结果被命令行解析成同名字段",
			World: harness.KindStandard,
			Run:   checkParseCache,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/cache", Side: contract.Client},
				{Endpoint: "DELETE /v1/cache", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.reload", Family: contract.FamilyCommands,
			Title: "重载结果被命令行解析成同名字段",
			World: harness.KindStandard,
			Run:   checkParseReload,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/reload", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.status", Family: contract.FamilyCommands,
			Title: "状态快照被命令行解析出关键事实",
			World: harness.KindStandard,
			Run:   checkParseStatus,
			Guards: []contract.Guardian{
				{Endpoint: "GET /status", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.state", Family: contract.FamilyCommands,
			Title: "状态明细被命令行解析成「停止同步」这一条人话",
			World: harness.KindStandard,
			Run:   checkParseState,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/state", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.check", Family: contract.FamilyCommands,
			Title: "探测结果把「不在名单」与「不可达」分开表达",
			World: harness.KindStandard,
			Run:   checkParseCheck,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/repos/{name}/check", Side: contract.Client},
			},
		},
		{
			ID: "cli.parse.download", Family: contract.FamilyCommands,
			Title: "下载类命令把正文真的落盘，半截失败不落盘",
			World: harness.KindStandard,
			Run:   checkParseDownload,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/archive", Side: contract.Client},
				{Endpoint: "GET /v1/repos/{name}/bundle", Side: contract.Client},
			},
		},
	}
}

// checkParseRepos 名单总览逐字段核对。
func checkParseRepos(world *harness.World, reporter *Reporter) {
	outcome := world.Command.Run([]string{"repos", "--json"})
	payload, err := jsonObject(outcome)
	if err != nil {
		reporter.Mismatch(contract.Client, "repos --json", err.Error(), outcome.Describe())
		return
	}
	items, ok := payload["repositories"].([]any)
	if !ok {
		reporter.Mismatch(contract.Client, "repos --json 的 repositories", "数组", fmt.Sprint(payload["repositories"]))
		return
	}
	reporter.Equal(contract.Client, "repos --json 的仓库条数", 3, len(items))

	alpha := requireRepo(reporter, items, contract.RepoOnDisk)
	if alpha == nil {
		return
	}
	reporter.Equal(contract.Client, "命令行解析 alpha 的 branch", "main", textOf(alpha, "branch"))
	reporter.Equal(contract.Client, "命令行解析 alpha 的 revision", harness.AlphaMainRevision, textOf(alpha, "revision"))
	reporter.Equal(contract.Client, "命令行解析 alpha 的 shallow", true, flagOf(alpha, "shallow"))
	reporter.Equal(contract.Client, "命令行解析 alpha 的 gitBytes（上次同步时记录）", float64(1234), numberValue(alpha, "gitBytes"))
	reporter.Equal(contract.Client, "命令行解析 alpha 的 blocked", true, flagOf(alpha, "blocked"))
	reporter.Equal(contract.Client, "命令行解析 alpha 的 blockedSize", float64(4096), numberValue(alpha, "blockedSize"))

	gamma := requireRepo(reporter, items, contract.RepoFullHistory)
	if gamma != nil {
		reporter.Equal(contract.Client, "命令行解析 gamma 的 branch", "dev", textOf(gamma, "branch"))
	}
}

// checkParseRepoList 名单视图：地址、档位、落盘与现测体积。
func checkParseRepoList(world *harness.World, reporter *Reporter) {
	outcome := world.Command.Run([]string{"repo", "list", "--json"})
	payload, err := jsonObject(outcome)
	if err != nil {
		reporter.Mismatch(contract.Client, "repo list --json", err.Error(), outcome.Describe())
		return
	}
	items, ok := payload["repositories"].([]any)
	if !ok {
		reporter.Mismatch(contract.Client, "repo list --json 的 repositories", "数组", fmt.Sprint(payload["repositories"]))
		return
	}

	alpha := requireRepo(reporter, items, contract.RepoOnDisk)
	if alpha != nil {
		reporter.Equal(contract.Client, "命令行解析 alpha 的 url", "https://example.com/alpha.git", textOf(alpha, "url"))
		reporter.Equal(contract.Client, "命令行解析 alpha 的 history", contract.HistoryShallow, textOf(alpha, "history"))
		reporter.Equal(contract.Client, "命令行解析 alpha 的 effectiveHistory", contract.HistoryShallow, textOf(alpha, "effectiveHistory"))
		reporter.Equal(contract.Client, "命令行解析 alpha 的 onDisk", true, flagOf(alpha, "onDisk"))
		if _, present := alpha["measuredBytes"]; present {
			reporter.Mismatch(contract.Client, "不测量时 alpha 的 measuredBytes", "不出现", "出现了")
		}
	}

	beta := requireRepo(reporter, items, contract.RepoDeclaredOnly)
	if beta != nil {
		reporter.Equal(contract.Client, "命令行解析 beta 的 onDisk", false, flagOf(beta, "onDisk"))
		reporter.Equal(contract.Client, "命令行解析 beta 声明的 history", "", textOf(beta, "history"))
		reporter.Equal(contract.Client, "命令行解析 beta 生效的 effectiveHistory", contract.HistoryShallow, textOf(beta, "effectiveHistory"))
	}

	measured := world.Command.Run([]string{"repo", "list", "--sizes", "--json"})
	payload, err = jsonObject(measured)
	if err != nil {
		reporter.Mismatch(contract.Client, "repo list --sizes --json", err.Error(), measured.Describe())
		return
	}
	items, _ = payload["repositories"].([]any)
	alpha = requireRepo(reporter, items, contract.RepoOnDisk)
	if alpha != nil {
		if _, present := alpha["measuredBytes"]; !present {
			reporter.Mismatch(contract.Client, "测量时 alpha 的 measuredBytes", "出现", "缺失")
		} else {
			actual, sizeErr := harness.GitDirectorySize(world.RepoPath(contract.RepoOnDisk))
			if sizeErr == nil {
				reporter.Equal(contract.Client, "命令行解析 alpha 的 measuredBytes", float64(actual), numberValue(alpha, "measuredBytes"))
			}
		}
		reporter.Equal(contract.Client, "测量时 alpha 的 gitBytes 不被顶替", float64(1234), numberValue(alpha, "gitBytes"))
	}
	beta = requireRepo(reporter, items, contract.RepoDeclaredOnly)
	if beta != nil {
		if _, present := beta["measuredBytes"]; present {
			reporter.Mismatch(contract.Client, "未落盘仓库的 measuredBytes", "不出现", "出现了")
		}
	}
}

// checkParseFile 文件接口的正文与头。
func checkParseFile(world *harness.World, reporter *Reporter) {
	outcome := world.Command.Run([]string{"get", contract.RepoOnDisk, "README.md", "--lines", "3-5", "--json"})
	payload, err := jsonObject(outcome)
	if err != nil {
		reporter.Mismatch(contract.Client, "get --json", err.Error(), outcome.Describe())
		return
	}
	expected := strings.Join(strings.SplitAfter(harness.ReadmeContent, "\n")[2:5], "")
	reporter.Equal(contract.Client, "命令行解析的 content", expected, textOf(payload, "content"))
	reporter.Equal(contract.Client, "命令行解析的 revision", harness.AlphaMainRevision, textOf(payload, "revision"))
	reporter.Equal(contract.Client, "命令行解析的 size", float64(len(harness.ReadmeContent)), numberValue(payload, "size"))
	reporter.Equal(contract.Client, "命令行解析的 returnedBytes", float64(len(expected)), numberValue(payload, "returnedBytes"))
	reporter.Equal(contract.Client, "命令行解析的 totalLines", float64(harness.ReadmeLines), numberValue(payload, "totalLines"))
	reporter.Equal(contract.Client, "命令行解析的 firstLine", float64(3), numberValue(payload, "firstLine"))
	reporter.Equal(contract.Client, "命令行解析的 lastLine", float64(5), numberValue(payload, "lastLine"))

	firstLine := firstLineOf(harness.ReadmeContent)
	truncated := world.Command.Run([]string{"get", contract.RepoOnDisk, "README.md", "--max-bytes", itoa(len(firstLine) + 9), "--json"})
	payload, err = jsonObject(truncated)
	if err != nil {
		reporter.Mismatch(contract.Client, "get --max-bytes --json", err.Error(), truncated.Describe())
		return
	}
	reporter.Equal(contract.Client, "命令行解析的截断标记", contract.TruncatedByBytes, textOf(payload, "truncated"))
	reporter.Equal(contract.Client, "命令行解析的截断正文", firstLine, textOf(payload, "content"))
	reporter.Equal(contract.Client, "命令行解析的截断后末行", float64(1), numberValue(payload, "lastLine"))
}

// checkParseTree 目录结果。
func checkParseTree(world *harness.World, reporter *Reporter) {
	outcome := world.Command.Run([]string{"tree", contract.RepoOnDisk, "docs", "--depth", "2", "--json"})
	payload, err := jsonObject(outcome)
	if err != nil {
		reporter.Mismatch(contract.Client, "tree --json", err.Error(), outcome.Describe())
		return
	}
	reporter.Equal(contract.Client, "命令行解析的 repo", contract.RepoOnDisk, textOf(payload, "repo"))
	reporter.Equal(contract.Client, "命令行解析的 path", "docs", textOf(payload, "path"))
	reporter.Equal(contract.Client, "命令行解析的 depth", float64(2), numberValue(payload, "depth"))
	reporter.Equal(contract.Client, "命令行解析的 revision", harness.AlphaMainRevision, textOf(payload, "revision"))
	entries, ok := payload["entries"].([]any)
	if !ok || len(entries) < 2 {
		reporter.Mismatch(contract.Client, "命令行解析出的目录项", "至少 2 条", fmt.Sprint(payload["entries"]))
		return
	}
	for _, entry := range entries {
		item, _ := entry.(map[string]any)
		if textOf(item, "name") == "" || textOf(item, "path") == "" || textOf(item, "type") == "" {
			reporter.Mismatch(contract.Client, "命令行解析出的目录项", "name/path/type 都不为空", fmt.Sprint(item))
			break
		}
	}
}

// checkParseSearch 检索结果。
func checkParseSearch(world *harness.World, reporter *Reporter) {
	outcome := world.Command.Run([]string{"search", contract.RepoOnDisk, "needle", "--context", "1", "--limit", "10", "--json"})
	payload, err := jsonObject(outcome)
	if err != nil {
		reporter.Mismatch(contract.Client, "search --json", err.Error(), outcome.Describe())
		return
	}
	reporter.Equal(contract.Client, "命令行解析的 repo", contract.RepoOnDisk, textOf(payload, "repo"))
	reporter.Equal(contract.Client, "命令行解析的 query", "needle", textOf(payload, "query"))
	reporter.Equal(contract.Client, "命令行解析的 revision", harness.AlphaMainRevision, textOf(payload, "revision"))
	matches, ok := payload["matches"].([]any)
	if !ok || len(matches) != 2 {
		reporter.Mismatch(contract.Client, "命令行解析的命中条数", "2", fmt.Sprint(payload["matches"]))
		return
	}
	first, _ := matches[0].(map[string]any)
	reporter.Equal(contract.Client, "命令行解析的首处命中路径", "README.md", textOf(first, "path"))
	reporter.Equal(contract.Client, "命令行解析的首处命中行号", float64(4), numberValue(first, "line"))
	before, _ := first["before"].([]any)
	reporter.Equal(contract.Client, "命令行解析的首处命中上下文行数", 1, len(before))
	reporter.Equal(contract.Client, "命令行解析的 scannedFiles", float64(harness.AlphaTextFiles), numberValue(payload, "scannedFiles"))
}

// checkParseFind 文件名查找结果。
func checkParseFind(world *harness.World, reporter *Reporter) {
	outcome := world.Command.Run([]string{"find", contract.RepoOnDisk, "*.md", "--json"})
	payload, err := jsonObject(outcome)
	if err != nil {
		reporter.Mismatch(contract.Client, "find --json", err.Error(), outcome.Describe())
		return
	}
	reporter.Equal(contract.Client, "命令行解析的 pattern", "*.md", textOf(payload, "pattern"))
	reporter.Equal(contract.Client, "命令行解析的 repo", contract.RepoOnDisk, textOf(payload, "repo"))
	entries, ok := payload["entries"].([]any)
	if !ok || len(entries) != 2 {
		reporter.Mismatch(contract.Client, "命令行解析的条目数", "2", fmt.Sprint(payload["entries"]))
		return
	}
	for _, entry := range entries {
		item, _ := entry.(map[string]any)
		if textOf(item, "name") == "" || textOf(item, "path") == "" {
			reporter.Mismatch(contract.Client, "命令行解析的查找条目", "name/path 都不为空", fmt.Sprint(item))
			break
		}
	}
}

// checkParseAdd 加入名单的结果。
func checkParseAdd(world *harness.World, reporter *Reporter) {
	outcome := world.Command.Run([]string{"repo", "add", "--branch", "release", "--history", "full", "--json",
		"https://example.com/" + contract.RepoToAdd + ".git"})
	payload, err := jsonObject(outcome)
	if err != nil {
		reporter.Mismatch(contract.Client, "repo add --json", err.Error(), outcome.Describe())
		return
	}
	added, ok := payload["added"].([]any)
	if !ok || len(added) != 1 {
		reporter.Mismatch(contract.Client, "命令行解析的 added 条数", "1", fmt.Sprint(payload["added"]))
		return
	}
	item, _ := added[0].(map[string]any)
	reporter.Equal(contract.Client, "命令行解析的 added.name", contract.RepoToAdd, textOf(item, "name"))
	reporter.Equal(contract.Client, "命令行解析的 added.url", "https://example.com/delta.git", textOf(item, "url"))
	reporter.Equal(contract.Client, "命令行解析的 added.branch", "release", textOf(item, "branch"))
	reporter.Equal(contract.Client, "命令行解析的 added.history", contract.HistoryFull, textOf(item, "history"))
	reporter.Equal(contract.Client, "命令行解析的 added.effectiveHistory", contract.HistoryFull, textOf(item, "effectiveHistory"))
	reporter.Equal(contract.Client, "命令行解析的 repositories", float64(4), numberValue(payload, "repositories"))

	reloaded, ok := payload["reloaded"].(map[string]any)
	if !ok {
		reporter.Mismatch(contract.Client, "命令行解析的 reloaded", "对象（缺省 reload 为真）", fmt.Sprint(payload["reloaded"]))
		return
	}
	reporter.Equal(contract.Client, "命令行解析的 reloaded.indexReloaded", true, flagOf(reloaded, "indexReloaded"))
	reporter.Equal(contract.Client, "命令行解析的 reloaded.configReloaded", true, flagOf(reloaded, "configReloaded"))
	reporter.Equal(contract.Client, "命令行解析的 reloaded.repositories", float64(4), numberValue(reloaded, "repositories"))
}

// checkParseRemove 移出名单：两个磁盘事实分开。
func checkParseRemove(world *harness.World, reporter *Reporter) {
	withData := world.Command.Run([]string{"repo", "rm", contract.RepoFullHistory, "--json"})
	payload, err := jsonObject(withData)
	if err != nil {
		reporter.Mismatch(contract.Client, "repo rm --json", err.Error(), withData.Describe())
		return
	}
	reporter.Equal(contract.Client, "命令行解析的 name", contract.RepoFullHistory, textOf(payload, "name"))
	reporter.Equal(contract.Client, "命令行解析的 removed", true, flagOf(payload, "removed"))
	reporter.Equal(contract.Client, "命令行解析的 dataExisted", true, flagOf(payload, "dataExisted"))
	reporter.Equal(contract.Client, "命令行解析的 dataKept（缺省保留数据）", true, flagOf(payload, "dataKept"))

	withoutData := world.Command.Run([]string{"repo", "rm", "--drop-data", contract.RepoOnDisk, "--json"})
	payload, err = jsonObject(withoutData)
	if err != nil {
		reporter.Mismatch(contract.Client, "repo rm --drop-data --json", err.Error(), withoutData.Describe())
		return
	}
	reporter.Equal(contract.Client, "命令行解析的 dataExisted（--drop-data）", true, flagOf(payload, "dataExisted"))
	reporter.Equal(contract.Client, "命令行解析的 dataKept（--drop-data）", false, flagOf(payload, "dataKept"))
}

// checkParseUnblock 解封结果。
func checkParseUnblock(world *harness.World, reporter *Reporter) {
	outcome := world.Command.Run([]string{"repo", "unblock", contract.RepoOnDisk, "--json"})
	payload, err := jsonObject(outcome)
	if err != nil {
		reporter.Mismatch(contract.Client, "repo unblock --json", err.Error(), outcome.Describe())
		return
	}
	reporter.Equal(contract.Client, "命令行解析的 name", contract.RepoOnDisk, textOf(payload, "name"))
	reporter.Equal(contract.Client, "命令行解析的 cleared", true, flagOf(payload, "cleared"))
	previous, ok := payload["previous"].(map[string]any)
	if !ok {
		reporter.Mismatch(contract.Client, "命令行解析的 previous", "对象", fmt.Sprint(payload["previous"]))
		return
	}
	reporter.Equal(contract.Client, "命令行解析的 previous.blocked", true, flagOf(previous, "blocked"))
	reporter.Equal(contract.Client, "命令行解析的 previous.blockedSize", float64(4096), numberValue(previous, "blockedSize"))
	reporter.Equal(contract.Client, "命令行解析的 previous.consecutiveFailures", float64(3), numberValue(previous, "consecutiveFailures"))
	reporter.Equal(contract.Client, "命令行解析的 previous.lastError", "远端超时", textOf(previous, "lastError"))
}

// checkParseCompact 瘦身结果：字段与两种动作结论。
func checkParseCompact(world *harness.World, reporter *Reporter) {
	outcome := world.Command.Run([]string{"repo", "compact", "--dry-run", contract.RepoGitShallow, "--json"})
	payload, err := jsonObject(outcome)
	if err != nil {
		reporter.Mismatch(contract.Client, "repo compact --json", err.Error(), outcome.Describe())
		return
	}
	results, ok := payload["results"].([]any)
	if !ok || len(results) != 1 {
		reporter.Mismatch(contract.Client, "命令行解析的 results 条数", "1", fmt.Sprint(payload["results"]))
		return
	}
	item, _ := results[0].(map[string]any)
	reporter.Equal(contract.Client, "命令行解析的 compact.name", contract.RepoGitShallow, textOf(item, "name"))
	reporter.Equal(contract.Client, "命令行解析的 compact.action（本来就是浅克隆）", contract.ActionAlreadyShallow, textOf(item, "action"))
	before, ok := item["before"].(map[string]any)
	if !ok {
		reporter.Mismatch(contract.Client, "命令行解析的 compact.before", "对象", fmt.Sprint(item["before"]))
	} else {
		reporter.Equal(contract.Client, "命令行解析的 compact.before.shallow", true, flagOf(before, "shallow"))
		if _, present := before["gitBytes"]; !present {
			reporter.Mismatch(contract.Client, "命令行解析的 compact.before.gitBytes", "存在", "缺失")
		}
	}
	if _, present := item["durationMs"]; !present {
		reporter.Mismatch(contract.Client, "命令行解析的 compact.durationMs", "存在", "缺失")
	}

	fullHistory := world.Command.Run([]string{"repo", "compact", "--dry-run", contract.RepoGitFullHistory, "--json"})
	payload, err = jsonObject(fullHistory)
	if err != nil {
		reporter.Mismatch(contract.Client, "repo compact（完整历史）--json", err.Error(), fullHistory.Describe())
		return
	}
	results, _ = payload["results"].([]any)
	if len(results) == 1 {
		item, _ := results[0].(map[string]any)
		reporter.Equal(contract.Client, "命令行解析的 compact.action（要求完整历史）", contract.ActionSkippedFullHistory, textOf(item, "action"))
		reporter.Equal(contract.Client, "命令行解析的 compact.branch", "main", textOf(item, "branch"))
	}
}

// checkParseCache 缓存清单与清理结果。
func checkParseCache(world *harness.World, reporter *Reporter) {
	listing := world.Command.Run([]string{"cache", "list", "--json"})
	payload, err := jsonObject(listing)
	if err != nil {
		reporter.Mismatch(contract.Client, "cache list --json", err.Error(), listing.Describe())
		return
	}
	files, ok := payload["files"].([]any)
	if !ok || len(files) != 3 {
		reporter.Mismatch(contract.Client, "命令行解析的缓存产物条数", "3", fmt.Sprint(payload["files"]))
		return
	}
	sum := 0.0
	for _, file := range files {
		item, _ := file.(map[string]any)
		if textOf(item, "name") == "" || textOf(item, "repo") == "" || textOf(item, "revision") == "" {
			reporter.Mismatch(contract.Client, "命令行解析的缓存条目", "name/repo/revision 都不为空", fmt.Sprint(item))
			break
		}
		sum += numberValue(item, "bytes")
	}
	reporter.Equal(contract.Client, "命令行解析的 totalBytes", sum, numberValue(payload, "totalBytes"))

	cleared := world.Command.Run([]string{"cache", "clean", "--yes", contract.RepoFullHistory, "--json"})
	payload, err = jsonObject(cleared)
	if err != nil {
		reporter.Mismatch(contract.Client, "cache clean --json", err.Error(), cleared.Describe())
		return
	}
	reporter.Equal(contract.Client, "命令行解析的 removed", float64(1), numberValue(payload, "removed"))
	if numberValue(payload, "bytes") <= 0 {
		reporter.Mismatch(contract.Client, "命令行解析的 bytes", "大于 0", fmt.Sprint(payload["bytes"]))
	}
	skipped, ok := payload["skipped"].([]any)
	if !ok || len(skipped) == 0 {
		reporter.Mismatch(contract.Client, "命令行解析的 skipped", "逐条说明", fmt.Sprint(payload["skipped"]))
		return
	}
	for _, entry := range skipped {
		item, _ := entry.(map[string]any)
		if textOf(item, "name") == "" || textOf(item, "reason") == "" {
			reporter.Mismatch(contract.Client, "命令行解析的 skipped 条目", "name 与 reason 都不为空", fmt.Sprint(item))
			break
		}
	}
}

// checkParseReload 重载结果。
func checkParseReload(world *harness.World, reporter *Reporter) {
	outcome := world.Command.Run([]string{"reload", "--json"})
	payload, err := jsonObject(outcome)
	if err != nil {
		reporter.Mismatch(contract.Client, "reload --json", err.Error(), outcome.Describe())
		return
	}
	reporter.Equal(contract.Client, "命令行解析的 configReloaded", true, flagOf(payload, "configReloaded"))
	reporter.Equal(contract.Client, "命令行解析的 indexReloaded", true, flagOf(payload, "indexReloaded"))
	reporter.Equal(contract.Client, "命令行解析的 repositories", float64(3), numberValue(payload, "repositories"))
	reporter.Equal(contract.Client, "命令行解析的 error", "", textOf(payload, "error"))
	if textOf(payload, "at") == "" {
		reporter.Mismatch(contract.Client, "命令行解析的 at", "非空时间", "空")
	}
	if _, present := payload["needsRestart"]; !present {
		reporter.Mismatch(contract.Client, "命令行解析的 needsRestart", "存在", "缺失")
	}
}

// checkParseStatus 状态快照。
func checkParseStatus(world *harness.World, reporter *Reporter) {
	outcome := world.Command.Run([]string{"status", "--json"})
	payload, err := jsonObject(outcome)
	if err != nil {
		reporter.Mismatch(contract.Client, "status --json", err.Error(), outcome.Describe())
		return
	}
	reporter.Equal(contract.Client, "命令行解析的 repositoryCount", float64(3), numberValue(payload, "repositoryCount"))
	reporter.Equal(contract.Client, "命令行解析的 syncInterval", "6h", textOf(payload, "syncInterval"))
	reporter.Equal(contract.Client, "命令行解析的 defaultHistory", contract.HistoryShallow, textOf(payload, "defaultHistory"))
	if _, present := payload["running"]; !present {
		reporter.Mismatch(contract.Client, "命令行解析的 running", "存在", "缺失")
	}
}

// checkParseState 状态明细：命令行把「被拦住的仓库与体积」读成人话。
func checkParseState(world *harness.World, reporter *Reporter) {
	outcome := world.Command.Run([]string{"status", "--json"})
	// status --json 只回快照原文；明细由状态命令的第二段渲染给出，因此这里跑一次人读形态。
	human := world.Command.Run([]string{"status"})
	blockedLine := "停止同步: 1 个仓库"
	if !strings.Contains(human.Stdout, blockedLine) {
		reporter.Mismatch(contract.Client, "status 的停止同步段", blockedLine, trimForMessage(human.Stdout))
	}
	sizeLine := contract.RepoOnDisk + " (4.0 KB)"
	if !strings.Contains(human.Stdout, sizeLine) {
		reporter.Mismatch(contract.Client, "status 的停止同步条目", sizeLine, trimForMessage(human.Stdout))
	}

	// 明细里的几项事实也必须被读出来：repositoryCount 之外，状态快照的 results 结构存在。
	if _, err := jsonObject(outcome); err != nil {
		reporter.Mismatch(contract.Client, "status --json", err.Error(), outcome.Describe())
	}
}

// checkParseCheck 探测结果：四种结论分开。
func checkParseCheck(world *harness.World, reporter *Reporter) {
	outcome := world.Command.Run([]string{"repo", "check", contract.RepoAbsent, "--json"})
	payload, err := jsonObject(outcome)
	if err != nil {
		reporter.Mismatch(contract.Client, "repo check --json", err.Error(), outcome.Describe())
		return
	}
	reporter.Equal(contract.Client, "命令行解析的 checked", float64(1), numberValue(payload, "checked"))
	reporter.Equal(contract.Client, "命令行解析的 notListed", float64(1), numberValue(payload, "notListed"))
	reporter.Equal(contract.Client, "命令行解析的 unreachable", float64(0), numberValue(payload, "unreachable"))
	results, ok := payload["results"].([]any)
	if !ok || len(results) != 1 {
		reporter.Mismatch(contract.Client, "命令行解析的 results", "1 条", fmt.Sprint(payload["results"]))
		return
	}
	item, _ := results[0].(map[string]any)
	reporter.Equal(contract.Client, "命令行解析的探测 state", "not_listed", textOf(item, "state"))
	reporter.Equal(contract.Client, "repo check 的退出码（不在名单）", contract.ExitNotFound, outcome.Exit)
}

// checkParseDownload 下载类命令：正文真的落盘，失败不落盘。
func checkParseDownload(world *harness.World, reporter *Reporter) {
	archivePath := world.Command.OutputPath("snapshot.archive")
	snapshot := world.Command.Run([]string{"pull", contract.RepoOnDisk, "-o", archivePath})
	reporter.Equal(contract.Client, "pull 的退出码", contract.ExitOK, snapshot.Exit)
	if body, err := os.ReadFile(archivePath); err != nil {
		reporter.Mismatch(contract.Client, "pull 落盘的文件", "存在且可读", err.Error())
	} else if len(body) < 2 || string(body[:2]) != "PK" {
		reporter.Mismatch(contract.Client, "pull 落盘的内容", "zip 字节流", fmt.Sprint(len(body))+" 字节")
	}

	bundlePath := world.Command.OutputPath("full.bundle")
	bundle := world.Command.Run([]string{"bundle", contract.RepoAbsent, "-o", bundlePath})
	reporter.Equal(contract.Client, "bundle 对不存在仓库的退出码", contract.ExitNotFound, bundle.Exit)
	if _, err := os.ReadFile(bundlePath); err == nil {
		reporter.Mismatch(contract.Client, "失败时 bundle 的落盘文件", "不存在", "存在")
	}
}

// flagOf 读一个布尔字段。
func flagOf(object map[string]any, name string) bool {
	value, _ := object[name].(bool)
	return value
}

// numberValue 读一个数字字段。
func numberValue(object map[string]any, name string) float64 {
	value, _ := object[name].(float64)
	return value
}
