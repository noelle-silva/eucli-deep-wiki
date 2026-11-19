package guard

import (
	"fmt"
	"strings"

	"eucli-deep-wiki-contract-guard/contract"
	"eucli-deep-wiki-contract-guard/harness"
)

// APIChecks 守「接口一族」：地址、方法、参数名、响应字段名与响应形态。
func APIChecks() []Check {
	checks := []Check{
		{
			ID: "api.health.public", Family: contract.FamilyAPI,
			Title: "健康检查匿名可用且只回答状态",
			World: harness.KindEmpty,
			Run:   checkHealth,
			Guards: []contract.Guardian{
				{Endpoint: "GET /healthz", Side: contract.Server},
			},
		},
		{
			ID: "api.auth.required", Family: contract.FamilyAPI,
			Title: "除健康检查外每个接口都要求钥匙",
			World: harness.KindStandard,
			Run:   checkAuthRequired,
		},
		{
			ID: "api.repos.fields", Family: contract.FamilyAPI,
			Title: "名单总览带齐名单声明、生效档位与落盘事实",
			World: harness.KindStandard,
			Run:   checkRepositoryList,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos", Side: contract.Server},
			},
		},
		{
			ID: "api.repos.measure", Family: contract.FamilyAPI,
			Title: "measure 为真时才现测体积，且不顶替 gitBytes",
			World: harness.KindStandard,
			Run:   checkRepositoryMeasure,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos", Side: contract.Server},
			},
		},
		{
			ID: "api.repos.show", Family: contract.FamilyAPI,
			Title: "单仓元数据给出分支、提交与历史深度",
			World: harness.KindStandard,
			Run:   checkRepositoryShow,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}", Side: contract.Server},
			},
		},
		{
			ID: "api.tree.shape", Family: contract.FamilyAPI,
			Title: "目录接口的字段、条目字段与「清洗后路径」回显",
			World: harness.KindStandard,
			Run:   checkTreeShape,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}/tree", Side: contract.Server},
			},
		},
		{
			ID: "api.search.shape", Family: contract.FamilyAPI,
			Title: "内容检索的字段与命中位置",
			World: harness.KindStandard,
			Run:   checkSearchShape,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}/search", Side: contract.Server},
			},
		},
		{
			ID: "api.find.shape", Family: contract.FamilyAPI,
			Title: "文件名查找的字段与命中条目",
			World: harness.KindStandard,
			Run:   checkFindShape,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}/find", Side: contract.Server},
			},
		},
		{
			ID: "api.status.shape", Family: contract.FamilyAPI,
			Title: "状态快照带齐运行期字段",
			World: harness.KindStandard,
			Run:   checkStatusShape,
			Guards: []contract.Guardian{
				{Endpoint: "GET /status", Side: contract.Server},
			},
		},
		{
			ID: "api.state.shape", Family: contract.FamilyAPI,
			Title: "状态明细逐仓给出拦截与失败事实",
			World: harness.KindStandard,
			Run:   checkStateShape,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/state", Side: contract.Server},
			},
		},
		{
			ID: "api.reload.shape", Family: contract.FamilyAPI,
			Title: "重载接口返回完整结果",
			World: harness.KindStandard,
			Run:   checkReloadShape,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/reload", Side: contract.Server},
			},
		},
		{
			ID: "api.sync.single", Family: contract.FamilyAPI,
			Title: "按需同步：不在名单的仓库与「不可达」分开表达",
			World: harness.KindStandard,
			Run:   checkSyncSingle,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/repos/{name}/sync", Side: contract.Server},
				{Code: "repo_not_found", Side: contract.Server},
			},
		},
		{
			ID: "api.sync.all", Family: contract.FamilyAPI,
			Title: "全量同步立即受理并回答已开始",
			World: harness.KindEmpty,
			Run:   checkSyncAll,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/sync", Side: contract.Server},
			},
		},
		{
			ID: "api.check.single", Family: contract.FamilyAPI,
			Title: "单仓远端探测：不在名单与不可达是两件事",
			World: harness.KindStandard,
			Run:   checkCheckEndpoint,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/repos/{name}/check", Side: contract.Server},
				{Code: "repo_not_found", Side: contract.Server},
			},
		},
		{
			ID: "api.bundle.error", Family: contract.FamilyAPI,
			Title: "bundle 下载在仓库不存在时给出同一条失败",
			World: harness.KindStandard,
			Run:   checkBundleError,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/repos/{name}/bundle", Side: contract.Server},
				{Code: "repo_not_found", Side: contract.Server},
			},
		},
	}
	return append(checks, manageChecks()...)
}

// checkHealth 健康检查是唯一匿名接口。
func checkHealth(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /healthz")
	message := world.GetAnonymous(endpoint.Path, nil)
	object, ok := requireJSON(reporter, endpoint.ID, message, endpoint.Status)
	if !ok {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)
	reporter.Equal(contract.Server, endpoint.ID+" 的状态值", "ok", textOf(object, "status"))
}

// checkAuthRequired 逐个接口确认：不带钥匙一律拒绝。
func checkAuthRequired(world *harness.World, reporter *Reporter) {
	for _, endpoint := range contract.Endpoints {
		if !endpoint.Auth {
			continue
		}
		path := endpoint.Route(contract.RepoOnDisk)
		message := world.Do(harness.Call{Method: endpoint.Method, Path: path, Anonymous: true})
		if message.Status != 401 {
			reporter.Mismatch(contract.Server, endpoint.ID+" 的鉴权", "HTTP 401", message.Describe())
			continue
		}
		if message.Code() != "unauthorized" {
			reporter.Mismatch(contract.Server, endpoint.ID+" 鉴权失败的错误标识", "unauthorized", message.Code())
		}
	}
}

// checkRepositoryList 名单总览：契约点名的四个事实必须逐条给出。
func checkRepositoryList(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos")
	object, ok := requireJSON(reporter, endpoint.ID, world.Get(endpoint.Path, nil), endpoint.Status)
	if !ok {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)
	items := arrayField(reporter, endpoint.ID+" 响应字段 repositories", object, "repositories")
	reporter.Equal(contract.Server, endpoint.ID+" 的仓库条数", 3, len(items))

	cases := []struct {
		name             string
		url              string
		history          string
		effectiveHistory string
		onDisk           bool
		branch           string
		shallow          bool
	}{
		{name: contract.RepoOnDisk, url: "https://example.com/alpha.git", history: contract.HistoryShallow, effectiveHistory: contract.HistoryShallow, onDisk: true, branch: "main", shallow: true},
		{name: contract.RepoDeclaredOnly, url: "https://example.com/beta.git", history: "", effectiveHistory: contract.HistoryShallow, onDisk: false},
		{name: contract.RepoFullHistory, url: "https://example.com/gamma.git", history: contract.HistoryFull, effectiveHistory: contract.HistoryFull, onDisk: true, branch: "dev", shallow: false},
	}
	for _, expected := range cases {
		item := requireRepo(reporter, items, expected.name)
		if item == nil {
			continue
		}
		reporter.Fields(contract.Server, "GET /v1/repos 里 "+expected.name+" 的名单事实", item, contract.RepoListFields)
		fact := "GET /v1/repos 里 " + expected.name
		reporter.Equal(contract.Server, fact+" 的 url", expected.url, textOf(item, "url"))
		reporter.Equal(contract.Server, fact+" 的 history", expected.history, textOf(item, "history"))
		reporter.Equal(contract.Server, fact+" 的 effectiveHistory", expected.effectiveHistory, textOf(item, "effectiveHistory"))
		onDisk, _ := boolOf(item, "onDisk")
		reporter.Equal(contract.Server, fact+" 的 onDisk", expected.onDisk, onDisk)
		if expected.onDisk {
			reporter.Equal(contract.Server, fact+" 的 branch", expected.branch, textOf(item, "branch"))
			shallow, _ := boolOf(item, "shallow")
			reporter.Equal(contract.Server, fact+" 的 shallow", expected.shallow, shallow)
		}
		reporter.FieldAbsent(contract.Server, "GET /v1/repos 未测量时的 "+expected.name, item, "measuredBytes")
	}
}

// checkRepositoryMeasure measure 只加 measuredBytes，不动 gitBytes。
func checkRepositoryMeasure(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos")
	object, ok := requireJSON(reporter, endpoint.ID+"?measure=true", world.Get(endpoint.Path, harness.Query("measure", "true")), endpoint.Status)
	if !ok {
		return
	}
	items := arrayField(reporter, endpoint.ID+" 响应字段 repositories", object, "repositories")

	for _, name := range []string{contract.RepoOnDisk, contract.RepoFullHistory} {
		item := requireRepo(reporter, items, name)
		if item == nil {
			continue
		}
		measured, ok := numberOf(item, "measuredBytes")
		if !ok {
			reporter.Mismatch(contract.Server, "GET /v1/repos 现测时 "+name+" 的 measuredBytes", "存在", "缺失")
			continue
		}
		actual, err := harness.GitDirectorySize(world.RepoPath(name))
		if err != nil {
			reporter.Mismatch(contract.Both, "现场 "+name+" 的 .git 体积", "能数出来", err.Error())
			continue
		}
		reporter.Equal(contract.Server, "GET /v1/repos 现测时 "+name+" 的 measuredBytes", float64(actual), measured)
	}

	alpha := requireRepo(reporter, items, contract.RepoOnDisk)
	if alpha != nil {
		// gitBytes 必须还是「上次同步成功时记录的体积」，不能被现测值顶替。
		recorded, _ := numberOf(alpha, "gitBytes")
		reporter.Equal(contract.Server, "GET /v1/repos 现测时 alpha 的 gitBytes", float64(1234), recorded)
	}

	beta := requireRepo(reporter, items, contract.RepoDeclaredOnly)
	if beta != nil {
		reporter.FieldAbsent(contract.Server, "GET /v1/repos 现测时未落盘的 "+contract.RepoDeclaredOnly, beta, "measuredBytes")
	}
}

// checkRepositoryShow 单仓元数据。
func checkRepositoryShow(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos/{name}")
	path := endpoint.Route(contract.RepoOnDisk)
	object, ok := requireJSON(reporter, endpoint.ID, world.Get(path, nil), endpoint.Status)
	if !ok {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 响应", object, []string{"name", "branch", "revision", "shallow"})
	reporter.Equal(contract.Server, endpoint.ID+" 的 name", contract.RepoOnDisk, textOf(object, "name"))
	reporter.Equal(contract.Server, endpoint.ID+" 的 branch", "main", textOf(object, "branch"))
	reporter.Equal(contract.Server, endpoint.ID+" 的 revision", harness.AlphaMainRevision, textOf(object, "revision"))
	shallow, _ := boolOf(object, "shallow")
	reporter.Equal(contract.Server, endpoint.ID+" 的 shallow", true, shallow)
	recorded, _ := numberOf(object, "gitBytes")
	reporter.Equal(contract.Server, endpoint.ID+" 的 gitBytes（上次同步时记录的体积）", float64(1234), recorded)
}

// checkTreeShape 目录接口的字段、条目字段与路径回显口径。
func checkTreeShape(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos/{name}/tree")
	path := endpoint.Route(contract.RepoOnDisk)

	object, ok := requireJSON(reporter, endpoint.ID, world.Get(path, harness.Query("path", "/docs/./")), endpoint.Status)
	if !ok {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)
	reporter.Equal(contract.Server, endpoint.ID+" 的 repo", contract.RepoOnDisk, textOf(object, "repo"))
	reporter.Equal(contract.Server, endpoint.ID+" 的 path（清洗后回显）", "docs", textOf(object, "path"))
	reporter.Equal(contract.Server, endpoint.ID+" 的 revision", harness.AlphaMainRevision, textOf(object, "revision"))

	entries := arrayField(reporter, endpoint.ID+" 响应字段 entries", object, "entries")
	if len(entries) == 0 {
		reporter.Mismatch(contract.Server, endpoint.ID+" 的条目", "非空", "空")
		return
	}
	first, _ := entries[0].(map[string]any)
	reporter.Fields(contract.Server, endpoint.ID+" 的目录项", first, contract.TreeEntryFields)
	for _, entry := range entries {
		item, _ := entry.(map[string]any)
		if !strings.HasPrefix(textOf(item, "path"), "docs/") {
			reporter.Mismatch(contract.Server, endpoint.ID+" 条目的路径", "以 docs/ 开头", textOf(item, "path"))
			break
		}
	}
}

// checkSearchShape 内容检索的字段与命中位置。
func checkSearchShape(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos/{name}/search")
	path := endpoint.Route(contract.RepoOnDisk)
	object, ok := requireJSON(reporter, endpoint.ID, world.Get(path, harness.Query("q", "needle")), endpoint.Status)
	if !ok {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)
	reporter.Equal(contract.Server, endpoint.ID+" 的 query", "needle", textOf(object, "query"))
	matches := arrayField(reporter, endpoint.ID+" 响应字段 matches", object, "matches")
	reporter.Equal(contract.Server, endpoint.ID+" 的命中条数", 2, len(matches))
	for index, expectedLine := range []int{4, 7} {
		if index >= len(matches) {
			break
		}
		match, _ := matches[index].(map[string]any)
		reporter.Fields(contract.Server, endpoint.ID+" 的命中", match, contract.SearchMatchFields)
		reporter.Equal(contract.Server, endpoint.ID+" 命中的 path", "README.md", textOf(match, "path"))
		line, _ := numberOf(match, "line")
		reporter.Equal(contract.Server, fmt.Sprintf("%s 第 %d 处命中的行号", endpoint.ID, index+1), float64(expectedLine), line)
	}
	scanned, _ := numberOf(object, "scannedFiles")
	if scanned <= 0 {
		reporter.Mismatch(contract.Server, endpoint.ID+" 的 scannedFiles", "大于 0", fmt.Sprint(scanned))
	}
}

// checkFindShape 文件名查找的字段与命中条目。
func checkFindShape(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos/{name}/find")
	path := endpoint.Route(contract.RepoOnDisk)
	object, ok := requireJSON(reporter, endpoint.ID, world.Get(path, harness.Query("name", "*.md")), endpoint.Status)
	if !ok {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)
	reporter.Equal(contract.Server, endpoint.ID+" 的 pattern", "*.md", textOf(object, "pattern"))
	entries := arrayField(reporter, endpoint.ID+" 响应字段 entries", object, "entries")
	reporter.Equal(contract.Server, endpoint.ID+" 的命中条数", 2, len(entries))
	for _, entry := range entries {
		item, _ := entry.(map[string]any)
		reporter.Fields(contract.Server, endpoint.ID+" 的命中条目", item, contract.FindEntryFields)
	}
}

// checkStatusShape 状态快照。
func checkStatusShape(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /status")
	object, ok := requireJSON(reporter, endpoint.ID, world.Get(endpoint.Path, nil), endpoint.Status)
	if !ok {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)
	count, _ := numberOf(object, "repositoryCount")
	reporter.Equal(contract.Server, endpoint.ID+" 的 repositoryCount", float64(3), count)
	reporter.Equal(contract.Server, endpoint.ID+" 的 defaultHistory", contract.HistoryShallow, textOf(object, "defaultHistory"))
}

// checkStateShape 状态明细。
func checkStateShape(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/state")
	object, ok := requireJSON(reporter, endpoint.ID, world.Get(endpoint.Path, nil), endpoint.Status)
	if !ok {
		return
	}
	repositories := objectField(reporter, endpoint.ID+" 响应字段 repositories", object, "repositories")
	alpha := objectField(reporter, endpoint.ID+" 里 alpha 的记录", repositories, contract.RepoOnDisk)
	if alpha == nil {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 里 alpha 的记录", alpha, contract.StateFields)
	blocked, _ := boolOf(alpha, "blocked")
	reporter.Equal(contract.Server, endpoint.ID+" 里 alpha 的 blocked", true, blocked)
	size, _ := numberOf(alpha, "blockedSize")
	reporter.Equal(contract.Server, endpoint.ID+" 里 alpha 的 blockedSize", float64(4096), size)
	failures, _ := numberOf(alpha, "consecutiveFailures")
	reporter.Equal(contract.Server, endpoint.ID+" 里 alpha 的 consecutiveFailures", float64(3), failures)
	reporter.Equal(contract.Server, endpoint.ID+" 里 alpha 的 lastError", "远端超时", textOf(alpha, "lastError"))
}

// checkReloadShape 重载结果。
func checkReloadShape(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/reload")
	object, ok := requireJSON(reporter, endpoint.ID, world.Post(endpoint.Path, nil), endpoint.Status)
	if !ok {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)
	repositories, _ := numberOf(object, "repositories")
	reporter.Equal(contract.Server, endpoint.ID+" 的 repositories", float64(3), repositories)
	reloaded, _ := boolOf(object, "configReloaded")
	reporter.Equal(contract.Server, endpoint.ID+" 的 configReloaded", true, reloaded)
	indexReloaded, _ := boolOf(object, "indexReloaded")
	reporter.Equal(contract.Server, endpoint.ID+" 的 indexReloaded", true, indexReloaded)
	at, _ := object["at"].(string)
	if at == "" {
		reporter.Mismatch(contract.Server, endpoint.ID+" 的 at", "非空时间", "空")
	}
}

// checkSyncSingle 按需同步：不存在的仓库给 repo_not_found。
func checkSyncSingle(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos/{name}/sync")
	message := world.Post(endpoint.Route(contract.RepoAbsent), nil)
	reporter.Outcome("POST /v1/repos/{name}/sync 对不在名单的仓库", message, 404, "repo_not_found")
}

// checkSyncAll 全量同步：立即受理。
func checkSyncAll(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/sync")
	object, ok := requireJSON(reporter, endpoint.ID, world.Post(endpoint.Path, nil), endpoint.Status)
	if !ok {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)
	reporter.Equal(contract.Server, endpoint.ID+" 的 status", "started", textOf(object, "status"))
}

// checkCheckEndpoint 单仓探测：不在名单是 repo_not_found，与「不可达」分开。
func checkCheckEndpoint(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos/{name}/check")
	message := world.Post(endpoint.Route(contract.RepoAbsent), nil)
	reporter.Outcome("POST /v1/repos/{name}/check 对不在名单的仓库", message, 404, "repo_not_found")
}

// checkBundleError bundle 对不存在的仓库给同一条失败。
func checkBundleError(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos/{name}/bundle")
	message := world.Get(endpoint.Route(contract.RepoAbsent), nil)
	reporter.Outcome("GET /v1/repos/{name}/bundle 对不在名单的仓库", message, 404, "repo_not_found")
}
