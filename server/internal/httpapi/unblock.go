package httpapi

import (
	"net/http"

	"eucli-deep-wiki-server/internal/ops"
)

// unblockRequest 是「解除停止同步」的请求体，可以完全没有正文。
type unblockRequest struct {
	// ClearFailures 可选，缺省为真：连带清零连续失败次数与最近错误。
	ClearFailures *bool `json:"clearFailures"`
}

// unblockRepository 解除某个仓库的停止同步状态。
//
// 本来就没被拦是成功：状态码 200，cleared 为假。它改的是服务端自己运行态里
// 那份状态，下一轮同步的写回基于同一份数据，不会把「已解除」覆盖掉。
func unblockRepository(manager *ops.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request unblockRequest
		if err := decodeBody(r, &request); err != nil {
			writeBodyError(w, err)
			return
		}

		result, err := manager.Unblock(r.PathValue("name"), ops.UnblockOptions{
			ClearFailures: resolveFlag(request.ClearFailures, true),
		})
		if err != nil {
			writeManagementError(w, err, "state_write_failed")
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
