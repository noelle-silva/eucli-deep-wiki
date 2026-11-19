package httpapi

import (
	"net/http"

	"eucli-deep-wiki-server/internal/ops"
)

// addRequest 是「加入名单」的请求体。
type addRequest struct {
	// URLs 必填，至少一项；整批原子，任一项不合法则整批不生效。
	URLs []string `json:"urls"`
	// Branch 可选：新仓库固定的分支。
	Branch string `json:"branch"`
	// History 可选：shallow 或 full。
	History string `json:"history"`
	// Reload 可选，缺省为真：让新名单立即生效。
	Reload *bool `json:"reload"`
}

// addRepositories 把一批地址加入名单。
func addRepositories(manager *ops.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request addRequest
		if err := decodeBody(r, &request); err != nil {
			writeBodyError(w, err)
			return
		}

		result, err := manager.Add(ops.AddOptions{
			URLs:    request.URLs,
			Branch:  request.Branch,
			History: request.History,
			Apply:   resolveFlag(request.Reload, true),
		})
		if err != nil {
			writeManagementError(w, err, "index_write_failed")
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
