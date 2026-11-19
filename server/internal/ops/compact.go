package ops

import (
	"context"
	"path/filepath"
	"time"

	"eucli-deep-wiki-server/internal/repoindex"
	"eucli-deep-wiki-server/internal/vault"
)

// CompactAction 是一次就地瘦身的结论。
//
// 四种取值全部是成功：两种是「做了什么」，两种是「什么都不用做，为什么」。
// 调用方据此决定怎么向人解释，而不是靠错误文本来猜。
type CompactAction string

const (
	// Compacted 表示已经就地转成浅克隆。
	Compacted CompactAction = "compacted"
	// WouldCompact 表示预演结论是「会转换」，本次没有动仓库。
	WouldCompact CompactAction = "would_compact"
	// AlreadyShallow 表示本来就是浅克隆，无需转换。
	AlreadyShallow CompactAction = "already_shallow"
	// SkippedFullHistory 表示名单要求完整历史，按约定跳过。
	SkippedFullHistory CompactAction = "skipped_full_history"
)

// CompactOptions 是一次就地瘦身的请求。
type CompactOptions struct {
	// DryRun 为真时只判断会不会转换，不真的动仓库。
	DryRun bool
}

// RepositorySize 是仓库元数据的一份快照：是不是浅克隆、元数据占多大。
type RepositorySize struct {
	Shallow  bool  `json:"shallow"`
	GitBytes int64 `json:"gitBytes"`
}

// CompactResult 是一次就地瘦身的结果。
//
// 前后两份「是否浅克隆 + 元数据体积」都带上：调用方不需要再去问一次
// 才能确认这次到底改变了什么。
type CompactResult struct {
	Name   string         `json:"name"`
	Action CompactAction  `json:"action"`
	Branch string         `json:"branch"`
	Before RepositorySize `json:"before"`
	After  RepositorySize `json:"after"`
	// DurationMs 是这次动作的耗时，单位毫秒。
	DurationMs int64 `json:"durationMs"`
}

// Compact 把一个完整仓库就地转为浅克隆，支持预演。
//
// 它是唯一的破坏性运维动作：改写分支引用、删标签、回收对象，因此整段动作
// 在同步闸门内执行——同步既不会在半路上插进来，也不会被它打断；闸门已被
// 同步占用时直接报告 ErrBusy。
//
// 「本来就是浅克隆」与「名单要求完整历史」都是成功结论（Action 里如实表达），
// 不是错误。真正会失败的是：不在名单、磁盘上没有副本、同步正忙、仓库状态
// 不干净（游离头指针）、远端不可达、版本管理命令失败——这些都由 error 返回，
// 各有自己的标识，调用方按标识区分。
func (m *Manager) Compact(ctx context.Context, name string, options CompactOptions) (CompactResult, error) {
	var (
		result CompactResult
		err    error
	)
	if !m.exclusive(func() {
		result, err = m.compact(ctx, name, options)
	}) {
		return CompactResult{}, ErrBusy
	}
	return result, err
}

// compact 是 Compact 的实际动作，调用时已经站在同步闸门内。
func (m *Manager) compact(ctx context.Context, name string, options CompactOptions) (CompactResult, error) {
	snapshot := m.source.Snapshot()
	repository, found := find(snapshot.Repositories, name)
	if !found {
		return CompactResult{}, ErrRepositoryNotFound
	}

	path := filepath.Join(snapshot.RepositoryDir, repository.Name)
	if !isDirectory(path) {
		return CompactResult{}, ErrRepositoryNotOnDisk
	}

	startedAt := time.Now()
	result := CompactResult{
		Name:   repository.Name,
		Branch: branchOf(repository, snapshot.Vault),
	}

	shallow, err := m.git.IsShallow(ctx, path)
	if err != nil {
		return CompactResult{}, err
	}
	size, err := m.git.GitDirectorySize(path)
	if err != nil {
		return CompactResult{}, err
	}
	result.Before = RepositorySize{Shallow: shallow, GitBytes: size}
	result.After = result.Before

	if repository.EffectiveHistory(snapshot.Config.DefaultHistory) != repoindex.HistoryShallow {
		result.Action = SkippedFullHistory
		result.DurationMs = time.Since(startedAt).Milliseconds()
		return result, nil
	}
	if shallow {
		result.Action = AlreadyShallow
		result.DurationMs = time.Since(startedAt).Milliseconds()
		return result, nil
	}
	if options.DryRun {
		result.Action = WouldCompact
		result.DurationMs = time.Since(startedAt).Milliseconds()
		return result, nil
	}

	if err := m.git.Compact(ctx, path); err != nil {
		return CompactResult{}, err
	}
	// 转换之后再测一次：不去猜「瘦了多少」，而是问仓库现在的样子。
	// 这一步失败就如实报告失败——此时仓库已经转换完成，再跑一次会得到
	// 「本来就是浅克隆」，结论会自己收敛。
	converted, err := m.git.IsShallow(ctx, path)
	if err != nil {
		return CompactResult{}, err
	}
	afterSize, err := m.git.GitDirectorySize(path)
	if err != nil {
		return CompactResult{}, err
	}
	result.Action = Compacted
	result.After = RepositorySize{Shallow: converted, GitBytes: afterSize}
	result.DurationMs = time.Since(startedAt).Milliseconds()
	return result, nil
}

// branchOf 取仓库分支：磁盘上的副本说了算，读不到时退回名单里的声明。
//
// 读 HEAD 不启动 git 进程。还没有副本、或者游离头指针的仓库都读不出分支名，
// 这时报告名单声明的那个——它是这份名单对这个仓库的意图，不是猜出来的。
func branchOf(repository repoindex.Repository, view *vault.Vault) string {
	meta, err := view.Meta(repository.Name)
	if err == nil && meta.Branch != "" {
		return meta.Branch
	}
	return repository.Branch
}
