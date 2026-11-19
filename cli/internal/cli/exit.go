package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"

	"eucli-dw/internal/apiclient"
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

// failureSpec 是一条契约失败标识在命令行这一侧的落地：说明 + 退出码。
//
// 键取自契约第四节，两侧只靠契约对齐。说明与退出码必须成对出现，
// 任何一条命令都不许把契约里的失败私自塌成「其它失败」。
type failureSpec struct {
	message string
	exit    int
}

// failureSpecs 是契约里定义的失败标识。
var failureSpecs = map[string]failureSpec{
	"invalid_url":        {message: "地址不合法：只接受加密传输地址（服务端只认这一种），不能带查询串或锚点，且要能取出仓库名", exit: exitUsage},
	"invalid_name":       {message: "从地址推不出合法的仓库名（必须是单层目录名）", exit: exitUsage},
	"invalid_history":    {message: "历史档位不合法：只支持 shallow 或 full", exit: exitUsage},
	"invalid_branch":     {message: "分支名不合法", exit: exitUsage},
	"invalid_body":       {message: "请求体不是合法结构化数据，或字段类型不对", exit: exitUsage},
	"confirm_required":   {message: "这次操作需要显式确认，确认后加 --yes 再执行", exit: exitUsage},
	"detached_head":      {message: "仓库处于游离头指针状态，先让同步把分支切回来", exit: exitUsage},
	"duplicate_name":     {message: "名单里已有同名仓库", exit: exitConflict},
	"sync_busy":          {message: "该仓库正在同步，等这一轮结束再试", exit: exitConflict},
	"cache_busy":         {message: "缓存正在生成，等生成结束再试", exit: exitConflict},
	"repo_not_found":     {message: "仓库不在名单里", exit: exitNotFound},
	"remote_unreachable": {message: "远端不可达：检查网络，或地址是否还有效", exit: exitNetwork},
	"index_write_failed": {message: "名单写盘失败：检查服务端名单文件的权限与磁盘空间", exit: exitFailure},
	"state_write_failed": {message: "同步状态写盘失败：检查服务端状态文件的权限与磁盘空间", exit: exitFailure},
	"git_failed":         {message: "服务端版本管理工具执行失败", exit: exitFailure},
	"remove_failed":      {message: "缓存文件删除失败", exit: exitFailure},
}

// exitCodeFor 把错误映射成退出码：先认契约标识，再退回按状态码分类。
func exitCodeFor(err error) int {
	if err == nil {
		return exitOK
	}

	var apiErr *apiclient.APIError
	if errors.As(err, &apiErr) {
		if spec, ok := failureSpecs[apiErr.Code]; ok {
			return spec.exit
		}
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

	if isNetworkError(err) {
		return exitNetwork
	}
	return exitFailure
}

// describeFailure 把一次失败讲清楚：契约里有说明的先用说明，再附服务端原文。
func describeFailure(err error) string {
	var apiErr *apiclient.APIError
	if errors.As(err, &apiErr) {
		if spec, ok := failureSpecs[apiErr.Code]; ok {
			text := fmt.Sprintf("%s（%s）", spec.message, apiErr.Code)
			if apiErr.Message != "" {
				text += "；服务端：" + apiErr.Message
			}
			return text
		}
	}
	return err.Error()
}

// isNetworkError 判断是否为连接或超时类错误。
func isNetworkError(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// reportError 输出错误；带 --json 时给出结构化错误，便于程序判断。
func reportError(stderr io.Writer, err error, asJSON bool) {
	if !asJSON {
		fmt.Fprintf(stderr, "失败: %s\n", describeFailure(err))
		return
	}

	payload := map[string]any{"ok": false, "message": describeFailure(err)}
	var apiErr *apiclient.APIError
	if errors.As(err, &apiErr) {
		payload["status"] = apiErr.StatusCode
		payload["code"] = apiErr.Code
		payload["message"] = apiErr.Message
	} else if isNetworkError(err) {
		payload["code"] = "network_error"
	}

	encoder := json.NewEncoder(stderr)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(payload)
}
