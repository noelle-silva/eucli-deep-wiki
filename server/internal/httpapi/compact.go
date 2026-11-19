package httpapi

import (
	"context"
	"net/http"

	"eucli-deep-wiki-server/internal/ops"
)

// compactRepository 把一个仓库就地瘦身，可预演。
//
// 这是分钟级动作，而且中途被中断会留下改了一半的仓库：因此它既不跟随客户端
// 断开而取消，也不受默认的请求时限约束——真正给每一步设上限的是配置里的
// 版本管理命令超时。等待多久由调用方自己决定。
func compactRepository(manager *ops.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dryRun, err := parseFlag("dryRun", r.URL.Query().Get("dryRun"), false)
		if err != nil {
			writeParamError(w, "invalid_flag", err)
			return
		}

		result, err := manager.Compact(
			context.WithoutCancel(r.Context()),
			r.PathValue("name"),
			ops.CompactOptions{DryRun: dryRun},
		)
		if err != nil {
			writeManagementError(w, err, "git_failed")
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
