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

	if !containsPair(args, "-c", longPathConfig) {
		t.Fatalf("gitArgs() = %v, want -c %s", args, longPathConfig)
	}
	if runtime.GOOS == "windows" && !containsPair(args, "-c", fileModeConfig) {
		t.Fatalf("gitArgs() = %v, want -c %s on windows", args, fileModeConfig)
	}
	if runtime.GOOS != "windows" && containsPair(args, "-c", fileModeConfig) {
		t.Fatalf("gitArgs() = %v, want no filemode override off windows", args)
	}

	tail := args[len(args)-len(base):]
	for i := range base {
		if tail[i] != base[i] {
			t.Fatalf("gitArgs() command args = %v, want %v", tail, base)
		}
	}
	if base[0] != "fetch" {
		t.Fatalf("gitArgs() mutated input: %v", base)
	}
}

func containsPair(args []string, flag string, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestSyncRecreatesRepositoryWithStaleLock(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := New(root, 30*time.Second)
	remoteURL := createLocalRepository(t, "fresh.txt", "fresh")

	first := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL})
	if first.Error != "" {
		t.Fatalf("first Sync() error = %q", first.Error)
	}

	target := filepath.Join(root, "example")
	lockPath := filepath.Join(target, ".git", "index.lock")
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	second := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL})
	if second.Error != "" {
		t.Fatalf("second Sync() error = %q", second.Error)
	}
	if second.Action != "recreated" {
		t.Fatalf("second Sync() action = %q, want recreated", second.Action)
	}
	assertMissing(t, lockPath)
	assertFileContent(t, filepath.Join(target, "fresh.txt"), "fresh")
}

func TestSyncAlignsModeOnlyChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := New(root, 30*time.Second)
	remoteURL := createLocalRepository(t, "fresh.txt", "fresh")

	first := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL})
	if first.Error != "" {
		t.Fatalf("first Sync() error = %q", first.Error)
	}

	target := filepath.Join(root, "example")
	runGit(t, target, "update-index", "--chmod=+x", "fresh.txt")

	second := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL})
	if second.Error != "" {
		t.Fatalf("second Sync() error = %q, want mode-only change tolerated", second.Error)
	}
	assertFileContent(t, filepath.Join(target, "fresh.txt"), "fresh")
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

func TestRemoteUnavailableMessage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		message string
		want    bool
	}{
		{
			name:    "仓库不存在",
			message: "git ls-remote origin: exit status 128: remote: Repository not found.",
			want:    true,
		},
		{
			name:    "路径不是 git 仓库",
			message: "git ls-remote origin: exit status 128: fatal: '/srv/gone.git' does not appear to be a git repository",
			want:    true,
		},
		{
			name:    "无法读取用户名",
			message: "git fetch origin: exit status 128: fatal: could not read Username for 'https://github.com': terminal prompts disabled",
			want:    true,
		},
		{
			name:    "无法读取密码",
			message: "git fetch origin: exit status 128: fatal: could not read Password for 'https://github.com': terminal prompts disabled",
			want:    true,
		},
		{
			name:    "网络中断",
			message: "git fetch --prune origin: exit status 128: fatal: unable to access 'https://github.com/x/y.git/': Recv failure: Connection was aborted",
			want:    false,
		},
		{
			name:    "锁冲突",
			message: "git fetch --prune origin: exit status 128: fatal: Unable to create '/repo/.git/index.lock': File exists.",
			want:    false,
		},
	}

	for _, tc := range cases {
		if got := remoteUnavailableMessage(tc.message); got != tc.want {
			t.Errorf("%s: remoteUnavailableMessage() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestClassifyRemoteUnavailableWrapsSentinel(t *testing.T) {
	t.Parallel()

	original := errors.New("git ls-remote origin: exit status 128: remote: Repository not found.")
	classified := classifyRemoteUnavailable(original)
	if !errors.Is(classified, ErrRemoteUnavailable) {
		t.Fatalf("classifyRemoteUnavailable() = %v, want ErrRemoteUnavailable", classified)
	}
	// 分类只是在错误链上加一个标记，原始错误必须仍然可查。
	if !errors.Is(classified, original) {
		t.Fatalf("classifyRemoteUnavailable() 丢失了原始错误: %v", classified)
	}

	transient := errors.New("git fetch origin: exit status 128: fatal: the remote end hung up unexpectedly")
	if got := classifyRemoteUnavailable(transient); errors.Is(got, ErrRemoteUnavailable) {
		t.Fatalf("瞬时故障被误判为远端不可用: %v", got)
	}
	if err := classifyRemoteUnavailable(nil); err != nil {
		t.Fatalf("classifyRemoteUnavailable(nil) = %v, want nil", err)
	}
}

func TestProbeRemoteAcceptsLiveRepository(t *testing.T) {
	t.Parallel()

	client := New(t.TempDir(), 30*time.Second)
	if err := client.ProbeRemote(context.Background(), createLocalRepository(t, "fresh.txt", "fresh")); err != nil {
		t.Fatalf("ProbeRemote() error = %v", err)
	}
}

func TestProbeRemoteReportsMissingRemote(t *testing.T) {
	t.Parallel()

	client := New(t.TempDir(), 30*time.Second)
	absent := fileURL(t, filepath.Join(t.TempDir(), "absent"))

	err := client.ProbeRemote(context.Background(), absent)
	if !errors.Is(err, ErrRemoteUnavailable) {
		t.Fatalf("ProbeRemote() error = %v, want ErrRemoteUnavailable", err)
	}
}

func TestProbeRemoteRequiresURL(t *testing.T) {
	t.Parallel()

	client := New(t.TempDir(), 30*time.Second)
	if err := client.ProbeRemote(context.Background(), "  "); err == nil {
		t.Fatal("ProbeRemote() error = nil, want url error")
	}
}

func TestSyncReportsRemoteUnavailableAfterRemoteDeleted(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := New(root, 30*time.Second)
	remote := createRepository(t, "fresh.txt", "fresh")
	remoteURL := fileURL(t, remote)

	first := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL})
	if first.Error != "" {
		t.Fatalf("first Sync() error = %q", first.Error)
	}
	if first.RemoteUnavailable {
		t.Fatal("成功同步不应标记远端不可用")
	}

	if err := os.RemoveAll(remote); err != nil {
		t.Fatalf("remove remote: %v", err)
	}

	second := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL})
	if second.Error == "" {
		t.Fatal("远端被删除后同步应当失败")
	}
	if !second.RemoteUnavailable {
		t.Fatalf("RemoteUnavailable = false, error = %q", second.Error)
	}
}

func TestRetryableNetworkMessage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		message string
		want    bool
	}{
		{
			name:    "接收中断",
			message: "git ls-remote --symref origin HEAD: exit status 128: fatal: unable to access 'https://github.com/x/y.git/': Failure when receiving data from the peer",
			want:    true,
		},
		{
			name:    "连接被中止",
			message: "git ls-remote --symref origin HEAD: exit status 128: fatal: unable to access 'https://github.com/x/y.git/': Recv failure: Connection was aborted",
			want:    true,
		},
		{
			name:    "认证失败",
			message: "git ls-remote origin: exit status 128: fatal: Authentication failed for 'https://github.com/x/y.git/'",
			want:    false,
		},
		{
			name:    "仓库不存在",
			message: "git ls-remote origin: exit status 128: remote: Repository not found.",
			want:    false,
		},
		{
			name:    "锁冲突",
			message: "git fetch --prune origin: exit status 128: fatal: Unable to create '/repo/.git/index.lock': File exists.",
			want:    false,
		},
	}

	for _, tc := range cases {
		if got := retryableNetworkMessage(tc.message); got != tc.want {
			t.Errorf("%s: retryableNetworkMessage() = %v, want %v", tc.name, got, tc.want)
		}
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

func TestSyncCompactsFullRepositoryInPlace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	client := New(root, 60*time.Second)
	remote := createRepository(t, "fresh.txt", "fresh")
	commitFile(t, remote, "second.txt", "second")
	remoteURL := fileURL(t, remote)

	first := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL})
	if first.Error != "" {
		t.Fatalf("first Sync() error = %q", first.Error)
	}

	target := filepath.Join(root, "example")
	if shallow, err := client.isShallowRepository(context.Background(), target); err != nil || shallow {
		t.Fatalf("初始应为完整仓库: shallow=%v err=%v", shallow, err)
	}

	second := client.Sync(context.Background(), Repository{Name: "example", URL: remoteURL, Shallow: true})
	if second.Error != "" {
		t.Fatalf("second Sync() error = %q", second.Error)
	}
	if second.Action != "compacted" {
		t.Fatalf("second Sync() action = %q, want compacted", second.Action)
	}

	shallow, err := client.isShallowRepository(context.Background(), target)
	if err != nil {
		t.Fatalf("isShallowRepository() error = %v", err)
	}
	if !shallow {
		t.Fatal("仓库未变成浅克隆")
	}
	assertFileContent(t, filepath.Join(target, "fresh.txt"), "fresh")
}
