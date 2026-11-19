package cache

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRepository 准备一个有一次提交的本地仓库，用于验证真实打包路径。
func newRepository(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, "", "init", dir)
	runGit(t, dir, "checkout", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write repository file: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}
