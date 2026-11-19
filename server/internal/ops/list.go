package ops

import (
	"os"
	"path/filepath"

	"eucli-deep-wiki-server/internal/repoindex"
	"eucli-deep-wiki-server/internal/vault"
)

// ListOptions 是一次名单查询的选项。
type ListOptions struct {
	// Measure 为真时为每个已落盘仓库现测一份元数据体积。
	// 它要遍历磁盘，比只读名单慢得多，因此必须显式要求。
	Measure bool
}

// ListedRepository 是名单里的一个仓库：内容事实 + 名单声明 + 磁盘现状。
//
// 内容事实（分支、提交、是否浅克隆）来自仓库目录本身，尚未落盘的仓库只有名字；
// 名单声明（地址、档位）来自名单文件；磁盘现状说明副本在不在。
type ListedRepository struct {
	vault.Meta

	// URL 是名单里声明的地址。
	URL string `json:"url"`
	// History 是名单里声明的历史档位，未声明时为空。
	History string `json:"history"`
	// EffectiveHistory 是实际生效的档位。
	EffectiveHistory string `json:"effectiveHistory"`
	// OnDisk 表示磁盘上是否已经有这个仓库的副本。
	OnDisk bool `json:"onDisk"`
	// MeasuredBytes 是本次现测的 .git 体积；未要求测量、测量失败或尚未落盘时不存在。
	MeasuredBytes *int64 `json:"measuredBytes,omitempty"`
}

// List 返回名单视图，按仓库名字典序排列。
//
// 顺序只按名字定：同一份名单，任何时候、任何人看到的都是一个顺序。
// 尚未落盘的仓库同样是名单里的项，只是内容事实为空——它不该因为还没同步
// 就从名单里消失。
func (m *Manager) List(options ListOptions) []ListedRepository {
	snapshot := m.source.Snapshot()

	declared := make(map[string]repoindex.Repository, len(snapshot.Repositories))
	for _, repository := range snapshot.Repositories {
		declared[repository.Name] = repository
	}

	metas := snapshot.Vault.Listing()
	listed := make([]ListedRepository, 0, len(metas))
	for _, meta := range metas {
		repository := declared[meta.Name]
		path := filepath.Join(snapshot.RepositoryDir, meta.Name)
		item := ListedRepository{
			Meta:             meta,
			URL:              repository.URL,
			History:          repository.History,
			EffectiveHistory: repository.EffectiveHistory(snapshot.Config.DefaultHistory),
			OnDisk:           isDirectory(path),
		}
		// 只给已经落盘的仓库测体积：没有副本就没有体积可测，
		// 测量失败时如实不给数字，而不是给一个 0 让人误以为仓库是空的。
		if options.Measure && item.OnDisk {
			if size, err := m.git.GitDirectorySize(path); err == nil {
				item.MeasuredBytes = &size
			}
		}
		listed = append(listed, item)
	}
	return listed
}

// isDirectory 判断路径是不是一个目录。
func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
