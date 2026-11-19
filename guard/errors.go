package guard

import (
	"fmt"
	"strconv"

	"eucli-deep-wiki-contract-guard/contract"
	"eucli-deep-wiki-contract-guard/harness"
)

// ErrorChecks 守「错误一族」：服务端真的这样答，命令行真的这样退。
//
// 每条错误标识各有两侧的责任：服务端必须给出契约规定的状态码与标识，命令行
// 必须把它映射成契约规定的退出码。两个方向分别断言，报错时直接指明是哪一侧。
func ErrorChecks() []Check {
	return []Check{
		{
			ID: "error.invalid_url", Family: contract.FamilyErrors,
			Title: "地址不合法的每一种情形都归到 invalid_url，整批不生效",
			World: harness.KindStandard,
			Run:   checkInvalidURL,
			Guards: []contract.Guardian{
				{Code: "invalid_url", Side: contract.Server},
				{Code: "invalid_url", Side: contract.Client},
			},
		},
		{
			ID: "error.invalid_name", Family: contract.FamilyErrors,
			Title: "推不出合法单层目录名归到 invalid_name",
			World: harness.KindStandard,
			Run:   checkInvalidName,
			Guards: []contract.Guardian{
				{Code: "invalid_name", Side: contract.Server},
				{Code: "invalid_name", Side: contract.Client},
			},
		},
		{
			ID: "error.invalid_history", Family: contract.FamilyErrors,
			Title: "历史档位不合法归到 invalid_history",
			World: harness.KindStandard,
			Run:   checkInvalidHistory,
			Guards: []contract.Guardian{
				{Code: "invalid_history", Side: contract.Server},
				{Code: "invalid_history", Side: contract.Client},
			},
		},
		{
			ID: "error.invalid_branch", Family: contract.FamilyErrors,
			Title: "分支名不合法归到 invalid_branch",
			World: harness.KindStandard,
			Run:   checkInvalidBranch,
			Guards: []contract.Guardian{
				{Code: "invalid_branch", Side: contract.Server},
				{Code: "invalid_branch", Side: contract.Client},
			},
		},
		{
			ID: "error.invalid_body", Family: contract.FamilyErrors,
			Title: "请求体不合法或字段类型不对归到 invalid_body",
			World: harness.KindStandard,
			Run:   checkInvalidBody,
			Guards: []contract.Guardian{
				{Code: "invalid_body", Side: contract.Server},
			},
		},
		{
			ID: "error.duplicate_name", Family: contract.FamilyErrors,
			Title: "与名单已有项重名、同批重名都归到 duplicate_name",
			World: harness.KindStandard,
			Run:   checkDuplicateName,
			Guards: []contract.Guardian{
				{Code: "duplicate_name", Side: contract.Server},
				{Code: "duplicate_name", Side: contract.Client},
			},
		},
		{
			ID: "error.index_write_failed", Family: contract.FamilyErrors,
			Title: "名单写盘失败归到 index_write_failed",
			World: harness.KindStandard,
			Run:   checkIndexWriteFailed,
			Guards: []contract.Guardian{
				{Code: "index_write_failed", Side: contract.Server},
				{Code: "index_write_failed", Side: contract.Client},
			},
		},
		{
			ID: "error.repo_not_found", Family: contract.FamilyErrors,
			Title: "不在名单的仓库归到 repo_not_found",
			World: harness.KindStandard,
			Run:   checkRepoNotFound,
			Guards: []contract.Guardian{
				{Code: "repo_not_found", Side: contract.Server},
				{Code: "repo_not_found", Side: contract.Client},
			},
		},
		{
			ID: "error.state_write_failed.server", Family: contract.FamilyErrors,
			Title: "状态写盘失败归到 state_write_failed（服务端）",
			World: harness.KindStandard,
			Run:   checkStateWriteFailedServer,
			Guards: []contract.Guardian{
				{Code: "state_write_failed", Side: contract.Server},
			},
		},
		{
			ID: "error.state_write_failed.client", Family: contract.FamilyErrors,
			Title: "状态写盘失败被命令行映射成退出码 1",
			World: harness.KindStandard,
			Run:   checkStateWriteFailedClient,
			Guards: []contract.Guardian{
				{Code: "state_write_failed", Side: contract.Client},
			},
		},
		{
			ID: "error.confirm_required", Family: contract.FamilyErrors,
			Title: "没确认就清理归到 confirm_required",
			World: harness.KindStandard,
			Run:   checkConfirmRequired,
			Guards: []contract.Guardian{
				{Code: "confirm_required", Side: contract.Server},
				{Code: "confirm_required", Side: contract.Client},
			},
		},
		{
			ID: "error.remove_failed", Family: contract.FamilyErrors,
			Title: "删磁盘副本失败归到 remove_failed",
			World: harness.KindStandard,
			Run:   checkRemoveFailed,
			Guards: []contract.Guardian{
				{Code: "remove_failed", Side: contract.Server},
				{Code: "remove_failed", Side: contract.Client},
			},
		},
		{
			ID: "error.sync_busy", Family: contract.FamilyErrors,
			Title: "同步占着闸门时瘦身归到 sync_busy",
			World: harness.KindGitBusy,
			Run:   checkSyncBusy,
			Guards: []contract.Guardian{
				{Code: "sync_busy", Side: contract.Server},
				{Code: "sync_busy", Side: contract.Client},
			},
		},
		{
			ID: "error.detached_head", Family: contract.FamilyErrors,
			Title: "游离头指针归到 detached_head",
			World: harness.KindGit,
			Run:   checkDetachedHead,
			Guards: []contract.Guardian{
				{Code: "detached_head", Side: contract.Server},
				{Code: "detached_head", Side: contract.Client},
			},
		},
		{
			ID: "error.remote_unreachable", Family: contract.FamilyErrors,
			Title: "远端不可达归到 remote_unreachable",
			World: harness.KindGit,
			Run:   checkRemoteUnreachable,
			Guards: []contract.Guardian{
				{Code: "remote_unreachable", Side: contract.Server},
				{Code: "remote_unreachable", Side: contract.Client},
			},
		},
		{
			ID: "error.git_failed", Family: contract.FamilyErrors,
			Title: "版本管理工具执行失败归到 git_failed",
			World: harness.KindGit,
			Run:   checkGitFailed,
			Guards: []contract.Guardian{
				{Code: "git_failed", Side: contract.Server},
				{Code: "git_failed", Side: contract.Client},
			},
		},
		{
			ID: "error.unauthorized", Family: contract.FamilyErrors,
			Title: "缺少或无效钥匙归到 unauthorized",
			World: harness.KindStandard,
			Run:   checkUnauthorized,
			Guards: []contract.Guardian{
				{Code: "unauthorized", Side: contract.Server},
				{Code: "unauthorized", Side: contract.Client},
			},
		},
		{
			ID: "error.exit_table", Family: contract.FamilyErrors,
			Title: "契约里的每个错误标识都被命令行映射成契约规定的退出码",
			World: harness.KindEmpty,
			Run:   checkExitTable,
			Guards: func() []contract.Guardian {
				guards := make([]contract.Guardian, 0, len(contract.Errors))
				for _, spec := range contract.Errors {
					guards = append(guards, contract.Guardian{Code: spec.Code, Side: contract.Client})
				}
				return guards
			}(),
		},
	}
}

// checkInvalidURL 地址不合法的每一种都由同一个标识回答。
func checkInvalidURL(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos")
	cases := []struct {
		reason string
		urls   []string
	}{
		{reason: "不是加密传输地址", urls: []string{"http://example.com/plain.git"}},
		{reason: "带查询串", urls: []string{"https://example.com/query.git?ref=main"}},
		{reason: "带锚点", urls: []string{"https://example.com/anchor.git#readme"}},
		{reason: "取不出名字", urls: []string{"https://example.com/"}},
		{reason: "一个地址都没有", urls: []string{}},
	}
	for _, testCase := range cases {
		message := world.PostJSON(endpoint.Path, nil, map[string]any{"urls": testCase.urls})
		reporter.Outcome("POST /v1/repos 地址不合法（"+testCase.reason+"）", message, 400, "invalid_url")
	}

	outcome := world.Command.Run([]string{"repo", "add", "http://example.com/plain.git"})
	reporter.Equal(contract.Client, "repo add 不合法地址的退出码", contract.ExitUsage, outcome.Exit)
}

// checkInvalidName 推不出合法单层目录名。
func checkInvalidName(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos")
	message := world.PostJSON(endpoint.Path, nil, map[string]any{"urls": []string{"https://example.com/a/.."}})
	reporter.Outcome("POST /v1/repos 推不出合法名字", message, 400, "invalid_name")

	outcome := world.Command.Run([]string{"repo", "add", "https://example.com/a/.."})
	reporter.Equal(contract.Client, "repo add 推不出名字的退出码", contract.ExitUsage, outcome.Exit)
}

// checkInvalidHistory 历史档位不合法。
func checkInvalidHistory(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos")
	message := world.PostJSON(endpoint.Path, nil, map[string]any{
		"urls":    []string{"https://example.com/history.git"},
		"history": "deep",
	})
	reporter.Outcome("POST /v1/repos 历史档位不合法", message, 400, "invalid_history")

	// 命令行自己也做前置校验：写法不合法就地拒绝，退出码与契约一致。
	outcome := world.Command.Run([]string{"repo", "add", "--history", "deep", "https://example.com/history.git"})
	reporter.Equal(contract.Client, "repo add --history deep 的退出码", contract.ExitUsage, outcome.Exit)
}

// checkInvalidBranch 分支名不合法。
func checkInvalidBranch(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos")
	message := world.PostJSON(endpoint.Path, nil, map[string]any{
		"urls":   []string{"https://example.com/branch.git"},
		"branch": "bad branch",
	})
	reporter.Outcome("POST /v1/repos 分支名不合法", message, 400, "invalid_branch")

	outcome := world.Command.Run([]string{"repo", "add", "--branch", "bad branch", "https://example.com/branch.git"})
	reporter.Equal(contract.Client, "repo add 分支名不合法的退出码", contract.ExitUsage, outcome.Exit)
}

// checkInvalidBody 请求体本身不合法。
func checkInvalidBody(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos")

	notJSON := world.PostRaw(endpoint.Path, nil, "{urls: nope")
	reporter.Outcome("POST /v1/repos 请求体不是 JSON", notJSON, 400, "invalid_body")

	wrongType := world.PostJSON(endpoint.Path, nil, map[string]any{"urls": "https://example.com/type.git"})
	reporter.Outcome("POST /v1/repos 字段类型不对", wrongType, 400, "invalid_body")
}

// checkDuplicateName 重名。
func checkDuplicateName(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos")

	existing := world.PostJSON(endpoint.Path, nil, map[string]any{"urls": []string{"https://example.com/alpha.git"}})
	reporter.Outcome("POST /v1/repos 与名单已有项重名", existing, 409, "duplicate_name")

	withinBatch := world.PostJSON(endpoint.Path, nil, map[string]any{
		"urls": []string{"https://example.com/twin.git", "https://example.com/twin.git"},
	})
	reporter.Outcome("POST /v1/repos 同一批里重名", withinBatch, 409, "duplicate_name")

	outcome := world.Command.Run([]string{"repo", "add", "https://example.com/alpha.git"})
	reporter.Equal(contract.Client, "repo add 重名的退出码", contract.ExitConflict, outcome.Exit)
}

// checkIndexWriteFailed 名单写盘失败。
func checkIndexWriteFailed(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos")
	if err := world.BreakIndex(); err != nil {
		reporter.Mismatch(contract.Both, "现场名单文件", "能被换成同名目录", err.Error())
		return
	}
	message := world.PostJSON(endpoint.Path, nil, map[string]any{"urls": []string{"https://example.com/write.git"}})
	reporter.Outcome("POST /v1/repos 名单写盘失败", message, 500, "index_write_failed")

	outcome := world.Command.Run([]string{"repo", "add", "https://example.com/write.git"})
	reporter.Equal(contract.Client, "repo add 名单写盘失败的退出码", contract.ExitFailure, outcome.Exit)
}

// checkRepoNotFound 不在名单。
func checkRepoNotFound(world *harness.World, reporter *Reporter) {
	remove := contract.MustEndpoint("DELETE /v1/repos/{name}")
	reporter.Outcome("DELETE /v1/repos/{name} 不在名单",
		world.Delete(remove.Route(contract.RepoAbsent), nil), 404, "repo_not_found")

	syncOnce := contract.MustEndpoint("POST /v1/repos/{name}/sync")
	reporter.Outcome("POST /v1/repos/{name}/sync 不在名单",
		world.Post(syncOnce.Route(contract.RepoAbsent), nil), 404, "repo_not_found")

	check := contract.MustEndpoint("POST /v1/repos/{name}/check")
	reporter.Outcome("POST /v1/repos/{name}/check 不在名单",
		world.Post(check.Route(contract.RepoAbsent), nil), 404, "repo_not_found")

	outcome := world.Command.Run([]string{"repo", "rm", contract.RepoAbsent})
	reporter.Equal(contract.Client, "repo rm 不在名单的退出码", contract.ExitNotFound, outcome.Exit)
}

// checkStateWriteFailedServer 状态写盘失败：服务端这一侧。
//
// 一次性动作：解封会先改内存态再落盘，落盘失败之后「本来没被拦」就成了真的。
// 因此服务端与命令行各自用一份现场，谁也不会把对方的触发条件用掉。
func checkStateWriteFailedServer(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos/{name}/unblock")
	if err := world.BreakState(); err != nil {
		reporter.Mismatch(contract.Both, "现场状态文件", "能被换成同名目录", err.Error())
		return
	}
	message := world.PostJSON(endpoint.Route(contract.RepoOnDisk), nil, map[string]any{})
	reporter.Outcome("POST /v1/repos/{name}/unblock 状态写盘失败", message, 500, "state_write_failed")
}

// checkStateWriteFailedClient 状态写盘失败：命令行这一侧。
func checkStateWriteFailedClient(world *harness.World, reporter *Reporter) {
	if err := world.BreakState(); err != nil {
		reporter.Mismatch(contract.Both, "现场状态文件", "能被换成同名目录", err.Error())
		return
	}
	outcome := world.Command.Run([]string{"repo", "unblock", contract.RepoOnDisk})
	reporter.Equal(contract.Client, "repo unblock 状态写盘失败的退出码", contract.ExitFailure, outcome.Exit)
}

// checkConfirmRequired 没确认就清理。
func checkConfirmRequired(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("DELETE /v1/cache")
	message := world.Delete(endpoint.Path, nil)
	reporter.Outcome("DELETE /v1/cache 不带 confirm", message, 400, "confirm_required")

	outcome := world.Command.Run([]string{"cache", "clean"})
	reporter.Equal(contract.Client, "cache clean 不带 --yes 的退出码", contract.ExitUsage, outcome.Exit)
}

// checkRemoveFailed 删磁盘副本失败。
func checkRemoveFailed(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("DELETE /v1/repos/{name}")

	restore, err := world.HoldRemovalFailure(contract.RepoOnDisk, "README.md")
	if err != nil {
		reporter.Mismatch(contract.Both, "现场：占住仓库里的文件", "能占住", err.Error())
		return
	}
	message := world.Delete(endpoint.Route(contract.RepoOnDisk), harness.Query("keepData", "false"))
	restore()
	reporter.Outcome("DELETE /v1/repos/{name} 删磁盘副本失败", message, 500, "remove_failed")

	restore, err = world.HoldRemovalFailure(contract.RepoFullHistory, "src/gamma.txt")
	if err != nil {
		reporter.Mismatch(contract.Both, "现场：占住仓库里的文件", "能占住", err.Error())
		return
	}
	outcome := world.Command.Run([]string{"repo", "rm", "--drop-data", contract.RepoFullHistory})
	restore()
	reporter.Equal(contract.Client, "repo rm --drop-data 删数据失败的退出码", contract.ExitFailure, outcome.Exit)
}

// checkSyncBusy 同步闸门被真实占住时，瘦身报告「正忙」。
func checkSyncBusy(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos/{name}/compact")

	release, err := world.HoldSyncGate()
	if err != nil {
		reporter.Mismatch(contract.Both, "现场：占住同步闸门", "能占住", err.Error())
		return
	}
	defer release()

	message := world.Post(endpoint.Route(contract.RepoGitBlackHole), harness.Query("dryRun", "true"))
	reporter.Outcome("POST /v1/repos/{name}/compact 同步正忙", message, 409, "sync_busy")

	outcome := world.Command.Run([]string{"repo", "compact", "--dry-run", contract.RepoGitBlackHole})
	reporter.Equal(contract.Client, "repo compact 同步正忙的退出码", contract.ExitConflict, outcome.Exit)
}

// checkDetachedHead 游离头指针。
func checkDetachedHead(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos/{name}/compact")
	message := world.Post(endpoint.Route(contract.RepoGitDetached), nil)
	reporter.Outcome("POST /v1/repos/{name}/compact 游离头指针", message, 400, "detached_head")

	outcome := world.Command.Run([]string{"repo", "compact", contract.RepoGitDetached})
	reporter.Equal(contract.Client, "repo compact 游离头指针的退出码", contract.ExitUsage, outcome.Exit)
}

// checkRemoteUnreachable 远端不可达。
func checkRemoteUnreachable(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos/{name}/compact")
	message := world.Post(endpoint.Route(contract.RepoGitBadOrigin), nil)
	reporter.Outcome("POST /v1/repos/{name}/compact 远端不可达", message, 424, "remote_unreachable")

	outcome := world.Command.Run([]string{"repo", "compact", contract.RepoGitBadOrigin})
	reporter.Equal(contract.Client, "repo compact 远端不可达的退出码", contract.ExitNetwork, outcome.Exit)
}

// checkGitFailed 版本管理工具执行失败。
func checkGitFailed(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("POST /v1/repos/{name}/compact")
	message := world.Post(endpoint.Route(contract.RepoGitLockedConfig), nil)
	reporter.Outcome("POST /v1/repos/{name}/compact 版本管理工具失败", message, 500, "git_failed")

	outcome := world.Command.Run([]string{"repo", "compact", contract.RepoGitLockedConfig})
	reporter.Equal(contract.Client, "repo compact 版本管理工具失败的退出码", contract.ExitFailure, outcome.Exit)
}

// checkUnauthorized 缺少或无效钥匙。
func checkUnauthorized(world *harness.World, reporter *Reporter) {
	endpoint := contract.MustEndpoint("GET /v1/repos")

	anonymous := world.GetAnonymous(endpoint.Path, nil)
	if reporter.Outcome("GET /v1/repos 不带钥匙", anonymous, 401, "unauthorized") {
		if anonymous.Header.Get("WWW-Authenticate") == "" {
			reporter.Mismatch(contract.Server, "401 响应的 WWW-Authenticate", "存在", "缺失")
		}
	}

	wrong := world.Do(harness.Call{
		Method: "GET", Path: endpoint.Path,
		Headers: map[string]string{"Authorization": "Bearer wrong-key-000000"},
	})
	reporter.Outcome("GET /v1/repos 带错钥匙", wrong, 401, "unauthorized")

	outcome := world.Command.Run([]string{"repos"}, "--key", "wrong-key-000000")
	reporter.Equal(contract.Client, "命令行带错钥匙的退出码", contract.ExitAuth, outcome.Exit)
}

// checkExitTable 把契约里的每条错误标识定向喂给命令行，核对退出码。
//
// 服务端自己能不能产出这些标识由各条 error.* 用例负责；这里只负责「映射表」：
// 命令行拿到某个标识时给出的退出码必须是契约里的那个数。
func checkExitTable(world *harness.World, reporter *Reporter) {
	for _, spec := range contract.Errors {
		stub := harness.NewStub(spec.Status, map[string]string{
			"code": spec.Code, "message": "契约映射用例",
		})
		outcome := world.Command.Run([]string{"repos"}, "--server", stub.URL())
		stub.Close()

		fact := fmt.Sprintf("错误标识 %s（HTTP %d）的命令行退出码", spec.Code, spec.Status)
		reporter.Equal(contract.Client, fact, spec.Exit, outcome.Exit)
		if spec.Exit == contract.ExitOK {
			reporter.Mismatch(contract.Client, fact, "非零退出码", strconv.Itoa(spec.Exit))
		}
	}
}
