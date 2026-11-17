package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"eucli-deep-wiki/internal/status"
	"eucli-deep-wiki/internal/vault"
)

// StatusStore 提供同步状态快照。
type StatusStore interface {
	Snapshot() status.Snapshot
}

// Options 是路由所需的全部依赖。
type Options struct {
	Store      StatusStore
	Vault      *vault.Vault
	Auth       *Authenticator
	Sync       SyncTrigger
	CacheDir   string
	GitTimeout time.Duration
}

// NewRouter 组装全部 HTTP 接口。
// 健康检查保持匿名，其余接口都需要 Authorization: Bearer <key>。
func NewRouter(options Options) http.Handler {
	mux := http.NewServeMux()
	shared := &exportState{}

	mux.HandleFunc("GET /healthz", health)

	mux.Handle("GET /status", options.Auth.Wrap(http.HandlerFunc(statusHandler(options.Store))))

	mux.Handle("GET /v1/repos", options.Auth.Wrap(listRepositories(options.Vault)))
	mux.Handle("GET /v1/repos/{name}", options.Auth.Wrap(showRepository(options.Vault)))
	mux.Handle("GET /v1/repos/{name}/tree", options.Auth.Wrap(showTree(options.Vault)))
	mux.Handle("GET /v1/repos/{name}/file", options.Auth.Wrap(showFile(options.Vault)))
	mux.Handle("GET /v1/repos/{name}/search", options.Auth.Wrap(searchRepository(options.Vault)))
	mux.Handle("GET /v1/repos/{name}/archive", options.Auth.Wrap(showArchive(options.Vault)))
	mux.Handle("GET /v1/repos/{name}/bundle", options.Auth.Wrap(
		showBundle(options.Vault, options.CacheDir, options.GitTimeout, shared),
	))
	mux.Handle("GET /v1/archive", options.Auth.Wrap(showMultiArchive(options.Vault)))

	if options.Sync != nil {
		mux.Handle("POST /v1/repos/{name}/sync", options.Auth.Wrap(syncRepository(options.Sync)))
		mux.Handle("POST /v1/sync", options.Auth.Wrap(syncAllRepositories(options.Sync)))
	}

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
