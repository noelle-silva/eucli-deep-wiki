package httpapi

import "net/http"

// reloadHandler 立即重载配置与索引。
//
// 重载失败时仍然返回 200 与完整结果：请求本身被正确处理了，
// 失败原因在结果的 error 里，服务继续用旧配置与旧视图运行。
func reloadHandler(source Live) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, source.Reload())
	}
}
