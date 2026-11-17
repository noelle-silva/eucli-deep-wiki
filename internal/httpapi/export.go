package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"eucli-deep-wiki/internal/bundle"
	"eucli-deep-wiki/internal/vault"
)

// exportState 串行化同一仓库的 bundle 生成，避免并发重复打包。
type exportState struct {
	mu sync.Mutex
}

func showArchive(repoVault *vault.Vault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		paths := r.URL.Query()["path"]

		targets := make([]vault.ArchiveTarget, 0, len(paths))
		if len(paths) == 0 {
			targets = append(targets, vault.ArchiveTarget{Repo: name})
		}
		for _, item := range paths {
			targets = append(targets, vault.ArchiveTarget{Repo: name, Path: item})
		}

		writeArchive(w, r, repoVault, targets)
	}
}

func showMultiArchive(repoVault *vault.Vault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		includes := r.URL.Query()["include"]
		if len(includes) == 0 {
			writeJSON(w, http.StatusBadRequest, apiError{
				Code:    "include_required",
				Message: "至少需要一个 include=<仓库>[:路径] 参数",
			})
			return
		}

		targets := make([]vault.ArchiveTarget, 0, len(includes))
		for _, item := range includes {
			repository, target := splitInclude(item)
			if repository == "" {
				writeJSON(w, http.StatusBadRequest, apiError{
					Code:    "invalid_include",
					Message: fmt.Sprintf("include=%q 缺少仓库名", item),
				})
				return
			}
			targets = append(targets, vault.ArchiveTarget{Repo: repository, Path: target})
		}

		writeArchive(w, r, repoVault, targets)
	}
}

func writeArchive(w http.ResponseWriter, r *http.Request, repoVault *vault.Vault, targets []vault.ArchiveTarget) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = vault.FormatZip
	}

	filename := "archive"
	if len(targets) == 1 && targets[0].Repo != "" {
		filename = targets[0].Repo
	}

	switch format {
	case vault.FormatZip:
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+".zip\"")
	case vault.FormatTarGz:
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+".tar.gz\"")
	default:
		writeJSON(w, http.StatusBadRequest, apiError{
			Code:    "invalid_format",
			Message: "format 只支持 zip 或 tar.gz",
		})
		return
	}

	if err := repoVault.WriteArchive(targets, format, w); err != nil {
		// 头已发出时无法再改状态码，只能中断连接让客户端感知失败。
		switch {
		case errors.Is(err, vault.ErrRepoNotFound):
			http.Error(w, "repository not found", http.StatusNotFound)
		case errors.Is(err, vault.ErrArchiveTooLarge):
			http.Error(w, "archive exceeds size limit", http.StatusRequestEntityTooLarge)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func showBundle(repoVault *vault.Vault, cacheDir string, timeout time.Duration, shared *exportState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")

		meta, err := repoVault.Meta(name)
		if err != nil {
			writeVaultError(w, err)
			return
		}
		if meta.Revision == "" {
			writeJSON(w, http.StatusBadRequest, apiError{
				Code:    "no_revision",
				Message: "仓库还没有可导出的提交",
			})
			return
		}

		cachePath := filepath.Join(cacheDir, safeName(name)+"-"+meta.Revision+".bundle")
		repoDir, err := repoVault.RepositoryDir(name)
		if err != nil {
			writeVaultError(w, err)
			return
		}

		shared.mu.Lock()
		if _, err := os.Stat(cachePath); err != nil {
			if err := bundle.Create(r.Context(), repoDir, cachePath, timeout); err != nil {
				shared.mu.Unlock()
				writeJSON(w, http.StatusInternalServerError, apiError{
					Code:    "bundle_failed",
					Message: err.Error(),
				})
				return
			}
		}
		shared.mu.Unlock()

		w.Header().Set("Content-Type", "application/x-git-bundle")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+name+".bundle\"")
		http.ServeFile(w, r, cachePath)
	}
}

func splitInclude(value string) (string, string) {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) == 1 {
		return strings.TrimSpace(parts[0]), ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func safeName(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")
	return replacer.Replace(name)
}
