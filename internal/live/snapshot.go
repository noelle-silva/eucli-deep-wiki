package live

import (
	"time"

	"eucli-deep-wiki/internal/config"
	"eucli-deep-wiki/internal/repoindex"
	"eucli-deep-wiki/internal/vault"
)

// Snapshot 是某一时刻生效的完整视图。
//
// 它在构建完成后不再修改：配置、仓库清单与只读视图永远互相对齐，
// 因此不会出现「同步清单已经换了、查询仍然 404」这种错位。
type Snapshot struct {
	// Config 是当前生效的配置。
	Config *config.Config
	// Repositories 是当前生效的仓库清单。
	Repositories []repoindex.Repository
	// Vault 是按当前清单建立的只读视图。
	Vault *vault.Vault
	// RepositoryDir 是实际读写的仓库根目录，启动时确定，重载不改变。
	RepositoryDir string

	// ConfigLoadedAt 与 IndexLoadedAt 是最近一次成功加载的时间。
	ConfigLoadedAt time.Time
	IndexLoadedAt  time.Time

	// LastReload 是最近一次重载尝试的结果。
	LastReload Result
	// NeedsRestart 列出改了但必须重启才能生效的字段。
	NeedsRestart []string

	// 最近一次加载尝试时看到的文件指纹：文件没变就不重复解析。
	configStamp fileStamp
	indexStamp  fileStamp
	// 生效中的配置与索引是否就是磁盘上的那份。
	configInSync bool
	indexInSync  bool
}

// Status 是随重载刷新的运行期状态，由 /status 直接透出。
type Status struct {
	ConfigLoadedAt  time.Time `json:"configLoadedAt"`
	IndexLoadedAt   time.Time `json:"indexLoadedAt"`
	RepositoryCount int       `json:"repositoryCount"`
	SyncInterval    string    `json:"syncInterval"`
	GCThresholdMB   int       `json:"gcThresholdMB"`
	GCGrowthMB      int       `json:"gcGrowthMB"`
	SizeLimitMB     int       `json:"sizeLimitMB"`
	DefaultHistory  string    `json:"defaultHistory"`
	LastReload      Result    `json:"lastReload"`
	NeedsRestart    []string  `json:"needsRestart"`
}

// Result 是一次重载尝试的结果。
//
// /v1/reload 直接返回它，/status 则始终带着最近一次的结果；
// Error 非空时旧配置与旧视图都保持不变。
type Result struct {
	At time.Time `json:"at"`
	// ConfigReloaded 表示生效中的配置就是磁盘上的这份：
	// 要么本次重新加载成功，要么文件没变而上次加载成功。为 false 时 Error 非空。
	ConfigReloaded bool `json:"configReloaded"`
	// IndexReloaded 表示生效中的仓库清单与只读视图就是磁盘上的这份，含义同上。
	IndexReloaded bool `json:"indexReloaded"`
	// Repositories 是重载后生效的仓库数。
	Repositories int `json:"repositories"`
	// NeedsRestart 列出改了但必须重启才能生效的字段。
	NeedsRestart []string `json:"needsRestart"`
	// Error 说明失败原因；失败的那份文件不会被采用。
	Error string `json:"error,omitempty"`
}

// Status 汇总当前生效状态。
func (s Snapshot) Status() Status {
	needsRestart := make([]string, len(s.NeedsRestart))
	copy(needsRestart, s.NeedsRestart)

	return Status{
		ConfigLoadedAt:  s.ConfigLoadedAt,
		IndexLoadedAt:   s.IndexLoadedAt,
		RepositoryCount: len(s.Repositories),
		SyncInterval:    s.Config.SyncInterval,
		GCThresholdMB:   s.Config.GCThresholdMB,
		GCGrowthMB:      s.Config.GCGrowthMB,
		SizeLimitMB:     s.Config.SizeLimitMB,
		DefaultHistory:  s.Config.DefaultHistory,
		LastReload:      s.LastReload,
		NeedsRestart:    needsRestart,
	}
}
