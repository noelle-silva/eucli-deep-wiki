package contract

// 退出码：沿用现有口径，命令行对外只用这几个数。
const (
	ExitOK       = 0
	ExitFailure  = 1 // 其它失败
	ExitUsage    = 2 // 参数或用法错误
	ExitNotFound = 3 // 仓库或路径不存在
	ExitAuth     = 4 // 鉴权失败
	ExitNetwork  = 5 // 网络或超时
	ExitConflict = 6 // 已有任务在跑
	ExitRejected = 7 // 被拒绝：过大、非文本等
)

// ErrorSpec 是一条失败在两侧的落点：服务端产出什么状态码与标识，命令行映射成什么退出码。
type ErrorSpec struct {
	// Code 是对外的错误标识（响应体里的 code）。
	Code string
	// Status 是服务端必须给出的状态码。
	Status int
	// Exit 是命令行必须给出的退出码。
	Exit int
	// Meaning 说明这条失败说的是什么。
	Meaning string
	// Emitted 说明这一条怎么被真实诱发。
	Emitted string
}

// Errors 是契约第四节定义的全部失败，外加既有的鉴权失败。
//
// 一条失败只有一份出处：状态码与退出码都写在这里。两侧各自实现，用例分别
// 检查「服务端真的这样答」与「命令行真的这样退」。
var Errors = []ErrorSpec{
	{
		Code: "invalid_url", Status: 400, Exit: ExitUsage,
		Meaning: "地址不合法：不是加密传输地址、带查询串或锚点、取不出名字",
		Emitted: "POST /v1/repos 送 http 地址、带 ?ref=1 的地址、空路径、无地址",
	},
	{
		Code: "invalid_name", Status: 400, Exit: ExitUsage,
		Meaning: "从地址推导出的名字不是合法的单层目录名",
		Emitted: "POST /v1/repos 送一个末段为 .. 的地址",
	},
	{
		Code: "invalid_history", Status: 400, Exit: ExitUsage,
		Meaning: "历史档位不是 shallow / full",
		Emitted: "POST /v1/repos 送 history=deep；命令行 --history deep 就地拒绝",
	},
	{
		Code: "invalid_branch", Status: 400, Exit: ExitUsage,
		Meaning: "分支名不合法",
		Emitted: "POST /v1/repos 送带空白的分支名",
	},
	{
		Code: "invalid_body", Status: 400, Exit: ExitUsage,
		Meaning: "请求体不是合法结构化数据，或字段类型不对",
		Emitted: "POST /v1/repos 送一段不是 JSON 的正文，以及把 urls 写成字符串",
	},
	{
		Code: "duplicate_name", Status: 409, Exit: ExitConflict,
		Meaning: "与名单已有项重名",
		Emitted: "POST /v1/repos 送一个已经在名单里的地址，以及同一批里重名",
	},
	{
		Code: "index_write_failed", Status: 500, Exit: ExitFailure,
		Meaning: "名单写盘失败",
		Emitted: "把名单文件换成同名目录后 POST /v1/repos",
	},
	{
		Code: "repo_not_found", Status: 404, Exit: ExitNotFound,
		Meaning: "仓库不在名单里",
		Emitted: "对不在名单的仓库名调用 DELETE /v1/repos/{name}、sync、check、bundle",
	},
	{
		Code: "state_write_failed", Status: 500, Exit: ExitFailure,
		Meaning: "同步状态写盘失败",
		Emitted: "把状态文件换成同名目录后 POST /v1/repos/{name}/unblock",
	},
	{
		Code: "sync_busy", Status: 409, Exit: ExitConflict,
		Meaning: "该仓库正忙，此刻不能做破坏性操作",
		Emitted: "同步闸门被占住时 POST /v1/repos/{name}/compact",
	},
	{
		Code: "detached_head", Status: 400, Exit: ExitUsage,
		Meaning: "仓库停在游离头指针上",
		Emitted: "对游离头指针的真实仓库 POST /v1/repos/{name}/compact",
	},
	{
		Code: "remote_unreachable", Status: 424, Exit: ExitNetwork,
		Meaning: "远端不可达",
		Emitted: "把 origin 指向不存在的仓库后 POST /v1/repos/{name}/compact",
	},
	{
		Code: "git_failed", Status: 500, Exit: ExitFailure,
		Meaning: "服务端版本管理工具执行失败",
		Emitted: "让 .git/config 被锁住后 POST /v1/repos/{name}/compact",
	},
	{
		Code: "confirm_required", Status: 400, Exit: ExitUsage,
		Meaning: "这次操作需要显式确认",
		Emitted: "不带 confirm 调用 DELETE /v1/cache",
	},
	{
		Code: "cache_busy", Status: 409, Exit: ExitConflict,
		Meaning: "缓存正忙",
		Emitted: "两次清理真的并发时出现",
	},
	{
		Code: "remove_failed", Status: 500, Exit: ExitFailure,
		Meaning: "缓存文件或仓库数据删除失败",
		Emitted: "仓库目录里有一个删不掉的文件时 DELETE /v1/repos/{name}?keepData=false",
	},
	{
		Code: "unauthorized", Status: 401, Exit: ExitAuth,
		Meaning: "缺少或无效的 Authorization: Bearer <key>",
		Emitted: "不带钥匙或带错钥匙请求任意受保护接口",
	},
}

// 动作结论与格式取值也算契约事实，但它们的「错误」形态是成功：
// 下面两个表把「取值集合」与「哪一个由哪条用例守」写在同一处，
// 避免有人新增一个取值却没人为它写用例。
var (
	// RequiredCompactActions 是必须被真实诱发过的四个瘦身结论。
	RequiredCompactActions = []string{
		ActionCompacted, ActionWouldCompact, ActionAlreadyShallow, ActionSkippedFullHistory,
	}
	// RequiredArchiveFormats 是必须被真实下发过的两种打包格式。
	RequiredArchiveFormats = []string{FormatZip, FormatTarGz}
)
