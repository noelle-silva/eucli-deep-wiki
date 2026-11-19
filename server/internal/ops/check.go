package ops

import (
	"context"
	"sync"

	"eucli-deep-wiki-server/internal/repoindex"
)

// checkWorkers 是并发探测的上限，避免一次打满远端连接。
const checkWorkers = 8

// CheckOptions 是一次远端探测的请求。
type CheckOptions struct {
	// Names 为空时探测名单里的全部仓库。
	Names []string
}

// CheckedRepository 是单个仓库的探测结果。
//
// 「不在名单里」与「在名单里但远端不可达」是两件事，因此分开表达：
// 前者是一条没有探测过的记录，后者带着远端返回的原因。
type CheckedRepository struct {
	Name string `json:"name"`
	// Missing 表示仓库不在名单里，因此没有探测。
	Missing bool `json:"missing,omitempty"`
	// Reachable 只在 Missing 为假时有意义。
	Reachable bool   `json:"reachable"`
	Error     string `json:"error,omitempty"`
}

// CheckResult 是一次远端探测的结果。
type CheckResult struct {
	Repositories []CheckedRepository `json:"repositories"`
	Reachable    int                 `json:"reachable"`
	Unreachable  int                 `json:"unreachable"`
	Missing      int                 `json:"missing"`
}

// Check 探测仓库的远端是否还在。
//
// 探测只做一次远端引用查询：既不读也不改本地仓库，因此本地副本缺失或有损时
// 也能判断远端状态。结果顺序与请求顺序（或名单顺序）一致，与哪个探测先完成
// 无关——同样的请求，任何时候看到的都是同一个顺序。
func (m *Manager) Check(ctx context.Context, options CheckOptions) CheckResult {
	snapshot := m.source.Snapshot()

	targets := options.Names
	if len(targets) == 0 {
		targets = make([]string, 0, len(snapshot.Repositories))
		for _, repository := range snapshot.Repositories {
			targets = append(targets, repository.Name)
		}
	}

	checked := make([]CheckedRepository, len(targets))
	workers := checkWorkers
	if workers > len(targets) {
		workers = len(targets)
	}

	jobs := make(chan int)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			for index := range jobs {
				checked[index] = m.checkOne(ctx, snapshot.Repositories, targets[index])
			}
		}()
	}
	for index := range targets {
		jobs <- index
	}
	close(jobs)
	group.Wait()

	result := CheckResult{Repositories: checked}
	for _, item := range checked {
		switch {
		case item.Missing:
			result.Missing++
		case item.Reachable:
			result.Reachable++
		default:
			result.Unreachable++
		}
	}
	return result
}

// checkOne 探测一个仓库的远端。
func (m *Manager) checkOne(ctx context.Context, repositories []repoindex.Repository, name string) CheckedRepository {
	repository, found := find(repositories, name)
	if !found {
		return CheckedRepository{Name: name, Missing: true, Error: "仓库不在名单里"}
	}

	checked := CheckedRepository{Name: repository.Name}
	if err := m.git.ProbeRemote(ctx, repository.URL); err != nil {
		checked.Error = err.Error()
		return checked
	}
	checked.Reachable = true
	return checked
}
