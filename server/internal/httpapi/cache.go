package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"eucli-deep-wiki-server/internal/cache"
)

// ArtifactStore 是产物缓存能力：保证产物存在、看清单、做清理。
//
// 这三件事必须来自同一个实现：缓存目录布局与生成互斥只在那里定义一次，
// 接口层不重算文件名，也不自己判断「谁在生成」。
type ArtifactStore interface {
	Ensure(ctx context.Context, repository string, revision string, repositoryDir string, timeout time.Duration) (string, error)
	List(options cache.ListOptions) (cache.ListResult, error)
	Clean(options cache.CleanOptions) (cache.CleanResult, error)
}

// listCache 列出缓存产物：文件名、归属仓库、对应提交、字节数、修改时间与合计。
//
// 不返回服务端绝对路径：调用方要的是「缓存里有什么、多大」，不是部署目录长什么样。
// 目录不存在等于空清单，不是错误。
func listCache(store ArtifactStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := store.List(cache.ListOptions{
			Repository: strings.TrimSpace(r.URL.Query().Get("repo")),
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiError{Code: "internal_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// cleanCache 清理缓存产物。
//
// 必须显式确认；只删产物文件，正在生成的产物与其他内容都留在原地并逐条说明原因。
func cleanCache(store ArtifactStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		confirm, err := parseFlag("confirm", values.Get("confirm"), false)
		if err != nil {
			writeParamError(w, "invalid_flag", err)
			return
		}

		result, err := store.Clean(cache.CleanOptions{
			Repository: strings.TrimSpace(values.Get("repo")),
			Confirm:    confirm,
		})
		if err != nil {
			writeManagementError(w, err, "remove_failed")
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
