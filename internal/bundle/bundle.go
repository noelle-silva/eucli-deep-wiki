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

// Create 在仓库目录里生成 bundle 文件。
// 先写到临时文件再改名，避免下载方拿到半截内容。
func Create(ctx context.Context, repoPath string, outputPath string, timeout time.Duration) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}

	temporary := outputPath + ".tmp"
	_ = os.Remove(temporary)

	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "git", "-c", "core.longpaths=true", "bundle", "create", temporary, "--all")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("git bundle: %w: %s", err, strings.TrimSpace(string(out)))
	}

	return os.Rename(temporary, outputPath)
}
