package ops

import (
	"strings"

	"eucli-deep-wiki-server/internal/live"
	"eucli-deep-wiki-server/internal/repoindex"
)

// AddOptions 是一次加入名单的请求。
type AddOptions struct {
	// URLs 是要加入的地址，至少一个。
	URLs []string
	// Branch 是新仓库固定的分支，空表示用远端默认分支。
	Branch string
	// History 是新仓库的历史档位，空表示跟随服务端默认档位。
	History string
	// Apply 为真时写完名单立即让运行期状态重载，新名单马上可用。
	Apply bool
}

// AddedRepository 是一条新增记录：名单里写了什么，生效档位是什么。
type AddedRepository struct {
	Name             string `json:"name"`
	URL              string `json:"url"`
	Branch           string `json:"branch"`
	History          string `json:"history"`
	EffectiveHistory string `json:"effectiveHistory"`
}

// AddResult 是一次加入名单的结果。
type AddResult struct {
	Added []AddedRepository `json:"added"`
	// Repositories 是本次写入后名单里的仓库总数。
	Repositories int `json:"repositories"`
	// Reloaded 是「让新名单立即生效」那一步的结果；没要求立即生效时不存在。
	Reloaded *live.Result `json:"reloaded,omitempty"`
}

// Add 把一批地址加入名单。
//
// 整批原子：任何一条地址不合法、名字推不出来、历史档位或分支不合法、与已有的
// 重名，整批都不生效，磁盘上的名单一个字都不动。
//
// 输入校验的失败由 repoindex 给出标识（地址/名字/分支/档位/重名），写盘失败
// 返回底层错误；调用方按标识区分「哪里不对」。
func (m *Manager) Add(options AddOptions) (AddResult, error) {
	snapshot := m.source.Snapshot()

	// 名单以磁盘上的那一份为准：并发的另一次改动不该被这次静默覆盖。
	index, err := repoindex.Load(snapshot.IndexPath)
	if err != nil {
		return AddResult{}, err
	}

	additions := make([]repoindex.Repository, 0, len(options.URLs))
	for _, rawURL := range options.URLs {
		cleaned := strings.TrimSpace(rawURL)
		if cleaned == "" {
			continue
		}
		// 名字从地址推导、地址本身也在这里校验；档位与分支照原样带上，
		// 由写盘前的整份校验统一判定（不 trim，写错就是写错）。
		repository, err := repoindex.FromURL(cleaned)
		if err != nil {
			return AddResult{}, err
		}
		repository.Branch = options.Branch
		repository.History = options.History
		additions = append(additions, repository)
	}
	if len(additions) == 0 {
		return AddResult{}, ErrNoURLs
	}

	// 先拼出整份新名单再落盘：Save 会校验整份内容，成功之前旧的名单还在。
	next := repoindex.Index{
		Repositories: append(append([]repoindex.Repository{}, index.Repositories...), additions...),
	}
	if err := next.Save(snapshot.IndexPath); err != nil {
		return AddResult{}, err
	}

	result := AddResult{Repositories: len(next.Repositories)}
	for _, repository := range additions {
		result.Added = append(result.Added, AddedRepository{
			Name:             repository.Name,
			URL:              repository.URL,
			Branch:           repository.Branch,
			History:          repository.History,
			EffectiveHistory: repository.EffectiveHistory(snapshot.Config.DefaultHistory),
		})
	}

	if options.Apply {
		// 名单已经写在盘上，这一刻之后它才是「生效中」的那一份；
		// 生效失败不改写已经完成的事实，只如实带回结果。
		reloaded := m.source.Reload()
		result.Reloaded = &reloaded
	}
	return result, nil
}
