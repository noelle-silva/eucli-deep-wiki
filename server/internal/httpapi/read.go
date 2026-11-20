package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"eucli-deep-wiki-server/internal/ops"
	"eucli-deep-wiki-server/internal/vault"
)

// apiError 是统一的错误响应体。
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// view 取出当前生效的只读视图。
//
// 请求进入时中间件已经刷新过运行期状态，因此这里拿到的就是磁盘上的最新一份。
func view(source Live) *vault.Vault {
	return source.Snapshot().Vault
}

// listRepositories 返回名单总览：仓库内容事实 + 同步运行事实 + 名单声明。
//
// 名单事实（地址、档位、是否落盘、可选现测体积）只由运维能力回答；
// 这里只负责把三份事实按仓库名合到一条记录里，不自己再算一遍。
// measure 为真时才现测体积，体积放在 measuredBytes 里，与「上次同步时
// 记录的 gitBytes」互不影响。
func listRepositories(manager *ops.Manager, states StateSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		measure, err := parseFlag("measure", r.URL.Query().Get("measure"), false)
		if err != nil {
			writeParamError(w, "invalid_flag", err)
			return
		}

		recorded := snapshotStates(states)
		facts := manager.List(ops.ListOptions{Measure: measure})

		repositories := make([]listedRepository, 0, len(facts))
		for _, item := range facts {
			repositories = append(repositories, composeListed(composeRepository(item.Meta, recorded), item))
		}
		writeJSON(w, http.StatusOK, map[string]any{"repositories": repositories})
	}
}

func showRepository(source Live, states StateSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		meta, err := view(source).Meta(r.PathValue("name"))
		if err != nil {
			writeVaultError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, composeRepository(meta, snapshotStates(states)))
	}
}

// showTree 展开目录。
//
// exclude 是调用方给的「按名字排除目录」清单：缺省为空表示不排除任何目录，
// 因此老的调用方一个字都不用改。
func showTree(source Live) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		dirsOnly, err := parseFlag("dirsOnly", values.Get("dirsOnly"), false)
		if err != nil {
			writeParamError(w, "invalid_flag", err)
			return
		}

		tree, treeErr := view(source).Tree(r.PathValue("name"), vault.TreeOptions{
			Path:     values.Get("path"),
			Depth:    parseCount(values.Get("depth")),
			DirsOnly: dirsOnly,
			Exclude:  vault.ParseExcludedDirs(values.Get("exclude")),
		})
		if treeErr != nil {
			writeVaultError(w, treeErr)
			return
		}
		writeJSON(w, http.StatusOK, tree)
	}
}

func showFile(source Live) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		requested := values.Get("path")
		if requested == "" {
			writeJSON(w, http.StatusBadRequest, apiError{Code: "path_required", Message: "path 参数必须提供"})
			return
		}

		span, err := vault.ParseLineRange(values.Get("lines"))
		if err != nil {
			writeParamError(w, "invalid_lines", err)
			return
		}
		maxBytes, err := parseByteCount(values.Get("maxBytes"))
		if err != nil {
			writeParamError(w, "invalid_bytes", err)
			return
		}

		file, err := view(source).File(r.PathValue("name"), vault.FileOptions{
			Path:     requested,
			Lines:    span,
			MaxBytes: maxBytes,
		})
		if err != nil {
			writeVaultError(w, err)
			return
		}

		writeFileHeaders(w, file)
		if match := r.Header.Get("If-None-Match"); match != "" && match == file.ETag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(file.Content)
	}
}

// writeFileHeaders 把「文件有多长、这次给了哪一段」写进响应头。
// 正文保持纯文本，切段与截断的事实只走头部，管道消费不受影响。
func writeFileHeaders(w http.ResponseWriter, file vault.File) {
	w.Header().Set("ETag", file.ETag)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Last-Modified", file.ModifiedAt.UTC().Format(http.TimeFormat))
	w.Header().Set("X-Repo-Revision", file.Revision)
	w.Header().Set("X-File-Total-Lines", strconv.Itoa(file.TotalLines))
	w.Header().Set("X-File-First-Line", strconv.Itoa(file.FirstLine))
	w.Header().Set("X-File-Last-Line", strconv.Itoa(file.LastLine))
	w.Header().Set("X-File-Size", strconv.FormatInt(file.Size, 10))
	w.Header().Set("X-File-Bytes", strconv.Itoa(len(file.Content)))
	if file.Truncated != "" {
		w.Header().Set("X-File-Truncated", file.Truncated)
	}
}

func searchRepository(source Live) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		result, err := view(source).Search(r.PathValue("name"), vault.SearchOptions{
			Query:   values.Get("q"),
			Path:    values.Get("path"),
			Offset:  parseCount(values.Get("offset")),
			Limit:   parseCount(values.Get("limit")),
			Context: parseCount(values.Get("context")),
		})
		if err != nil {
			writeVaultError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func findFiles(source Live) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		match, err := parseNameMatch(values.Get("match"))
		if err != nil {
			writeParamError(w, "invalid_match", err)
			return
		}

		result, findErr := view(source).Find(r.PathValue("name"), vault.FindOptions{
			Pattern: values.Get("name"),
			Path:    values.Get("path"),
			Match:   match,
			Limit:   vault.NormalizeFindLimit(parseCount(values.Get("limit"))),
			Offset:  parseCount(values.Get("offset")),
		})
		if findErr != nil {
			writeVaultError(w, findErr)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// parseNameMatch 解析文件名匹配方式；缺省即精确匹配。
func parseNameMatch(raw string) (vault.NameMatch, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "exact":
		return vault.NameMatchExact, nil
	case "substring":
		return vault.NameMatchSubstring, nil
	default:
		return vault.NameMatchExact, fmt.Errorf("match 只支持 exact 或 substring: %q", raw)
	}
}

// parseCount 取一个正整数参数；缺省、非法或非正都按 0（未指定）处理。
// 这几个参数的语义都由下游的 Normalize* 收敛，这里不必重复判断。
func parseCount(raw string) int {
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

// parseByteCount 解析字节上限；缺省按 0（不限制），非法值直接报错。
func parseByteCount(raw string) (int64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("maxBytes 必须是非负整数: %q", raw)
	}
	return value, nil
}

// parseFlag 解析开关型参数；空串按 fallback 处理，其余必须是合法的布尔写法。
func parseFlag(name string, raw string, fallback bool) (bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(trimmed)
	if err != nil {
		return false, fmt.Errorf("%s 必须是布尔值: %q", name, raw)
	}
	return value, nil
}

func writeParamError(w http.ResponseWriter, code string, err error) {
	writeJSON(w, http.StatusBadRequest, apiError{Code: code, Message: err.Error()})
}

func writeVaultError(w http.ResponseWriter, err error) {
	statusCode, code, message := classify(err)
	writeJSON(w, statusCode, apiError{Code: code, Message: message})
}

func classify(err error) (int, string, string) {
	switch {
	case errors.Is(err, vault.ErrRepoNotFound):
		return http.StatusNotFound, "repo_not_found", "仓库不存在或尚未同步"
	case errors.Is(err, vault.ErrNotFound):
		return http.StatusNotFound, "path_not_found", "路径不存在"
	case errors.Is(err, vault.ErrInvalidPath):
		return http.StatusBadRequest, "invalid_path", "路径不得越出仓库"
	case errors.Is(err, vault.ErrNotRegular):
		return http.StatusBadRequest, "not_a_file", "目标不是普通文件"
	case errors.Is(err, vault.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, "file_too_large", "文件超过读取上限"
	case errors.Is(err, vault.ErrBinary):
		return http.StatusUnsupportedMediaType, "not_text", "文件不是可读文本"
	case errors.Is(err, vault.ErrInvalidQuery):
		return http.StatusBadRequest, "query_required", "q 参数必须提供"
	case errors.Is(err, vault.ErrInvalidName):
		return http.StatusBadRequest, "name_required", err.Error()
	case errors.Is(err, vault.ErrInvalidLineRange):
		return http.StatusBadRequest, "invalid_lines", err.Error()
	default:
		return http.StatusInternalServerError, "internal_error", err.Error()
	}
}
