package httpapi

import (
	"context"
	"errors"
	"net/http"

	"eucli-deep-wiki/internal/gitops"
	"eucli-deep-wiki/internal/runner"
)

// SyncTrigger 是对按需同步能力的抽象。
type SyncTrigger interface {
	SyncOne(ctx context.Context, name string) (gitops.Result, error)
	StartSyncAll(ctx context.Context) error
}

func syncRepository(trigger SyncTrigger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 客户端断开不应中断已经开始的同步，否则仓库可能停在半路。
		result, err := trigger.SyncOne(context.WithoutCancel(r.Context()), r.PathValue("name"))
		if err != nil {
			writeSyncError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func syncAllRepositories(trigger SyncTrigger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := trigger.StartSyncAll(context.WithoutCancel(r.Context())); err != nil {
			writeSyncError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
	}
}

func writeSyncError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, runner.ErrRepoNotManaged):
		writeJSON(w, http.StatusNotFound, apiError{Code: "repo_not_found", Message: "仓库不在索引中"})
	case errors.Is(err, runner.ErrSyncBusy):
		writeJSON(w, http.StatusConflict, apiError{Code: "sync_busy", Message: "已有同步任务在运行，请稍后再试"})
	default:
		writeJSON(w, http.StatusInternalServerError, apiError{Code: "sync_failed", Message: err.Error()})
	}
}
