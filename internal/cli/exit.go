package cli

import (
	"context"
	"errors"
	"net/url"

	"eucli-deep-wiki/internal/apiclient"
)

// 退出码约定：调用方可以据此决定重试、跳过还是中止。
const (
	exitOK       = 0
	exitFailure  = 1 // 其它失败
	exitUsage    = 2 // 参数或用法错误
	exitNotFound = 3 // 仓库或路径不存在
	exitAuth     = 4 // 鉴权失败
	exitNetwork  = 5 // 网络或超时
	exitConflict = 6 // 已有任务在跑
	exitRejected = 7 // 被拒绝：过大、非文本等
)

// exitCodeFor 把错误映射成退出码。
func exitCodeFor(err error) int {
	if err == nil {
		return exitOK
	}

	var apiErr *apiclient.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 404:
			return exitNotFound
		case 401, 403:
			return exitAuth
		case 409:
			return exitConflict
		case 413, 415:
			return exitRejected
		default:
			return exitFailure
		}
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return exitNetwork
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return exitNetwork
	}
	return exitFailure
}
