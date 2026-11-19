// Package bundle 把本地仓库打包成 git bundle 文件。
package bundle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TemporarySuffix 是生成过程中的临时文件后缀。
//
// 产物先写临时文件再改名，因此「半截文件」只会以这个后缀出现。缓存清理
// 靠它认出「这不是成品」，不去删正在生成的内容。
const TemporarySuffix = ".tmp"

// Create 在仓库目录里生成 bundle 文件。
// 先写到临时文件再改名，避免下载方拿到半截内容；上次中断留下的临时文件会先清掉。
func Create(ctx context.Context, repoPath string, outputPath string, timeout time.Duration) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}

	temporary := outputPath + TemporarySuffix
	cleanup(temporary)

	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "git", "-c", "core.longpaths=true", "bundle", "create", temporary, "--all")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		cleanup(temporary)
		return fmt.Errorf("git bundle: %w: %s", err, strings.TrimSpace(string(out)))
	}

	if err := os.Rename(temporary, outputPath); err != nil {
		cleanup(temporary)
		return err
	}
	return nil
}

// cleanup 移除临时文件与 git 可能留下的锁文件。
func cleanup(temporary string) {
	_ = os.Remove(temporary)
	_ = os.Remove(temporary + ".lock")
}
