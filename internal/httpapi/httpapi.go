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
func NewRouter(store StatusStore, repoVault *vault.Vault) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", health)
	mux.HandleFunc("GET /status", statusHandler(store))

	mux.HandleFunc("GET /v1/repos", listRepositories(repoVault))
	mux.HandleFunc("GET /v1/repos/{name}", showRepository(repoVault))
	mux.HandleFunc("GET /v1/repos/{name}/tree", showTree(repoVault))
	mux.HandleFunc("GET /v1/repos/{name}/file", showFile(repoVault))
	mux.HandleFunc("GET /v1/repos/{name}/search", searchRepository(repoVault))

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
