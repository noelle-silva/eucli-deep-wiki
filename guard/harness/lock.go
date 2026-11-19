package harness

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// HoldRemovalFailure 让「删掉整个仓库目录」这一步真的失败，返回恢复函数。
//
// 两种文件系统各自认一种条件，两种都上：
//   - 在仓库里按住一个打开的文件句柄：Windows 会拒绝删除被占用的文件；
//   - 把仓库里的一个目录改成只读：POSIX 会拒绝改动只读目录里的条目。
//
// 两种条件在另一种平台上都是无害的额外动作。这里不做「兜底」：如果两者都没能
// 让删除失败，用例会如实变红——那说明这套机制在这台机器上不成立，需要人来看。
func (world *World) HoldRemovalFailure(repository string, relativeFile string) (restore func(), err error) {
	repoPath := world.RepoPath(repository)
	restores := make([]func(), 0, 2)

	readOnlyDir := filepath.Dir(filepath.Join(repoPath, relativeFile))
	if err := os.Chmod(readOnlyDir, 0o555); err == nil {
		restores = append(restores, func() { _ = os.Chmod(readOnlyDir, DirectoryPerm) })
	}

	handle, openErr := os.Open(filepath.Join(repoPath, relativeFile))
	if openErr == nil {
		world.mu.Lock()
		world.heldHandles = append(world.heldHandles, handle)
		world.mu.Unlock()
		restores = append(restores, func() { _ = handle.Close() })
	}

	if len(restores) == 0 {
		return nil, fmt.Errorf("没有任何一种方式能占住 %s（chmod 与文件句柄都失败）", repoPath)
	}
	return func() {
		for index := len(restores) - 1; index >= 0; index-- {
			restores[index]()
		}
	}, nil
}

// DirectorySize 独立数一遍某个目录的字节数：用来核对「现测体积」是真测的。
func DirectorySize(path string) (int64, error) {
	var total int64
	walkErr := filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return err
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	if walkErr != nil {
		return 0, walkErr
	}
	return total, nil
}

// GitDirectorySize 是 DirectorySize 在 .git 上的特化。
func GitDirectorySize(repoPath string) (int64, error) {
	return DirectorySize(filepath.Join(repoPath, ".git"))
}
