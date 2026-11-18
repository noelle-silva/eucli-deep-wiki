package httpapi

import (
	"encoding/json"
	"net/http"

	"eucli-deep-wiki/internal/live"
	"eucli-deep-wiki/internal/status"
)

// StatusStore 提供同步状态快照。
type StatusStore interface {
	Snapshot() status.Snapshot
}

// KeySource 提供当前生效的 key 集合。
type KeySource interface {
	APIKeys() []string
}

// Live 提供运行期可替换的生效视图与重载入口。
type Live interface {
	KeySource
	// EnsureFresh 在配置文件或索引变化时重新加载；没变化时只做两次 stat。
	EnsureFresh()
	// Snapshot 返回当前生效的完整视图。
	Snapshot() live.Snapshot
	// Reload 立即按磁盘内容重载配置与索引。
	Reload() live.Result
}

// Options 是路由所需的全部依赖。
type Options struct {
	Store    StatusStore
	Live     Live
	Sync     SyncTrigger
	Remote   RemoteChecker
	States   StateSource
	CacheDir string
}

// NewRouter 组装全部 HTTP 接口。
// 健康检查保持匿名，其余接口都需要 Authorization: Bearer <key>。
func NewRouter(options Options) http.Handler {
	mux := http.NewServeMux()
	shared := &exportState{}
	auth := NewAuthenticator(options.Live)

	// 受保护接口先让运行期状态跟上磁盘，再鉴权：
	// 新加入的 key 在下一个请求就能用，仓库清单同理。
	protect := func(handler http.HandlerFunc) http.Handler {
		return refresh(options.Live, auth.Wrap(handler))
	}

	mux.HandleFunc("GET /healthz", health)

	// 重载接口自己就是重载动作，不需要再提前刷新一次。
	mux.Handle("POST /v1/reload", auth.Wrap(reloadHandler(options.Live)))

	mux.Handle("GET /status", protect(statusHandler(options.Store, options.Live)))

	mux.Handle("GET /v1/repos", protect(listRepositories(options.Live)))
	mux.Handle("GET /v1/repos/{name}", protect(showRepository(options.Live)))
	mux.Handle("GET /v1/repos/{name}/tree", protect(showTree(options.Live)))
	mux.Handle("GET /v1/repos/{name}/file", protect(showFile(options.Live)))
	mux.Handle("GET /v1/repos/{name}/search", protect(searchRepository(options.Live)))
	mux.Handle("GET /v1/repos/{name}/find", protect(findFiles(options.Live)))
	mux.Handle("GET /v1/repos/{name}/archive", protect(showArchive(options.Live)))
	mux.Handle("GET /v1/repos/{name}/bundle", protect(showBundle(options.Live, options.CacheDir, shared)))
	mux.Handle("GET /v1/archive", protect(showMultiArchive(options.Live)))

	if options.Sync != nil {
		mux.Handle("POST /v1/repos/{name}/sync", protect(syncRepository(options.Sync)))
		mux.Handle("POST /v1/sync", protect(syncAllRepositories(options.Sync)))
	}

	if options.Remote != nil {
		mux.Handle("POST /v1/repos/{name}/check", protect(checkRepository(options.Remote)))
	}

	if options.States != nil {
		mux.Handle("GET /v1/state", protect(showState(options.States)))
	}

	return mux
}

// refresh 在进入受保护接口前先让运行期状态跟上磁盘。
func refresh(source Live, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		source.EnsureFresh()
		next.ServeHTTP(w, r)
	})
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// statusPayload 把同步快照与运行期状态平铺在同一个对象里：
// 原有字段语义不变，运行期字段只加不改。
type statusPayload struct {
	status.Snapshot
	live.Status
}

func statusHandler(store StatusStore, source Live) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, statusPayload{
			Snapshot: store.Snapshot(),
			Status:   source.Snapshot().Status(),
		})
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
