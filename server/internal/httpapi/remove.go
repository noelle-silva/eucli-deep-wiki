package httpapi

import (
	"net/http"

	"eucli-deep-wiki-server/internal/ops"
)

// removeRepository 把仓库移出名单。
//
// keepData 缺省为真：默认只摘名单，磁盘副本留着——「找不到」由名单决定，
// 而数据要不要一起删是另一件事，得显式要求。
func removeRepository(manager *ops.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keepData, err := parseFlag("keepData", r.URL.Query().Get("keepData"), true)
		if err != nil {
			writeParamError(w, "invalid_flag", err)
			return
		}

		result, err := manager.Remove(r.PathValue("name"), ops.RemoveOptions{KeepData: keepData})
		if err != nil {
			writeManagementError(w, err, "index_write_failed")
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
