package httpapi

import (
	"time"

	"eucli-deep-wiki/internal/state"
	"eucli-deep-wiki/internal/vault"
)

// repositoryView 把「仓库内容事实」与「同步运行事实」平铺在同一份响应里：
// 客户端一次请求就能判断仓库是什么、有多新、多大、是否被保护。
//
// vault.Meta 是匿名字段，序列化后字段与它自身完全一致，新增字段只加不改。
type repositoryView struct {
	vault.Meta

	// LastSyncAt 是最近一次同步成功的时间，GitBytes 是当时测得的 .git 体积。
	LastSyncAt time.Time `json:"lastSyncAt,omitempty"`
	GitBytes   int64     `json:"gitBytes,omitempty"`

	// Blocked 表示体积超过上限已停止自动同步；RemoteMissing 表示远端已判定消失。
	Blocked       bool  `json:"blocked,omitempty"`
	BlockedSize   int64 `json:"blockedSize,omitempty"`
	RemoteMissing bool  `json:"remoteMissing,omitempty"`
}

// snapshotStates 取一份状态副本；没有状态源时返回 nil，合并时按「无记录」处理。
func snapshotStates(states StateSource) map[string]state.RepoState {
	if states == nil {
		return nil
	}
	return states.Snapshot()
}

// composeRepository 合并两份事实源；没有状态记录（例如尚未同步过）时只给内容事实。
func composeRepository(meta vault.Meta, recorded map[string]state.RepoState) repositoryView {
	composed := repositoryView{Meta: meta}

	entry, ok := recorded[meta.Name]
	if !ok {
		return composed
	}

	composed.LastSyncAt = entry.LastSyncAt
	composed.GitBytes = entry.GitBytes
	composed.Blocked = entry.Blocked
	composed.BlockedSize = entry.BlockedSize
	composed.RemoteMissing = entry.RemoteMissing
	return composed
}
