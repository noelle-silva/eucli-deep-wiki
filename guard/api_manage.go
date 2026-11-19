package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"eucli-deep-wiki-contract-guard/contract"
	"eucli-deep-wiki-contract-guard/harness"
)

// manageChecks 是「接口一族」里改服务端状态的那几条，单独放一处便于阅读。
func manageChecks() []Check {
	return []Check{
		{
			ID: "api.repos.add", Family: contract.FamilyAPI,
			Title: "加入名单：整批原子、reload 缺省为真、返回新增项与总数",
			World: harness.KindStandard,
			Run:   checkRepositoryAdd,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/repos", Side: contract.Server},
			},
		},
		{
			ID: "api.repos.remove", Family: contract.FamilyAPI,
			Title: "移出名单：两个磁盘事实分开表达，缺省保留数据",
			World: harness.KindStandard,
			Run:   checkRepositoryRemove,
			Guards: []contract.Guardian{
				{Endpoint: "DELETE /v1/repos/{name}", Side: contract.Server},
			},
		},
		{
			ID: "api.repos.unblock", Family: contract.FamilyAPI,
			Title: "解除停止同步：本来没被拦也是成功，改的是运行态",
			World: harness.KindStandard,
			Run:   checkUnblock,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/repos/{name}/unblock", Side: contract.Server},
			},
		},
		{
			ID: "api.repos.unblock.keep_failures", Family: contract.FamilyAPI,
			Title: "解除停止同步：clearFailures 为假时失败计数与最近错误留原样",
			World: harness.KindStandard,
			Run:   checkUnblockKeepFailures,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/repos/{name}/unblock", Side: contract.Server},
			},
		},
		{
			ID: "api.repos.compact", Family: contract.FamilyAPI,
			Title: "就地瘦身：四种动作结论都按契约取值，且都是成功",
			World: harness.KindGit,
			Run:   checkCompactActions,
			Guards: []contract.Guardian{
				{Endpoint: "POST /v1/repos/{name}/compact", Side: contract.Server},
				{Action: contract.ActionCompacted, Side: contract.Server},
				{Action: contract.ActionWouldCompact, Side: contract.Server},
				{Action: contract.ActionAlreadyShallow, Side: contract.Server},
				{Action: contract.ActionSkippedFullHistory, Side: contract.Server},
			},
		},
		{
			ID: "api.cache.list", Family: contract.FamilyAPI,
			Title: "缓存清单：只列产物、给出合计、可按仓库过滤",
			World: harness.KindStandard,
			Run:   checkCacheList,
			Guards: []contract.Guardian{
				{Endpoint: "GET /v1/cache", Side: contract.Server},
			},
		},
		{
			ID: "api.cache.clean", Family: contract.FamilyAPI,
			Title: "缓存清理：必须显式确认，只删产物，其余进 skipped",
			World: harness.KindStandard,
			Run:   checkCacheClean,
			Guards: []contract.Guardian{
				{Endpoint: "DELETE /v1/cache", Side: contract.Server},
			},
		},
	}
}

// checkRepositoryAdd 加入名单：形状、缺省 reload、整批原子。
func checkRepositoryAdd(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos")

	object, ok := requireJSON(reporter, endpoint.ID, world.PostJSON(endpoint.Path, nil, map[string]any{
		"urls":    []string{"https://example.com/" + contract.RepoToAdd + ".git"},
		"branch":  "main",
		"history": contract.HistoryFull,
	}), endpoint.Status)
	if !ok {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)

	added := arrayField(reporter, endpoint.ID+" 响应字段 added", object, "added")
	if len(added) != 1 {
		reporter.Mismatch(contract.Server, endpoint.ID+" 的 added 条数", "1", fmt.Sprint(len(added)))
	} else {
		item, _ := added[0].(map[string]any)
		reporter.Fields(contract.Server, endpoint.ID+" 的 added 条目", item, contract.AddedFields)
		reporter.Equal(contract.Server, endpoint.ID+" 新增项的 name", contract.RepoToAdd, textOf(item, "name"))
		reporter.Equal(contract.Server, endpoint.ID+" 新增项的 url", "https://example.com/delta.git", textOf(item, "url"))
		reporter.Equal(contract.Server, endpoint.ID+" 新增项的 branch", "main", textOf(item, "branch"))
		reporter.Equal(contract.Server, endpoint.ID+" 新增项的 history", contract.HistoryFull, textOf(item, "history"))
		reporter.Equal(contract.Server, endpoint.ID+" 新增项的 effectiveHistory", contract.HistoryFull, textOf(item, "effectiveHistory"))
	}
	total, _ := numberOf(object, "repositories")
	reporter.Equal(contract.Server, endpoint.ID+" 的 repositories", float64(4), total)

	reloaded := objectField(reporter, endpoint.ID+" 响应字段 reloaded（reload 缺省为真）", object, "reloaded")
	if reloaded != nil {
		reporter.Fields(contract.Server, endpoint.ID+" 的 reloaded", reloaded, contract.ReloadedFields)
		indexReloaded, _ := boolOf(reloaded, "indexReloaded")
		reporter.Equal(contract.Server, endpoint.ID+" 的 reloaded.indexReloaded", true, indexReloaded)
	}

	// 第二次：reload 为假时 reloaded 不该出现，名单总数照样变化。
	object, ok = requireJSON(reporter, endpoint.ID+"（reload=false）", world.PostJSON(endpoint.Path, nil, map[string]any{
		"urls":   []string{"https://example.com/epsilon.git"},
		"reload": false,
	}), endpoint.Status)
	if ok {
		reporter.FieldAbsent(contract.Server, endpoint.ID+"（reload=false）", object, "reloaded")
		total, _ = numberOf(object, "repositories")
		reporter.Equal(contract.Server, endpoint.ID+"（reload=false）的 repositories", float64(5), total)
	}

	// 整批原子：一条地址不合法，整批都不生效。
	message := world.PostJSON(endpoint.Path, nil, map[string]any{
		"urls": []string{"https://example.com/zeta.git", "http://example.com/bad.git"},
	})
	if !reporter.Outcome("POST /v1/repos 整批里有一条不合法", message, 400, "invalid_url") {
		return
	}
	indexContent, err := world.ReadIndex()
	if err != nil {
		reporter.Mismatch(contract.Both, "现场名单文件", "可读", err.Error())
		return
	}
	if strings.Contains(indexContent, "zeta") {
		reporter.Mismatch(contract.Server, "整批失败的名单", "一条都不写进名单", "写进了 zeta")
	}
	listing, ok := requireJSON(reporter, "整批失败后的 GET /v1/repos", world.Get("/v1/repos", nil), 200)
	if ok {
		items := arrayField(reporter, "整批失败后的 GET /v1/repos 响应字段 repositories", listing, "repositories")
		reporter.Equal(contract.Server, "整批失败后的仓库条数", 5, len(items))
	}
}

// checkRepositoryRemove 移出名单：三种情形分别说清。
func checkRepositoryRemove(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("DELETE /v1/repos/{name}")

	// 情形一：缺省 keepData（参数都不带）→ 只摘名单、数据保留。
	object, ok := requireJSON(reporter, endpoint.ID, world.Delete(endpoint.Route(contract.RepoFullHistory), nil), endpoint.Status)
	if ok {
		reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)
		reporter.Equal(contract.Server, endpoint.ID+" 的 name", contract.RepoFullHistory, textOf(object, "name"))
		removed, _ := boolOf(object, "removed")
		existed, _ := boolOf(object, "dataExisted")
		kept, _ := boolOf(object, "dataKept")
		reporter.Equal(contract.Server, endpoint.ID+" 缺省时的 removed", true, removed)
		reporter.Equal(contract.Server, endpoint.ID+" 缺省时的 dataExisted", true, existed)
		reporter.Equal(contract.Server, endpoint.ID+" 缺省时的 dataKept", true, kept)
		if _, statErr := os.Stat(world.RepoPath(contract.RepoFullHistory)); statErr != nil {
			reporter.Mismatch(contract.Server, endpoint.ID+" 缺省时的磁盘副本", "保留", statErr.Error())
		}
	}

	// 情形二：keepData=false → 连磁盘数据一起删。
	object, ok = requireJSON(reporter, endpoint.ID+"（keepData=false）", world.Delete(endpoint.Route(contract.RepoOnDisk), harness.Query("keepData", "false")), endpoint.Status)
	if ok {
		existed, _ := boolOf(object, "dataExisted")
		kept, _ := boolOf(object, "dataKept")
		reporter.Equal(contract.Server, endpoint.ID+"（keepData=false）的 dataExisted", true, existed)
		reporter.Equal(contract.Server, endpoint.ID+"（keepData=false）的 dataKept", false, kept)
		if _, statErr := os.Stat(world.RepoPath(contract.RepoOnDisk)); statErr == nil {
			reporter.Mismatch(contract.Server, endpoint.ID+"（keepData=false）的磁盘副本", "已删除", "仍然存在")
		}
	}

	// 情形三：本来就没有副本 → dataExisted 是假（这是「操作之前」的事实），
	// 操作之后同样没有副本，因此 dataKept 也是假。整件事仍然成功。
	object, ok = requireJSON(reporter, endpoint.ID+"（本来没有副本）", world.Delete(endpoint.Route(contract.RepoDeclaredOnly), nil), endpoint.Status)
	if ok {
		removed, _ := boolOf(object, "removed")
		existed, _ := boolOf(object, "dataExisted")
		kept, _ := boolOf(object, "dataKept")
		reporter.Equal(contract.Server, endpoint.ID+"（本来没有副本）的 removed", true, removed)
		reporter.Equal(contract.Server, endpoint.ID+"（本来没有副本）的 dataExisted", false, existed)
		reporter.Equal(contract.Server, endpoint.ID+"（本来没有副本）的 dataKept", false, kept)
	}
}

// checkUnblock 解除停止同步：清掉拦截标记，并且下一次读状态看到的就是清掉之后的样子。
func checkUnblock(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos/{name}/unblock")
	path := endpoint.Route(contract.RepoOnDisk)

	object, ok := requireJSON(reporter, endpoint.ID, world.PostJSON(path, nil, map[string]any{}), endpoint.Status)
	if !ok {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)
	cleared, _ := boolOf(object, "cleared")
	reporter.Equal(contract.Server, endpoint.ID+" 的 cleared", true, cleared)

	previous := objectField(reporter, endpoint.ID+" 响应字段 previous", object, "previous")
	if previous != nil {
		reporter.Fields(contract.Server, endpoint.ID+" 的 previous", previous, contract.PreviousFields)
		blocked, _ := boolOf(previous, "blocked")
		reporter.Equal(contract.Server, endpoint.ID+" 的 previous.blocked", true, blocked)
		size, _ := numberOf(previous, "blockedSize")
		reporter.Equal(contract.Server, endpoint.ID+" 的 previous.blockedSize", float64(4096), size)
		failures, _ := numberOf(previous, "consecutiveFailures")
		reporter.Equal(contract.Server, endpoint.ID+" 的 previous.consecutiveFailures", float64(3), failures)
		reporter.Equal(contract.Server, endpoint.ID+" 的 previous.lastError", "远端超时", textOf(previous, "lastError"))
	}

	// 运行态必须真的改了：紧接着读状态明细，不应该还看到拦截标记。
	stateEndpoint := contract.MustEndpoint("GET /v1/state")
	state, stateOK := requireJSON(reporter, stateEndpoint.ID, world.Get(stateEndpoint.Path, nil), stateEndpoint.Status)
	if stateOK {
		repositories := objectField(reporter, stateEndpoint.ID+" 响应字段 repositories", state, "repositories")
		alpha := objectField(reporter, stateEndpoint.ID+" 里 alpha 的记录", repositories, contract.RepoOnDisk)
		if alpha != nil {
			if blocked, present := boolOf(alpha, "blocked"); present && blocked {
				reporter.Mismatch(contract.Server, "解封之后 "+stateEndpoint.ID+" 里 alpha 的 blocked", "false", "true")
			}
			if failures, present := numberOf(alpha, "consecutiveFailures"); present && failures != 0 {
				reporter.Mismatch(contract.Server, "解封之后 "+stateEndpoint.ID+" 里 alpha 的 consecutiveFailures", "0", fmt.Sprint(failures))
			}
			reporter.FieldAbsent(contract.Server, "解封之后 "+stateEndpoint.ID+" 里 alpha", alpha, "lastError")
		}
	}

	// 再来一次：本来没被拦是成功（200 + cleared=false）。
	object, ok = requireJSON(reporter, endpoint.ID+"（本来没被拦）", world.PostJSON(path, nil, map[string]any{}), endpoint.Status)
	if ok {
		cleared, _ := boolOf(object, "cleared")
		reporter.Equal(contract.Server, endpoint.ID+"（本来没被拦）的 cleared", false, cleared)
	}
}

// checkUnblockKeepFailures clearFailures 为假时只清拦截标记。
func checkUnblockKeepFailures(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos/{name}/unblock")
	object, ok := requireJSON(reporter, endpoint.ID, world.PostJSON(endpoint.Route(contract.RepoOnDisk), nil, map[string]any{
		"clearFailures": false,
	}), endpoint.Status)
	if !ok {
		return
	}
	cleared, _ := boolOf(object, "cleared")
	reporter.Equal(contract.Server, endpoint.ID+" 的 cleared", true, cleared)

	stateEndpoint := contract.MustEndpoint("GET /v1/state")
	state, stateOK := requireJSON(reporter, stateEndpoint.ID, world.Get(stateEndpoint.Path, nil), stateEndpoint.Status)
	if !stateOK {
		return
	}
	repositories := objectField(reporter, stateEndpoint.ID+" 响应字段 repositories", state, "repositories")
	alpha := objectField(reporter, stateEndpoint.ID+" 里 alpha 的记录", repositories, contract.RepoOnDisk)
	if alpha == nil {
		return
	}
	failures, _ := numberOf(alpha, "consecutiveFailures")
	reporter.Equal(contract.Server, "clearFailures=false 时 alpha 的 consecutiveFailures", float64(3), failures)
	reporter.Equal(contract.Server, "clearFailures=false 时 alpha 的 lastError", "远端超时", textOf(alpha, "lastError"))
}

// checkCompactActions 就地瘦身：四种动作结论都按契约取值。
//
// 四种全是成功，因此这里断言的是「动作取值」与「成功状态」，而不是错误。
// 现场里四个仓库各自对应一种结论，由真实 git 命令做出来。
func checkCompactActions(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos/{name}/compact")

	cases := []struct {
		repository string
		dryRun     bool
		action     string
	}{
		{repository: contract.RepoGitFull, dryRun: true, action: contract.ActionWouldCompact},
		{repository: contract.RepoGitShallow, dryRun: false, action: contract.ActionAlreadyShallow},
		{repository: contract.RepoGitFullHistory, dryRun: false, action: contract.ActionSkippedFullHistory},
	}
	for _, testCase := range cases {
		query := harness.Query()
		if testCase.dryRun {
			query = harness.Query("dryRun", "true")
		}
		message := world.Post(endpoint.Route(testCase.repository), query)
		fact := endpoint.ID + " 对 " + testCase.repository
		object, ok := requireJSON(reporter, fact, message, endpoint.Status)
		if !ok {
			continue
		}
		reporter.Fields(contract.Server, fact+" 响应", object, endpoint.Fields)
		reporter.Equal(contract.Server, fact+" 的 action", testCase.action, textOf(object, "action"))
		reporter.Equal(contract.Server, fact+" 的 name", testCase.repository, textOf(object, "name"))
		before := objectField(reporter, fact+" 的 before", object, "before")
		reporter.Fields(contract.Server, fact+" 的 before", before, contract.CompactSizeFields)
		after := objectField(reporter, fact+" 的 after", object, "after")
		reporter.Fields(contract.Server, fact+" 的 after", after, contract.CompactSizeFields)
		if _, present := object[contract.FieldCompactDuration]; !present {
			reporter.Mismatch(contract.Server, fact+" 的耗时字段", contract.FieldCompactDuration, "缺失")
		}
	}

	// 真正执行一次转换：它会把完整仓库就地变浅，因此放在最后。
	message := world.Post(endpoint.Route(contract.RepoGitFull), nil)
	object, ok := requireJSON(reporter, endpoint.ID+" 对 "+contract.RepoGitFull+"（真实执行）", message, endpoint.Status)
	if ok {
		reporter.Equal(contract.Server, "真实执行瘦身的 action", contract.ActionCompacted, textOf(object, "action"))
		after := objectField(reporter, "真实执行瘦身的 after", object, "after")
		if shallow, present := boolOf(after, "shallow"); !present || !shallow {
			reporter.Mismatch(contract.Server, "真实执行瘦身后的 after.shallow", "true", fmt.Sprint(shallow))
		}
	}
}

// checkCacheList 缓存清单：只列产物，合计等于逐条之和。
func checkCacheList(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/cache")
	object, ok := requireJSON(reporter, endpoint.ID, world.Get(endpoint.Path, nil), endpoint.Status)
	if !ok {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)
	files := arrayField(reporter, endpoint.ID+" 响应字段 files", object, "files")
	reporter.Equal(contract.Server, endpoint.ID+" 的产物条数", 3, len(files))

	sum := 0.0
	for _, file := range files {
		item, _ := file.(map[string]any)
		reporter.Fields(contract.Server, endpoint.ID+" 的产物条目", item, contract.CacheFileFields)
		reporter.FieldAbsent(contract.Server, endpoint.ID+" 的产物条目", item, "path")
		bytes, _ := numberOf(item, "bytes")
		sum += bytes
	}
	total, _ := numberOf(object, "totalBytes")
	reporter.Equal(contract.Server, endpoint.ID+" 的 totalBytes", sum, total)

	for _, name := range []string{contract.RepoOnDisk, contract.RepoFullHistory, contract.RepoDeclaredOnly} {
		found := false
		for _, file := range files {
			item, _ := file.(map[string]any)
			if textOf(item, "repo") == name {
				found = true
				reporter.Equal(contract.Server, endpoint.ID+" 里 "+name+" 产物的 revision", harness.AlphaMainRevision, textOf(item, "revision"))
			}
		}
		if !found {
			reporter.Mismatch(contract.Server, endpoint.ID+" 的产物归属", "包含 "+name, "缺失")
		}
	}

	filtered, ok := requireJSON(reporter, endpoint.ID+"?repo=alpha", world.Get(endpoint.Path, harness.Query("repo", contract.RepoOnDisk)), endpoint.Status)
	if ok {
		items := arrayField(reporter, endpoint.ID+"?repo=alpha 响应字段 files", filtered, "files")
		reporter.Equal(contract.Server, endpoint.ID+"?repo=alpha 的产物条数", 1, len(items))
	}
}

// checkCacheClean 缓存清理：确认是硬要求，只删产物。
func checkCacheClean(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("DELETE /v1/cache")

	// 没有确认 → 拒绝。
	message := world.Delete(endpoint.Path, nil)
	reporter.Outcome("DELETE /v1/cache 不带 confirm", message, 400, "confirm_required")

	alphaArtifact := filepath.Join(world.CacheDir, "alpha-"+harness.AlphaMainRevision+contract.CacheArtifactSuffix)
	artifactInfo, statErr := os.Stat(alphaArtifact)
	if statErr != nil {
		reporter.Mismatch(contract.Both, "现场 alpha 缓存产物", "存在", statErr.Error())
		return
	}

	// 按仓库确认清理：只删这个仓库的产物，其余目录项逐条说明原因。
	object, ok := requireJSON(reporter, endpoint.ID+"?repo=alpha&confirm=true", world.Delete(endpoint.Path, harness.Query("repo", contract.RepoOnDisk, "confirm", "true")), endpoint.Status)
	if !ok {
		return
	}
	reporter.Fields(contract.Server, endpoint.ID+" 响应", object, endpoint.Fields)
	removed, _ := numberOf(object, "removed")
	reporter.Equal(contract.Server, endpoint.ID+"?repo=alpha 的 removed", float64(1), removed)
	bytes, _ := numberOf(object, "bytes")
	reporter.Equal(contract.Server, endpoint.ID+"?repo=alpha 的 bytes", float64(artifactInfo.Size()), bytes)

	skipped := arrayField(reporter, endpoint.ID+" 响应字段 skipped", object, "skipped")
	if len(skipped) == 0 {
		reporter.Mismatch(contract.Server, endpoint.ID+" 的 skipped", "逐条说明没删的目录项", "空")
	}
	for _, entry := range skipped {
		item, _ := entry.(map[string]any)
		reporter.Fields(contract.Server, endpoint.ID+" 的 skipped 条目", item, contract.CacheSkippedFields)
		if textOf(item, "reason") == "" {
			reporter.Mismatch(contract.Server, endpoint.ID+" 的 skipped 原因", "非空", "空")
		}
	}

	// 非产物必须留在原地。
	if _, statErr := os.Stat(filepath.Join(world.CacheDir, "notes.txt")); statErr != nil {
		reporter.Mismatch(contract.Server, "缓存清理后的非产物文件", "保留", statErr.Error())
	}
	if info, statErr := os.Stat(filepath.Join(world.CacheDir, "sub")); statErr != nil || !info.IsDir() {
		reporter.Mismatch(contract.Server, "缓存清理后的子目录", "保留", "不见了")
	}

	// 再清一次全部：剩下两份真产物。
	object, ok = requireJSON(reporter, endpoint.ID+"?confirm=true", world.Delete(endpoint.Path, harness.Query("confirm", "true")), endpoint.Status)
	if ok {
		removed, _ := numberOf(object, "removed")
		reporter.Equal(contract.Server, endpoint.ID+"?confirm=true 的 removed", float64(2), removed)
	}
}
