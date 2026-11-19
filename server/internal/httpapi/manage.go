package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"eucli-deep-wiki-server/internal/cache"
	"eucli-deep-wiki-server/internal/gitops"
	"eucli-deep-wiki-server/internal/ops"
	"eucli-deep-wiki-server/internal/repoindex"
)

// 管理类接口的失败结论只在这里定义一次。
//
// 同一类失败无论在哪个能力里发生，对外都是同一个状态码与错误标识：命令行
// 靠它映射退出码、向人解释原因，因此不能一处一个说法。识别不出来的失败由
// 调用点给出兜底标识——「写盘失败」在写名单、写状态、跑版本管理命令这三种
// 场合含义不同，只有调用点知道该说哪一个。
func classifyManagement(err error) (int, string, string, bool) {
	switch {
	case errors.Is(err, repoindex.ErrInvalidURL), errors.Is(err, ops.ErrNoURLs):
		return http.StatusBadRequest, "invalid_url", err.Error(), true
	case errors.Is(err, repoindex.ErrInvalidName):
		return http.StatusBadRequest, "invalid_name", err.Error(), true
	case errors.Is(err, repoindex.ErrInvalidBranch):
		return http.StatusBadRequest, "invalid_branch", err.Error(), true
	case errors.Is(err, repoindex.ErrInvalidHistory):
		return http.StatusBadRequest, "invalid_history", err.Error(), true
	case errors.Is(err, repoindex.ErrDuplicateName):
		return http.StatusConflict, "duplicate_name", err.Error(), true
	case errors.Is(err, ops.ErrRepositoryNotFound), errors.Is(err, ops.ErrRepositoryNotOnDisk):
		return http.StatusNotFound, "repo_not_found", "仓库不在名单里，或磁盘上还没有它的副本", true
	case errors.Is(err, ops.ErrBusy):
		return http.StatusConflict, "sync_busy", "同步正在运行，此刻不能做破坏性操作", true
	case errors.Is(err, ops.ErrDataRemoval):
		return http.StatusInternalServerError, "remove_failed", err.Error(), true
	case errors.Is(err, gitops.ErrDetachedHead):
		return http.StatusBadRequest, "detached_head", "仓库停在游离头指针上，没有可跟的分支", true
	case errors.Is(err, gitops.ErrRemoteUnavailable):
		return http.StatusFailedDependency, "remote_unreachable", err.Error(), true
	case errors.Is(err, cache.ErrNotConfirmed):
		return http.StatusBadRequest, "confirm_required", "清理缓存需要显式确认", true
	case errors.Is(err, cache.ErrBusy):
		return http.StatusConflict, "cache_busy", "缓存正在清理，请稍后再试", true
	default:
		return 0, "", "", false
	}
}

// writeManagementError 把管理类失败写成对外结论。
func writeManagementError(w http.ResponseWriter, err error, fallback string) {
	if status, code, message, ok := classifyManagement(err); ok {
		writeJSON(w, status, apiError{Code: code, Message: message})
		return
	}
	writeJSON(w, http.StatusInternalServerError, apiError{Code: fallback, Message: err.Error()})
}

// decodeBody 解析可选请求体：没有正文视作空对象，缺省字段各自取默认值。
func decodeBody(r *http.Request, target any) error {
	if r.Body == nil {
		return nil
	}
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}

// writeBodyError 报告请求体解析失败。
func writeBodyError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, apiError{Code: "invalid_body", Message: err.Error()})
}

// resolveFlag 解析缺省为真的开关：请求里没写就按 fallback 处理。
func resolveFlag(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
