// Package contract 是两侧对外约定的唯一出处。
//
// 它不含任何实现，只描述「契约说什么是真的」：接口、参数、字段、错误标识、
// 状态码、退出码、上限数字、行区间写法、命令行与接口的对应关系。用例只读这
// 一张表，因此这些事实在整套守护里只有一份出处——改这里等于改契约，两侧的实现
// 必须同时跟上，否则守护立刻变红并指出是哪一侧的哪一条。
package contract

import (
	"net/http"
)

// Side 标明一条事实由哪一侧承担。
type Side string

const (
	// Server 是服务端 eucli-deep-wiki-server。
	Server Side = "服务端"
	// Client 是命令行 eucli-dw。
	Client Side = "命令行"
	// Both 表示两侧各持一份、必须逐一相等。
	Both Side = "两侧"
)

// Family 是用例的组织单位：同一族守同一类事实。
type Family string

const (
	// FamilyAPI 守接口地址、方法、参数名、响应字段名与响应形态。
	FamilyAPI Family = "接口一族"
	// FamilyNumbers 守各参数的上限、缺省值与「真收敛」行为。
	FamilyNumbers Family = "数字一族"
	// FamilyBodies 守「正文是什么、响应头是什么」，以及纯文本接口的表达。
	FamilyBodies Family = "正文与响应头一族"
	// FamilyErrors 守错误标识、状态码与退出码的映射。
	FamilyErrors Family = "错误一族"
	// FamilyCommands 守命令行与接口的对应、命令行对字段的解析与退出码落地。
	FamilyCommands Family = "命令行一族"
	// FamilyOwnership 守「守护者自己谁也不被谁依赖」：归属独立，两侧构建不需要它。
	FamilyOwnership Family = "归属一族"
)

// Families 是按展示顺序排列的全部类别。
var Families = []Family{FamilyOwnership, FamilyAPI, FamilyNumbers, FamilyBodies, FamilyErrors, FamilyCommands}

// In 标明请求参数出现的位置。
type In string

const (
	InQuery In = "query"
	InBody  In = "body"
	InPath  In = "path"
)

// Param 是契约里点名的一个请求参数。
type Param struct {
	Name string
	In   In
	// Default 是契约规定的缺省值；空串表示没有缺省值（必填，或按参数语义取值）。
	Default string
	// Required 表示这个参数必须出现。
	Required bool
	// Note 说明参数语义。
	Note string
}

// BodyKind 说明成功响应的正文形态。
type BodyKind string

const (
	BodyJSON   BodyKind = "json"
	BodyText   BodyKind = "text"
	BodyBinary BodyKind = "binary"
)

// Endpoint 是契约里的一个接口。
type Endpoint struct {
	// ID 是契约里的接口标识：方法 + 路由模板。
	ID string
	// Title 说明这个接口回答什么问题。
	Title  string
	Method string
	// Path 是路由模板，{name} 表示仓库名占位。
	Path string
	// Auth 为真时接口要求 Authorization: Bearer <key>。
	Auth bool
	// Status 是成功状态码。
	Status int
	// Params 是契约点名的请求参数。
	Params []Param
	// Fields 是成功响应里必须出现的顶层字段。
	Fields []string
	// Body 是成功响应的正文形态。
	Body BodyKind
	// Note 记录契约里对该接口的额外要求。
	Note string
}

var (
	// query helpers：让参数表读起来像契约原文。
	queryFlag = func(name string, fallback string, note string) Param {
		return Param{Name: name, In: InQuery, Default: fallback, Note: note}
	}
	queryValue = func(name string, required bool, note string) Param {
		return Param{Name: name, In: InQuery, Required: required, Note: note}
	}
	bodyField = func(name string, fallback string, note string) Param {
		return Param{Name: name, In: InBody, Default: fallback, Note: note}
	}
	pathValue = func(name string) Param {
		return Param{Name: name, In: InPath, Required: true}
	}
)

// Endpoints 是契约里的全部接口，顺序与契约文件一致：16 个既有 + 6 个新增。
var Endpoints = []Endpoint{
	{
		ID: "GET /healthz", Title: "健康检查（匿名）",
		Method: http.MethodGet, Path: "/healthz",
		Status: http.StatusOK, Fields: []string{"status"}, Body: BodyJSON,
	},
	{
		ID: "POST /v1/reload", Title: "立即重载配置与名单",
		Method: http.MethodPost, Path: "/v1/reload", Auth: true,
		Status: http.StatusOK,
		Fields: []string{"at", "configReloaded", "indexReloaded", "repositories", "needsRestart"},
		Body:   BodyJSON,
		Note:   "重载失败仍是 200，失败原因在 error 里。",
	},
	{
		ID: "GET /status", Title: "同步状态快照",
		Method: http.MethodGet, Path: "/status", Auth: true,
		Status: http.StatusOK,
		Fields: []string{"running", "total", "summary", "lastReload", "repositoryCount", "syncInterval", "defaultHistory"},
		Body:   BodyJSON,
	},
	{
		ID: "GET /v1/repos", Title: "名单总览（可现测体积）",
		Method: http.MethodGet, Path: "/v1/repos", Auth: true,
		Status: http.StatusOK,
		Params: []Param{queryFlag("measure", "false", "为真时对每个已落盘仓库现测元数据体积")},
		Fields: []string{"repositories"}, Body: BodyJSON,
		Note: "每条仓库对象带 url / history / effectiveHistory / onDisk；measure 为真时再补 measuredBytes。",
	},
	{
		ID: "GET /v1/repos/{name}", Title: "单个仓库的元数据",
		Method: http.MethodGet, Path: "/v1/repos/{name}", Auth: true,
		Status: http.StatusOK, Fields: []string{"name"}, Body: BodyJSON,
	},
	{
		ID: "GET /v1/repos/{name}/tree", Title: "展开目录",
		Method: http.MethodGet, Path: "/v1/repos/{name}/tree", Auth: true,
		Status: http.StatusOK,
		Params: []Param{
			pathValue("name"),
			queryValue("path", false, "展开起点，缺省为仓库根"),
			queryFlag("depth", "1", "展开层数，硬上限 8"),
			queryFlag("dirsOnly", "false", "只看目录"),
			queryValue("exclude", false, "逗号分隔的目录名；命中的目录不返回、也不进入其内部遍历（只作用于目录，缺省不排除）"),
		},
		Fields: []string{"repo", "revision", "path", "depth", "entries", "truncated"}, Body: BodyJSON,
		Note: "exclude 只按名字排除目录：同名普通文件照常返回；排除发生在遍历之前，不占条目配额。",
	},
	{
		ID: "GET /v1/repos/{name}/file", Title: "读取文件（正文纯文本）",
		Method: http.MethodGet, Path: "/v1/repos/{name}/file", Auth: true,
		Status: http.StatusOK,
		Params: []Param{
			pathValue("name"),
			queryValue("path", true, "仓库内路径"),
			queryValue("lines", false, "行区间写法：362-520、362-、-520、362"),
			queryValue("maxBytes", false, "正文字节上限"),
		},
		Body: BodyText,
		Note: "正文是纯文本，切段与截断的事实只走响应头。",
	},
	{
		ID: "GET /v1/repos/{name}/search", Title: "按内容检索",
		Method: http.MethodGet, Path: "/v1/repos/{name}/search", Auth: true,
		Status: http.StatusOK,
		Params: []Param{
			pathValue("name"),
			queryValue("q", true, "检索词"),
			queryValue("path", false, "限定目录"),
			queryValue("limit", false, "命中条数上限 100"),
			queryValue("offset", false, "翻页偏移"),
			queryValue("context", false, "命中前后各带多少行，上限 10"),
		},
		Fields: []string{"repo", "revision", "query", "offset", "matches", "scannedFiles", "scannedBytes", "truncated"},
		Body:   BodyJSON,
	},
	{
		ID: "GET /v1/repos/{name}/find", Title: "按文件名查找",
		Method: http.MethodGet, Path: "/v1/repos/{name}/find", Auth: true,
		Status: http.StatusOK,
		Params: []Param{
			pathValue("name"),
			queryValue("name", true, "文件名模式，支持 * 通配"),
			queryValue("path", false, "检索范围"),
			queryValue("match", false, "exact 或 substring，缺省精确"),
			queryValue("limit", false, "条数上限，缺省 200、硬上限 10000"),
			queryValue("offset", false, "翻页偏移"),
		},
		Fields: []string{"repo", "revision", "pattern", "offset", "entries", "scannedFiles", "truncated"},
		Body:   BodyJSON,
	},
	{
		ID: "GET /v1/repos/{name}/archive", Title: "下载快照包",
		Method: http.MethodGet, Path: "/v1/repos/{name}/archive", Auth: true,
		Status: http.StatusOK,
		Params: []Param{
			pathValue("name"),
			queryValue("path", false, "可重复，限定打包路径"),
			queryValue("format", false, "zip 或 tar.gz，缺省 zip"),
		},
		Body: BodyBinary,
	},
	{
		ID: "GET /v1/repos/{name}/bundle", Title: "下载带完整历史的 bundle",
		Method: http.MethodGet, Path: "/v1/repos/{name}/bundle", Auth: true,
		Status: http.StatusOK, Body: BodyBinary,
	},
	{
		ID: "GET /v1/archive", Title: "多仓库合并打包",
		Method: http.MethodGet, Path: "/v1/archive", Auth: true,
		Status: http.StatusOK,
		Params: []Param{
			queryValue("include", true, "可重复，形如 仓库 或 仓库:路径"),
			queryValue("format", false, "zip 或 tar.gz，缺省 zip"),
		},
		Body: BodyBinary,
	},
	{
		ID: "POST /v1/repos", Title: "加入名单（整批原子）",
		Method: http.MethodPost, Path: "/v1/repos", Auth: true,
		Status: http.StatusOK,
		Params: []Param{
			bodyField("urls", "", "必填，至少一项；任一项不合法则整批不生效"),
			bodyField("branch", "", "可选分支名"),
			bodyField("history", "", "可选，shallow 或 full"),
			bodyField("reload", "true", "为真时让新名单立即生效"),
		},
		Fields: []string{"added", "repositories"}, Body: BodyJSON,
		Note: "reload 为假时 reloaded 不出现。",
	},
	{
		ID: "DELETE /v1/repos/{name}", Title: "移出名单",
		Method: http.MethodDelete, Path: "/v1/repos/{name}", Auth: true,
		Status: http.StatusOK,
		Params: []Param{
			pathValue("name"),
			queryFlag("keepData", "true", "只摘名单、保留磁盘数据"),
		},
		Fields: []string{"name", "removed", "dataExisted", "dataKept"}, Body: BodyJSON,
	},
	{
		ID: "POST /v1/repos/{name}/unblock", Title: "解除停止同步",
		Method: http.MethodPost, Path: "/v1/repos/{name}/unblock", Auth: true,
		Status: http.StatusOK,
		Params: []Param{
			pathValue("name"),
			bodyField("clearFailures", "true", "连带清零失败计数与最近错误"),
		},
		Fields: []string{"name", "cleared", "previous"}, Body: BodyJSON,
		Note: "本来没有被拦是成功：cleared 为假、状态码仍是 200。",
	},
	{
		ID: "POST /v1/repos/{name}/compact", Title: "就地瘦身（可预演）",
		Method: http.MethodPost, Path: "/v1/repos/{name}/compact", Auth: true,
		Status: http.StatusOK,
		Params: []Param{
			pathValue("name"),
			queryFlag("dryRun", "false", "只预演，不动仓库"),
		},
		Fields: []string{"name", "action", "branch", "before", "after", "durationMs"}, Body: BodyJSON,
		Note: "compacted / would_compact / already_shallow / skipped_full_history 四种动作全部是成功。",
	},
	{
		ID: "GET /v1/cache", Title: "缓存清单",
		Method: http.MethodGet, Path: "/v1/cache", Auth: true,
		Status: http.StatusOK,
		Params: []Param{queryValue("repo", false, "按仓库过滤")},
		Fields: []string{"files", "totalBytes"}, Body: BodyJSON,
		Note: "目录不存在等于空清单；不返回服务端绝对路径。",
	},
	{
		ID: "DELETE /v1/cache", Title: "缓存清理",
		Method: http.MethodDelete, Path: "/v1/cache", Auth: true,
		Status: http.StatusOK,
		Params: []Param{
			queryValue("repo", false, "按仓库过滤"),
			Param{Name: "confirm", In: InQuery, Required: true, Note: "必须为真，否则拒绝"},
		},
		Fields: []string{"removed", "bytes", "skipped"}, Body: BodyJSON,
	},
	{
		ID: "POST /v1/repos/{name}/sync", Title: "按需同步单仓",
		Method: http.MethodPost, Path: "/v1/repos/{name}/sync", Auth: true,
		Status: http.StatusOK,
		Params: []Param{pathValue("name")},
		Fields: []string{"name", "action", "duration"}, Body: BodyJSON,
	},
	{
		ID: "POST /v1/sync", Title: "触发一轮全量同步",
		Method: http.MethodPost, Path: "/v1/sync", Auth: true,
		Status: http.StatusAccepted, Fields: []string{"status"}, Body: BodyJSON,
	},
	{
		ID: "POST /v1/repos/{name}/check", Title: "单仓远端探测",
		Method: http.MethodPost, Path: "/v1/repos/{name}/check", Auth: true,
		Status: http.StatusOK,
		Params: []Param{pathValue("name")},
		Fields: []string{"repo", "reachable"}, Body: BodyJSON,
	},
	{
		ID: "GET /v1/state", Title: "同步状态明细",
		Method: http.MethodGet, Path: "/v1/state", Auth: true,
		Status: http.StatusOK, Fields: []string{"repositories"}, Body: BodyJSON,
	},
}

// EndpointByID 按标识取接口。
func EndpointByID(id string) (Endpoint, bool) {
	for _, endpoint := range Endpoints {
		if endpoint.ID == id {
			return endpoint, true
		}
	}
	return Endpoint{}, false
}

// MustEndpoint 在接口标识不存在时直接失败：契约表被改坏不该静默通过。
func MustEndpoint(id string) Endpoint {
	endpoint, ok := EndpointByID(id)
	if !ok {
		panic("contract: unknown endpoint " + id)
	}
	return endpoint
}
