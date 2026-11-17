package httpapi

import (
	"net/http"

	"eucli-deep-wiki/internal/state"
)

// StateSource 提供每个仓库的体积保护状态。
type StateSource interface {
	Snapshot() map[string]state.RepoState
}

func showState(source StateSource) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"repositories": source.Snapshot()})
	}
}
