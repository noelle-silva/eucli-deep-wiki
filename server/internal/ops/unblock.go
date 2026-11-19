package ops

import (
	"time"

	"eucli-deep-wiki-server/internal/state"
)

// UnblockOptions 是一次解除停止同步的请求。
type UnblockOptions struct {
	// ClearFailures 为真时连「连续失败次数」与「最近错误」一起清零。
	ClearFailures bool
}

// InterceptedState 是被拦截的各项事实：操作前的原样，便于调用方回显。
//
// 只列与「停不停」直接相关的几项。上次同步时间、当时体积这些同步成功的事实
// 与拦截无关，解封不动它们，也就没有必要出现在这里。
type InterceptedState struct {
	Blocked             bool   `json:"blocked"`
	BlockedSize         int64  `json:"blockedSize"`
	RemoteMissing       bool   `json:"remoteMissing"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	LastError           string `json:"lastError"`
}

// UnblockResult 是一次解除停止同步的结果。
type UnblockResult struct {
	Name string `json:"name"`
	// Cleared 表示这次确实清掉了拦截标记；本来就没被拦时为 false。
	Cleared bool `json:"cleared"`
	// Previous 是操作前的状态。
	Previous InterceptedState `json:"previous"`
}

// Unblock 解除某个仓库的停止同步状态。
//
// 清掉「体积保护」与「远端消失」两枚标记及其时间。同步成功过的那些事实
// （上次同步时间、当时体积、上次回收体积）保持原样——解封不是把历史抹掉。
// 本来就没被拦是成功（Cleared=false），不是错误。
//
// 它改的是服务端自己运行态里那份状态：状态文件与内存中的记录是同一份，
// 下一次同步的写回基于同一份数据，不会把「已解除」覆盖掉。
func (m *Manager) Unblock(name string, options UnblockOptions) (UnblockResult, error) {
	snapshot := m.source.Snapshot()
	if _, found := find(snapshot.Repositories, name); !found {
		return UnblockResult{}, ErrRepositoryNotFound
	}

	before := m.states.Get(name)
	result := UnblockResult{Name: name, Previous: interceptOf(before)}
	if !before.Blocked && !before.RemoteMissing {
		return result, nil
	}

	err := m.states.Update(name, func(current state.RepoState) state.RepoState {
		current.Blocked = false
		current.BlockedAt = time.Time{}
		current.BlockedSize = 0
		current.RemoteMissing = false
		current.RemoteMissingAt = time.Time{}
		if options.ClearFailures {
			current.ConsecutiveFailures = 0
			current.LastError = ""
		}
		return current
	})
	if err != nil {
		return UnblockResult{}, err
	}

	result.Cleared = true
	return result, nil
}

// interceptOf 取出「停不停」相关的事实。
func interceptOf(recorded state.RepoState) InterceptedState {
	return InterceptedState{
		Blocked:             recorded.Blocked,
		BlockedSize:         recorded.BlockedSize,
		RemoteMissing:       recorded.RemoteMissing,
		ConsecutiveFailures: recorded.ConsecutiveFailures,
		LastError:           recorded.LastError,
	}
}
