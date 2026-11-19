package contract

// 嵌套对象的字段名：响应里这些对象必须带着契约点名的字段。
var (
	// TreeEntryFields 是目录项。
	TreeEntryFields = []string{"name", "path", "type", "size", "depth"}
	// SearchMatchFields 是一处检索命中。
	SearchMatchFields = []string{"path", "line", "text"}
	// SearchContextFields 是命中前后的上下文行。
	SearchContextFields = []string{"line", "text"}
	// FindEntryFields 是按文件名命中的一条。
	FindEntryFields = []string{"name", "path", "type", "size"}
	// CacheFileFields 是缓存清单里的一条产物。
	CacheFileFields = []string{"name", "repo", "revision", "bytes", "modifiedAt"}
	// CacheSkippedFields 是一条没被删掉的缓存目录项。
	CacheSkippedFields = []string{"name", "reason"}
	// AddedFields 是加入名单成功后返回的一条新增记录。
	AddedFields = []string{"name", "url", "branch", "history", "effectiveHistory"}
	// ReloadedFields 是「新名单立即生效」那一步的结果。
	ReloadedFields = []string{"configReloaded", "indexReloaded", "repositories"}
	// PreviousFields 是解除停止同步之前的状态。
	PreviousFields = []string{"blocked", "blockedSize", "remoteMissing", "consecutiveFailures", "lastError"}
	// CompactSizeFields 是瘦身前后的一份状态。
	CompactSizeFields = []string{"shallow", "gitBytes"}
	// StateFields 是单个仓库的同步状态明细里「一定会出现」的那几项。
	// 其余字段带 omitempty：零值时不出现，因此不能要求它们存在于每一条记录。
	StateFields = []string{"blocked", "blockedSize", "consecutiveFailures"}
	// RepoListFields 是名单总览里每条仓库对象独有的四个事实。
	RepoListFields = []string{"url", "history", "effectiveHistory", "onDisk"}
)

// 文件接口的响应头：正文之外的「文件多长、这次给了哪一段」只走头部。
const (
	HeaderRepoRevision = "X-Repo-Revision"
	HeaderTotalLines   = "X-File-Total-Lines"
	HeaderFirstLine    = "X-File-First-Line"
	HeaderLastLine     = "X-File-Last-Line"
	HeaderFileSize     = "X-File-Size"
	HeaderFileBytes    = "X-File-Bytes"
	HeaderTruncated    = "X-File-Truncated"
	HeaderETag         = "ETag"
	HeaderModified     = "Last-Modified"
	HeaderCacheControl = "Cache-Control"
)

// FileHeaders 是文件接口必须给出的响应头。
var FileHeaders = []string{
	HeaderRepoRevision, HeaderTotalLines, HeaderFirstLine,
	HeaderLastLine, HeaderFileSize, HeaderFileBytes,
}

// TruncatedByBytes 是「正文被字节上限截断」的标记值。
const TruncatedByBytes = "bytes"

// FileContentType 是文件接口的正文类型：纯文本，不伪装成结构化响应。
const FileContentType = "text/plain; charset=utf-8"

// 历史档位：契约里只有这两个取值。
const (
	HistoryShallow = "shallow"
	HistoryFull    = "full"
)

// HistoryModes 是档位的合法取值。
var HistoryModes = []string{HistoryShallow, HistoryFull}

// 就地瘦身的动作结论：四种全部是成功。
const (
	ActionCompacted          = "compacted"
	ActionWouldCompact       = "would_compact"
	ActionAlreadyShallow     = "already_shallow"
	ActionSkippedFullHistory = "skipped_full_history"
)

// CompactActions 是全部动作取值，两种「做了什么」加两种「为什么没做」。
var CompactActions = []string{
	ActionCompacted, ActionWouldCompact, ActionAlreadyShallow, ActionSkippedFullHistory,
}

// 打包格式与对应的正文类型。
const (
	FormatZip   = "zip"
	FormatTarGz = "tar.gz"
)

// ArchiveContentTypes 是两种格式的正文类型。
var ArchiveContentTypes = map[string]string{
	FormatZip:   "application/zip",
	FormatTarGz: "application/gzip",
}

// ContentDispositionPrefix 是下载类接口的响应头前缀：必须是附件。
const ContentDispositionPrefix = "attachment; filename="

// CacheArtifactSuffix 是缓存产物的后缀：只有带这个后缀、且最后一段像提交号的
// 文件才算产物；其余目录项清理时必须留在原地。
const CacheArtifactSuffix = ".bundle"

// CacheTemporarySuffix 是生成过程中的临时文件后缀，永远不删。
const CacheTemporarySuffix = ".tmp"
