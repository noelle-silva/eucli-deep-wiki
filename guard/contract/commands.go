package contract

// 世界名：一条用例需要哪一种现场。
const (
	// WorldStandard 是标准现场：三种仓库角色 + 缓存产物 + 同步状态。
	WorldStandard = "standard"
	// WorldEmpty 是空现场：名单为空、缓存为空，用于不碰版本管理工具的全量同步。
	WorldEmpty = "empty"
	// WorldBulk 是批量现场：几百个文件与足够多的命中，用于缺省条数与真收敛。
	WorldBulk = "bulk"
	// WorldGit 是真实版本管理现场：完整克隆、浅克隆、游离头指针、坏远端、锁住的配置。
	WorldGit = "git"
	// WorldGitBusy 是「同步真的卡在网上」的版本管理现场：一个黑障 + 空名单。
	WorldGitBusy = "git-busy"
	// WorldBrokenIndex 是名单文件被占住的现场：写名单必然失败。
	WorldBrokenIndex = "broken-index"
	// WorldBrokenState 是状态文件被占住的现场：写状态必然失败。
	WorldBrokenState = "broken-state"
)

// Worlds 是全部世界名，用于核对映射表没有漏项。
var Worlds = []string{
	WorldStandard, WorldEmpty, WorldBulk, WorldGit, WorldGitBusy, WorldBrokenIndex, WorldBrokenState,
}

// CommandRoute 是契约第三节的一行：命令行走哪个接口、带哪个参数。
type CommandRoute struct {
	// ID 是契约里的命令形态，给人看。
	ID string
	// Key 是这条命令在验证里的稳定标识：只用 ASCII，且必须唯一。
	// 显示名可以随文案变，标识不能——它是报告与定向运行的锚点。
	Key string
	// Endpoint 是契约端点标识。
	Endpoint string
	// Args 是命令与它自己的参数；{repo} 换成 Repo，{out} 换成一次性的输出文件路径。
	Args []string
	// Repo 是这条命令操作的仓库角色；空串表示不涉及仓库。
	Repo string
	// Query 是这次调用在查询串里必须出现的参数名。
	Query []string
	// QueryAbsent 是这次调用在查询串里必须不出现的参数名。
	QueryAbsent []string
	// QueryValues 是查询参数必须取的值。
	QueryValues map[string]string
	// Body 是这次调用在请求体里必须出现的字段名。
	Body []string
	// BodyValues 是请求体字段必须取的值；比较按文本进行。
	BodyValues map[string]string
	// Code 是这条命令必然失败时要落到的错误标识。
	Code string
	// World 是这条命令需要的现场。
	World string
	// Exit 是这条命令应有的退出码。
	Exit int
	// Note 说明这条命令验证的重点。
	Note string
}

// CommandRoutes 是契约第三节的完整对应关系，外加密钥与耗时的行为表。
var CommandRoutes = []CommandRoute{
	{
		ID: "repo add <地址>...", Key: "repo-add", Endpoint: "POST /v1/repos",
		Args: []string{"repo", "add", "https://example.com/" + RepoToAdd + ".git"},
		Body: []string{"urls", "reload"}, BodyValues: map[string]string{"reload": "true"},
		World: WorldStandard, Exit: ExitOK,
		Note: "reload 缺省为真，映射到 reload:true",
	},
	{
		ID: "repo add --no-reload", Key: "repo-add-no-reload", Endpoint: "POST /v1/repos",
		Args: []string{"repo", "add", "--no-reload", "https://example.com/epsilon.git"},
		Body: []string{"urls", "reload"}, BodyValues: map[string]string{"reload": "false"},
		World: WorldStandard, Exit: ExitOK,
		Note: "--no-reload 映射为 reload:false",
	},
	{
		ID: "repo list", Key: "repo-list", Endpoint: "GET /v1/repos",
		Args: []string{"repo", "list"}, World: WorldStandard, Exit: ExitOK,
		Note: "不带 --sizes 时不请求现测",
	},
	{
		ID: "repo list --sizes", Key: "repo-list-sizes", Endpoint: "GET /v1/repos",
		Args: []string{"repo", "list", "--sizes"}, Query: []string{"measure"},
		QueryValues: map[string]string{"measure": "true"},
		World:       WorldStandard, Exit: ExitOK,
		Note: "--sizes 映射为 measure=true",
	},
	{
		ID: "repo rm <仓库>", Key: "repo-rm", Endpoint: "DELETE /v1/repos/{name}",
		Args: []string{"repo", "rm", "{repo}"}, Repo: RepoDeclaredOnly,
		Query: []string{"keepData"}, QueryValues: map[string]string{"keepData": "true"},
		World: WorldStandard, Exit: ExitOK,
		Note: "缺省保留数据：keepData 必须出现且为真",
	},
	{
		ID: "repo rm --drop-data", Key: "repo-rm-drop-data", Endpoint: "DELETE /v1/repos/{name}",
		Args: []string{"repo", "rm", "--drop-data", "{repo}"}, Repo: RepoDeclaredOnly,
		Query: []string{"keepData"}, QueryValues: map[string]string{"keepData": "false"},
		World: WorldStandard, Exit: ExitOK,
		Note: "--drop-data 映射为 keepData=false",
	},
	{
		ID: "repo unblock <仓库>", Key: "repo-unblock", Endpoint: "POST /v1/repos/{name}/unblock",
		Args: []string{"repo", "unblock", "{repo}"}, Repo: RepoOnDisk,
		Body: []string{"clearFailures"}, World: WorldStandard, Exit: ExitOK,
	},
	{
		ID: "repo compact --dry-run [仓库]...", Key: "repo-compact-dry-run", Endpoint: "POST /v1/repos/{name}/compact",
		Args: []string{"repo", "compact", "--dry-run", "{repo}"}, Repo: RepoGitFullHistory,
		Query: []string{"dryRun"}, QueryValues: map[string]string{"dryRun": "true"},
		World: WorldGit, Exit: ExitOK,
		Note: "名单要求完整历史时动作是 skipped_full_history，仍是成功",
	},
	{
		ID: "repo check [仓库]...", Key: "repo-check", Endpoint: "POST /v1/repos/{name}/check",
		Args: []string{"repo", "check", "{repo}"}, Repo: RepoAbsent,
		World: WorldStandard, Exit: ExitNotFound, Code: "repo_not_found",
		Note: "先取名单再并发探测：不存在的仓库要能与「不可达」分开",
	},
	{
		ID: "cache list [--json]", Key: "cache-list", Endpoint: "GET /v1/cache",
		Args: []string{"cache", "list"}, World: WorldStandard, Exit: ExitOK,
	},
	{
		ID: "cache clean [--yes] [仓库]", Key: "cache-clean-unconfirmed", Endpoint: "DELETE /v1/cache",
		Args: []string{"cache", "clean"}, QueryAbsent: []string{"confirm"},
		World: WorldStandard, Exit: ExitUsage, Code: "confirm_required",
		Note: "未确认时服务端拒绝，命令行退出码是用法错误",
	},
	{
		ID: "cache clean --yes [仓库]", Key: "cache-clean", Endpoint: "DELETE /v1/cache",
		Args: []string{"cache", "clean", "--yes"}, Query: []string{"confirm"},
		QueryValues: map[string]string{"confirm": "1"},
		World:       WorldStandard, Exit: ExitOK,
	},
	{
		ID: "repos", Key: "repos", Endpoint: "GET /v1/repos",
		Args: []string{"repos"}, World: WorldStandard, Exit: ExitOK,
	},
	{
		ID: "overview <仓库>（目录）", Key: "overview-tree", Endpoint: "GET /v1/repos/{name}/tree",
		Args: []string{"overview", "{repo}"}, Repo: RepoOnDisk,
		World: WorldStandard, Exit: ExitOK,
	},
	{
		ID: "overview <仓库>（元数据）", Key: "overview-meta", Endpoint: "GET /v1/repos/{name}",
		Args: []string{"overview", "{repo}"}, Repo: RepoOnDisk,
		World: WorldStandard, Exit: ExitOK,
		Note: "概览是组合命令：元数据、目录、README 各取一次",
	},
	{
		ID: "tree <仓库> [目录]", Key: "tree", Endpoint: "GET /v1/repos/{name}/tree",
		Args: []string{"tree", "{repo}"}, Repo: RepoOnDisk,
		World: WorldStandard, Exit: ExitOK,
	},
	{
		ID: "get <仓库> <路径>...", Key: "get", Endpoint: "GET /v1/repos/{name}/file",
		Args: []string{"get", "{repo}", "README.md"}, Repo: RepoOnDisk,
		World: WorldStandard, Exit: ExitOK,
	},
	{
		ID: "search <仓库> <关键词>", Key: "search", Endpoint: "GET /v1/repos/{name}/search",
		Args: []string{"search", "{repo}", "needle"}, Repo: RepoOnDisk,
		World: WorldStandard, Exit: ExitOK,
	},
	{
		ID: "find <仓库> <文件名>", Key: "find", Endpoint: "GET /v1/repos/{name}/find",
		Args: []string{"find", "{repo}", "*.md"}, Repo: RepoOnDisk,
		World: WorldStandard, Exit: ExitOK,
	},
	{
		ID: "status [--json]", Key: "status", Endpoint: "GET /status",
		Args: []string{"status"}, World: WorldStandard, Exit: ExitOK,
	},
	{
		ID: "status（状态明细）", Key: "status-detail", Endpoint: "GET /v1/state",
		Args: []string{"status"}, World: WorldStandard, Exit: ExitOK,
		Note: "状态命令同时读快照与逐仓明细",
	},
	{
		ID: "reload", Key: "reload", Endpoint: "POST /v1/reload",
		Args: []string{"reload"}, World: WorldStandard, Exit: ExitOK,
	},
	{
		ID: "sync <仓库>", Key: "sync-one", Endpoint: "POST /v1/repos/{name}/sync",
		Args: []string{"sync", "{repo}"}, Repo: RepoAbsent,
		World: WorldStandard, Exit: ExitNotFound, Code: "repo_not_found",
	},
	{
		ID: "sync --all", Key: "sync-all", Endpoint: "POST /v1/sync",
		Args: []string{"sync", "--all"}, World: WorldEmpty, Exit: ExitOK,
	},
	{
		ID: "pull <仓库>[:路径] ...", Key: "pull", Endpoint: "GET /v1/archive",
		Args: []string{"pull", "{repo}", "-o", "{out}"}, Repo: RepoOnDisk,
		World: WorldStandard, Exit: ExitOK,
	},
	{
		ID: "bundle <仓库>", Key: "bundle", Endpoint: "GET /v1/repos/{name}/bundle",
		Args: []string{"bundle", "{repo}", "-o", "{out}"}, Repo: RepoAbsent,
		World: WorldStandard, Exit: ExitNotFound, Code: "repo_not_found",
	},
}

// CommandRouteByID 按标识取一行；不存在时直接失败。
func CommandRouteByID(id string) CommandRoute {
	for _, route := range CommandRoutes {
		if route.ID == id {
			return route
		}
	}
	panic("contract: unknown command route " + id)
}
