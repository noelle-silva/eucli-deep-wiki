package ops

import (
	"fmt"
	"os"
	"path/filepath"

	"eucli-deep-wiki-server/internal/repoindex"
)

// RemoveOptions 是一次移出名单的请求。
type RemoveOptions struct {
	// KeepData 为真时只摘名单，保留磁盘上的副本。
	KeepData bool
}

// RemoveResult 是一次移出名单的结果。
//
// 两个磁盘事实分开表达，因为它们是两件事：
// DataExisted 说的是「操作之前磁盘上有没有副本」，DataKept 说的是「操作之后还留着没有」。
// 只用一个字段推断「这次删了什么」，会在「本来就没落盘」的仓库上说错话。
type RemoveResult struct {
	Name string `json:"name"`
	// Removed 表示这次确实从名单里摘掉了它。
	Removed bool `json:"removed"`
	// DataExisted 表示操作之前磁盘上是否已有副本。
	DataExisted bool `json:"dataExisted"`
	// DataKept 表示这次操作之后磁盘上是否还留着它的副本。
	DataKept bool `json:"dataKept"`
}

// Remove 把一个仓库移出名单，可选连磁盘副本一起删。
//
// 先删数据再写名单：反过来的话，写盘一旦失败就会留下「名单里没有、磁盘上有」
// 的孤儿目录，而调用方已经被告知失败、也没有别的入口再去删它。当前顺序下
// 最坏结果是「数据删了、名单没改」，下一轮同步会把它重新拉回来——可恢复。
//
// 同步状态不属于名单，这里不动它：重新加回同名仓库后，想清掉旧的拦截标记
// 走 Unblock 这条路，语义上仍是同一件事。
func (m *Manager) Remove(name string, options RemoveOptions) (RemoveResult, error) {
	snapshot := m.source.Snapshot()

	// 名单以磁盘上的那一份为准：并发的另一次改动不该被这次静默覆盖。
	index, err := repoindex.Load(snapshot.IndexPath)
	if err != nil {
		return RemoveResult{}, err
	}

	repository, found := find(index.Repositories, name)
	if !found {
		return RemoveResult{}, ErrRepositoryNotFound
	}

	path := filepath.Join(snapshot.RepositoryDir, repository.Name)
	existed := isDirectory(path)
	if !options.KeepData && existed {
		if err := os.RemoveAll(path); err != nil {
			return RemoveResult{}, fmt.Errorf("%w: 删除 %s: %v", ErrDataRemoval, path, err)
		}
	}

	next := repoindex.Index{Repositories: without(index.Repositories, repository.Name)}
	if err := next.Save(snapshot.IndexPath); err != nil {
		return RemoveResult{}, err
	}

	return RemoveResult{
		Name:        repository.Name,
		Removed:     true,
		DataExisted: existed,
		DataKept:    isDirectory(path),
	}, nil
}

// without 返回去掉指定名字之后的名单。
func without(repositories []repoindex.Repository, name string) []repoindex.Repository {
	kept := make([]repoindex.Repository, 0, len(repositories))
	for _, repository := range repositories {
		if repository.Name == name {
			continue
		}
		kept = append(kept, repository)
	}
	return kept
}
