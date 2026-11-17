package gitops

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestGitArgsEnablesLongPathsForEveryCommand(t *testing.T) {
	t.Parallel()

	base := []string{"fetch", "--prune", "origin"}
	args := gitArgs(base)

	if args[0] != "-c" || args[1] != longPathConfig {
		t.Fatalf("gitArgs() prefix = %q %q, want -c %s", args[0], args[1], longPathConfig)
	}
	if args[2] != "fetch" || args[3] != "--prune" || args[4] != "origin" {
		t.Fatalf("gitArgs() command args = %v", args[2:])
	}
	if base[0] != "fetch" {
		t.Fatalf("gitArgs() mutated input: %v", base)
	}
}

func TestSyncRecreatesNonGitTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "example")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("make stale target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "stale.txt"), []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	client := New(root, 30*time.Second)
	result := client.Sync(context.Background(), Repository{
		Name: "example",
		URL:  createLocalRepository(t, "fresh.txt", "fresh"),
	})

	if result.Error != "" {
		t.Fatalf("Sync() error = %q", result.Error)
	}
	if result.Action != "recreated" {
		t.Fatalf("Sync() action = %q, want recreated", result.Action)
	}
	assertMissing(t, filepath.Join(target, "stale.txt"))
	assertExists(t, filepath.Join(target, "fresh.txt"))
}

func TestSyncRecreatesMismatchedOrigin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := New(root, 30*time.Second)
	firstURL := createLocalRepository(t, "first.txt", "first")
	secondURL := createLocalRepository(t, "second.txt", "second")

	first := client.Sync(context.Background(), Repository{Name: "example", URL: firstURL})
	if first.Error != "" {
		t.Fatalf("first Sync() error = %q", first.Error)
	}

	second := client.Sync(context.Background(), Repository{Name: "example", URL: secondURL})
	if second.Error != "" {
		t.Fatalf("second Sync() error = %q", second.Error)
	}
	if second.Action != "recreated" {
		t.Fatalf("second Sync() action = %q, want recreated", second.Action)
	}

	target := filepath.Join(root, "example")
	assertMissing(t, filepath.Join(target, "first.txt"))
	assertExists(t, filepath.Join(target, "second.txt"))
}

func TestSyncSkipsCleanCurrentRepository(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := New(root, 30*time.Second)
	remoteURL := createLocalRepository(t, "fresh.txt", "fresh")

	first := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL})
	if first.Error != "" {
		t.Fatalf("first Sync() error = %q", first.Error)
	}

	second := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL})
	if second.Error != "" {
		t.Fatalf("second Sync() error = %q", second.Error)
	}
	if second.Action != "skipped" {
		t.Fatalf("second Sync() action = %q, want skipped", second.Action)
	}
}

func TestSyncUpdatesWhenRemoteAdvances(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := New(root, 30*time.Second)
	remote := createRepository(t, "fresh.txt", "fresh")
	remoteURL := fileURL(t, remote)

	first := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL})
	if first.Error != "" {
		t.Fatalf("first Sync() error = %q", first.Error)
	}
	commitFile(t, remote, "fresh.txt", "new remote content")

	second := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL})
	if second.Error != "" {
		t.Fatalf("second Sync() error = %q", second.Error)
	}
	if second.Action != "updated" {
		t.Fatalf("second Sync() action = %q, want updated", second.Action)
	}
	assertFileContent(t, filepath.Join(root, "example", "fresh.txt"), "new remote content")
}

func TestSyncUpdatesDirtyRepositoryEvenWhenRemoteCurrent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := New(root, 30*time.Second)
	remoteURL := createLocalRepository(t, "fresh.txt", "fresh")

	first := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL})
	if first.Error != "" {
		t.Fatalf("first Sync() error = %q", first.Error)
	}
	targetFile := filepath.Join(root, "example", "fresh.txt")
	if err := os.WriteFile(targetFile, []byte("local change"), 0o600); err != nil {
		t.Fatalf("write local change: %v", err)
	}

	second := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL})
	if second.Error != "" {
		t.Fatalf("second Sync() error = %q", second.Error)
	}
	if second.Action != "updated" {
		t.Fatalf("second Sync() action = %q, want updated", second.Action)
	}
	assertFileContent(t, targetFile, "fresh")
}

func TestRemoveControlledTargetRejectsPathOutsideRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "root")
	outside := filepath.Join(t.TempDir(), "example")
	client := New(root, 30*time.Second)

	if err := client.removeControlledTarget(outside); err == nil {
		t.Fatal("removeControlledTarget() error = nil, want outside-root error")
	}
}

func TestSyncRejectsNonDirectoryRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "repository")
	if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write root file: %v", err)
	}

	client := New(root, 30*time.Second)
	result := client.Sync(context.Background(), Repository{
		Name: "example",
		URL:  createLocalRepository(t, "fresh.txt", "fresh"),
	})

	if result.Error == "" {
		t.Fatal("Sync() error = empty, want non-directory root error")
	}
}

func createLocalRepository(t *testing.T, fileName string, content string) string {
	t.Helper()

	return fileURL(t, createRepository(t, fileName, content))
}

func createRepository(t *testing.T, fileName string, content string) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, "", "init", dir)
	runGit(t, dir, "checkout", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	commitFile(t, dir, fileName, content)
	return dir
}

func commitFile(t *testing.T, dir string, fileName string, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(content), 0o600); err != nil {
		t.Fatalf("write repository file: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "update "+fileName)
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

func fileURL(t *testing.T, path string) string {
	t.Helper()

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	slashPath := filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String()
}

func assertExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertFileContent(t *testing.T, path string, expected string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != expected {
		t.Fatalf("%s content = %q, want %q", path, string(data), expected)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %s to be missing, got error %v", path, err)
	}
}
