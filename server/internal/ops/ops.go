// Package ops 提供服务端自己的运维能力：看名单、加仓库、摘仓库、
// 解除停止同步、就地瘦身、远端探测。
//
// 归属判定只有一条：谁持有数据，谁负责操作与校验。这几件事读写的是服务端
// 机器上的名单文件、同步状态与仓库目录，因此它们在这里，而不在命令行里。
// 对外以什么形态暴露（接口、参数、错误标识）由接口层决定，本包只回答
// 「做了什么、结果是什么」。
//
// 另外两件同样只属于服务端的事在它们各自的包里：产物缓存的清单与清理见
// cache 包；单仓按需同步与内容读取见 runner 与 vault 包。
package ops

import (
	"context"
	"errors"

	"eucli-deep-wiki-server/internal/live"
	"eucli-deep-wiki-server/internal/repoindex"
	"eucli-deep-wiki-server/internal/state"
)

var (
	// ErrRepositoryNotFound 表示仓库不在名单里。
	ErrRepositoryNotFound = errors.New("repository is not in the index")
	// ErrRepositoryNotOnDisk 表示仓库在名单里，但磁盘上还没有它的副本。
	ErrRepositoryNotOnDisk = errors.New("repository is not on disk")
	// ErrBusy 表示同步正占着仓库，此刻不能做破坏性操作。
	ErrBusy = errors.New("repository is busy")
	// ErrNoURLs 表示加入名单的请求里一个地址都没有。
	ErrNoURLs = errors.New("at least one repository URL is required")
	// ErrDataRemoval 表示连数据一起删时，删磁盘副本这一步失败。
	ErrDataRemoval = errors.New("removing repository data failed")
)

// Source 提供当前生效的运行期视图，并提供重载入口。
type Source interface {
	// Snapshot 返回当前生效的配置、名单与只读视图。
	Snapshot() live.Snapshot
	// Reload 立即按磁盘内容重载配置与名单，返回本次结果。
	Reload() live.Result
}

// Git 是本包需要的版本管理能力。
type Git interface {
	// ProbeRemote 只做一次远端探测，既不读也不改本地仓库。
	ProbeRemote(ctx context.Context, url string) error
	// IsShallow 判断目录里是不是浅克隆。
	IsShallow(ctx context.Context, path string) (bool, error)
	// Compact 把完整仓库就地转为浅克隆。
	Compact(ctx context.Context, path string) error
	// GitDirectorySize 统计 .git 目录占用的字节数。
	GitDirectorySize(path string) (int64, error)
}

// Options 是构建运维能力所需的依赖。
type Options struct {
	Source Source
	States *state.Store
	Git    Git
	// Exclusive 在同步闸门内执行破坏性操作：占用期间不会开始新的同步。
	// 闸门已经被同步占用时不执行 action，直接返回 false。
	Exclusive func(action func()) bool
}

// Manager 是运维能力的入口。
type Manager struct {
	source    Source
	states    *state.Store
	git       Git
	exclusive func(action func()) bool
}

// New 用一组依赖构建运维能力。
func New(options Options) *Manager {
	return &Manager{
		source:    options.Source,
		states:    options.States,
		git:       options.Git,
		exclusive: options.Exclusive,
	}
}

// find 按名字在名单里查仓库。
func find(repositories []repoindex.Repository, name string) (repoindex.Repository, bool) {
	for _, repository := range repositories {
		if repository.Name == name {
			return repository, true
		}
	}
	return repoindex.Repository{}, false
}
