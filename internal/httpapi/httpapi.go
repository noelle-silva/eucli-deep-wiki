package httpapi

import (
	"encoding/json"
	"net/http"

	"eucli-deep-wiki/internal/status"
	"eucli-deep-wiki/internal/vault"
)

// StatusStore 提供同步状态快照。
type StatusStore interface {
	Snapshot() status.Snapshot
}

// NewRouter 组装全部 HTTP 接口。
// 健康检查保持匿名，其余接口都需要 Authorization: Bearer <key>。
func NewRouter(store StatusStore, repoVault *vault.Vault, auth *Authenticator) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", health)

	mux.Handle("GET /status", auth.Wrap(http.HandlerFunc(statusHandler(store))))

	mux.Handle("GET /v1/repos", auth.Wrap(listRepositories(repoVault)))
	mux.Handle("GET /v1/repos/{name}", auth.Wrap(showRepository(repoVault)))
	mux.Handle("GET /v1/repos/{name}/tree", auth.Wrap(showTree(repoVault)))
	mux.Handle("GET /v1/repos/{name}/file", auth.Wrap(showFile(repoVault)))
	mux.Handle("GET /v1/repos/{name}/search", auth.Wrap(searchRepository(repoVault)))

	return mux
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func statusHandler(store StatusStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, store.Snapshot())
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
