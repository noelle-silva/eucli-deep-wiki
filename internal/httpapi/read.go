package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"eucli-deep-wiki/internal/vault"
)

// apiError 是统一的错误响应体。
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func listRepositories(repoVault *vault.Vault) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		names := repoVault.Repositories()
		metas := make([]vault.Meta, 0, len(names))
		for _, name := range names {
			meta, err := repoVault.Meta(name)
			if err != nil {
				continue
			}
			metas = append(metas, meta)
		}
		writeJSON(w, http.StatusOK, map[string]any{"repositories": metas})
	}
}

func showRepository(repoVault *vault.Vault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		meta, err := repoVault.Meta(r.PathValue("name"))
		if err != nil {
			writeVaultError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, meta)
	}
}

func showTree(repoVault *vault.Vault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tree, err := repoVault.Tree(r.PathValue("name"), r.URL.Query().Get("path"))
		if err != nil {
			writeVaultError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, tree)
	}
}

func showFile(repoVault *vault.Vault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requested := r.URL.Query().Get("path")
		if requested == "" {
			writeJSON(w, http.StatusBadRequest, apiError{Code: "path_required", Message: "path 参数必须提供"})
			return
		}

		file, err := repoVault.File(r.PathValue("name"), requested)
		if err != nil {
			writeVaultError(w, err)
			return
		}

		w.Header().Set("ETag", file.ETag)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Last-Modified", file.ModifiedAt.UTC().Format(http.TimeFormat))
		w.Header().Set("X-Repo-Revision", file.Revision)

		if match := r.Header.Get("If-None-Match"); match != "" && match == file.ETag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(file.Content)
	}
}

func searchRepository(repoVault *vault.Vault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		result, err := repoVault.Search(r.PathValue("name"), vault.SearchOptions{
			Query:  values.Get("q"),
			Path:   values.Get("path"),
			Offset: parseLimit(values.Get("offset")),
			Limit:  parseLimit(values.Get("limit")),
		})
		if err != nil {
			writeVaultError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func parseLimit(raw string) int {
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0
	}
	return value
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
	default:
		return http.StatusInternalServerError, "internal_error", err.Error()
	}
}
