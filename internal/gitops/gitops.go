package gitops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	longPathConfig = "core.longpaths=true"
	fileModeConfig = "core.filemode=false"
)

var (
	errGitCommandTimeout  = errors.New("git command timed out")
	errGitCommandCanceled = errors.New("git command canceled")

	// ErrRemoteUnavailable 表示远端仓库已经不可用：被删除、转为私有，
	// 或者当前环境没有访问它的凭据。它属于确定性失败，重试没有意义。
	ErrRemoteUnavailable = errors.New("remote repository unavailable")
)

type targetState int

const (
	targetMissing targetState = iota
	targetReady
	targetRebuild
	targetDepthMismatch
)

type Repository struct {
	Name    string
	URL     string
	Branch  string
	Shallow bool
}

type Result struct {
	Name       string        `json:"name"`
	URL        string        `json:"url"`
	Path       string        `json:"path"`
	Branch     string        `json:"branch,omitempty"`
	Action     string        `json:"action"`
	StartedAt  time.Time     `json:"startedAt"`
	FinishedAt time.Time     `json:"finishedAt"`
	Duration   time.Duration `json:"duration"`
	Error      string        `json:"error,omitempty"`
	// RemoteUnavailable 说明本次失败是「远端仓库不可用」，供上层判定仓库是否已经消失。
	RemoteUnavailable bool `json:"remoteUnavailable,omitempty"`
}

type localState struct {
	branch   string
	revision string
	clean    bool
}

type remoteHead struct {
	branch   string
	revision string
}

func (s localState) matches(head remoteHead) bool {
	return s.clean && s.branch == head.branch && s.revision == head.revision
}

// TimeoutFunc 返回当前生效的单条 git 命令超时。
//
// 用函数而不是固定值：改配置里的 gitCommandTimeout 立刻生效，
// 不需要重启服务。
type TimeoutFunc func() time.Duration

// FixedTimeout 返回固定超时，供不需要运行期调整的调用方使用。
func FixedTimeout(timeout time.Duration) TimeoutFunc {
	return func() time.Duration { return timeout }
}

type Client struct {
	root    string
	timeout TimeoutFunc
}

// New 构建 git 客户端；root 是仓库根目录，timeout 在每次执行命令时现取。
func New(root string, timeout TimeoutFunc) *Client {
	return &Client{root: root, timeout: timeout}
}

func (c *Client) Sync(ctx context.Context, repo Repository) Result {
	startedAt := time.Now()
	result := Result{
		Name:      repo.Name,
		URL:       repo.URL,
		Path:      filepath.Join(c.root, repo.Name),
		StartedAt: startedAt,
	}

	branch, action, err := c.sync(ctx, repo, result.Path)
	result.Branch = branch
	result.Action = action
	result.FinishedAt = time.Now()
	result.Duration = result.FinishedAt.Sub(result.StartedAt)
	if err != nil {
		result.Error = err.Error()
		result.RemoteUnavailable = errors.Is(err, ErrRemoteUnavailable)
	}
	return result
}

func (c *Client) sync(ctx context.Context, repo Repository, path string) (string, string, error) {
	if err := c.ensureRootDirectory(); err != nil {
		return "", "failed", err
	}
	if err := c.ensureControlledTarget(path); err != nil {
		return "", "failed", err
	}

	state, err := c.inspectTarget(ctx, path, repo.URL, repo.Shallow)
	if err != nil {
		return "", "failed", err
	}

	switch state {
	case targetMissing:
		return c.cloneAndEnforce(ctx, repo, path, "cloned", "clone failed")
	case targetRebuild:
		if err := c.removeControlledTarget(path); err != nil {
			return "", "recreate failed", err
		}
		return c.cloneAndEnforce(ctx, repo, path, "recreated", "recreate failed")
	case targetDepthMismatch:
		branch, action, err := c.convertDepth(ctx, repo, path)
		if err != nil {
			// 就地转换失败时退回重建，保证仓库最终可用。
			if removeErr := c.removeControlledTarget(path); removeErr != nil {
				return "", "recreate failed", removeErr
			}
			rebuiltBranch, rebuiltAction, rebuildErr := c.cloneAndEnforce(ctx, repo, path, "recreated", "recreate failed")
			if rebuildErr != nil {
				return rebuiltBranch, rebuiltAction, rebuildErr
			}
			return rebuiltBranch, rebuiltAction, nil
		}
		return branch, action, nil
	}

	return c.syncReadyTarget(ctx, repo, path)
}

// convertDepth 在不重新下载工作树的前提下切换历史深度。
func (c *Client) convertDepth(ctx context.Context, repo Repository, path string) (string, string, error) {
	if repo.Shallow {
		branch, err := c.shrinkToShallow(ctx, path)
		if err != nil {
			return branch, "compact failed", err
		}
		return branch, "compacted", nil
	}

	branch, err := c.expandToFull(ctx, path)
	if err != nil {
		return branch, "expand failed", err
	}
	return branch, "expanded", nil
}

// IsShallow 判断仓库当前是否是浅克隆。
func (c *Client) IsShallow(ctx context.Context, path string) (bool, error) {
	return c.isShallowRepository(ctx, path)
}

// Compact 把已存在的完整仓库就地转为浅克隆。
func (c *Client) Compact(ctx context.Context, path string) error {
	_, err := c.shrinkToShallow(ctx, path)
	return err
}

// ProbeRemote 只做一次 ls-remote 探测远端是否可达。
//
// 探测直接打远端地址，既不读取也不改动本地仓库，因此本地副本缺失或有损时
// 也能判断远端状态。
func (c *Client) ProbeRemote(ctx context.Context, url string) error {
	if strings.TrimSpace(url) == "" {
		return errors.New("repository url is required")
	}
	_, err := c.runNetwork(ctx, "", "ls-remote", "--heads", url)
	return err
}

// shrinkToShallow 把完整仓库就地变浅。
//
// 只切深度还不够：克隆时带下来的成百上千个远端分支与标签仍然引用旧历史，
// gc 清不掉。因此这里同时收窄 refspec、删除无关引用，再清理对象。
func (c *Client) shrinkToShallow(ctx context.Context, path string) (string, error) {
	branch, err := c.currentBranch(ctx, path)
	if err != nil {
		return "", err
	}
	if branch == "" {
		return "", errors.New("cannot compact a detached HEAD")
	}

	if err := c.restrictFetchRefspec(ctx, path, branch); err != nil {
		return branch, err
	}
	if _, err := c.runNetwork(ctx, path, "fetch", "--depth", "1", "--update-shallow", "origin", branch); err != nil {
		return branch, err
	}
	if err := c.pruneReferences(ctx, path, branch); err != nil {
		return branch, err
	}
	if _, err := c.run(ctx, path, "reflog", "expire", "--expire=now", "--all"); err != nil {
		return branch, err
	}
	if err := c.GarbageCollect(ctx, path); err != nil {
		return branch, err
	}
	return branch, nil
}

// restrictFetchRefspec 让后续 fetch 只跟一个分支，避免再把所有分支拉回来。
func (c *Client) restrictFetchRefspec(ctx context.Context, path string, branch string) error {
	if _, err := c.run(ctx, path, "config", "--unset-all", "remote.origin.fetch"); err != nil {
		if isCommandControlError(err) || !isGitExitError(err) {
			return err
		}
	}

	spec := fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)
	if _, err := c.run(ctx, path, "config", "--add", "remote.origin.fetch", spec); err != nil {
		return err
	}
	return nil
}

// pruneReferences 删除目标分支以外的远端引用与全部标签。
func (c *Client) pruneReferences(ctx context.Context, path string, branch string) error {
	keep := fmt.Sprintf("refs/remotes/origin/%s", branch)

	out, err := c.run(ctx, path, "for-each-ref", "--format=%(refname)", "refs/remotes", "refs/tags")
	if err != nil {
		return err
	}

	var deletions []string
	for _, ref := range strings.Split(out, "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" || ref == keep || ref == "refs/remotes/origin/HEAD" {
			continue
		}
		deletions = append(deletions, "delete "+ref)
	}
	if len(deletions) == 0 {
		return nil
	}

	input := strings.Join(deletions, "\n") + "\n"
	_, err = c.runInput(ctx, path, input, "update-ref", "--stdin")
	return err
}

// runInput 运行需要从标准输入喂数据的 git 命令。
func (c *Client) runInput(ctx context.Context, dir string, input string, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "git", gitArgs(args)...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, text)
	}
	return text, nil
}

// expandToFull 把浅克隆补成完整历史，工作树保持不变。
func (c *Client) expandToFull(ctx context.Context, path string) (string, error) {
	branch, err := c.currentBranch(ctx, path)
	if err != nil {
		return "", err
	}

	if _, err := c.runNetwork(ctx, path, "fetch", "--unshallow", "origin"); err != nil {
		return branch, err
	}
	if err := c.enforceResolvedRemoteState(ctx, path, branch); err != nil {
		return branch, err
	}
	return branch, nil
}

func (c *Client) syncReadyTarget(ctx context.Context, repo Repository, path string) (string, string, error) {
	local, err := c.inspectLocalState(ctx, path)
	if err != nil {
		return "", "cleanup failed", err
	}

	remote, err := c.inspectRemoteHead(ctx, path, repo.Branch)
	if err != nil {
		return "", "remote check failed", err
	}
	if local.matches(remote) {
		return remote.branch, "skipped", nil
	}

	fetchArgs := []string{"fetch", "--prune"}
	if repo.Shallow {
		fetchArgs = append(fetchArgs, "--depth", "1")
	}
	fetchArgs = append(fetchArgs, "origin")
	if _, err := c.runNetwork(ctx, path, fetchArgs...); err != nil {
		return remote.branch, "fetch failed", err
	}
	if err := c.enforceResolvedRemoteState(ctx, path, remote.branch); err != nil {
		return remote.branch, "cleanup failed", err
	}

	return remote.branch, "updated", nil
}

func (c *Client) inspectTarget(ctx context.Context, path string, expectedURL string, shallow bool) (targetState, error) {
	stat, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return targetMissing, nil
	}
	if err != nil {
		return targetMissing, fmt.Errorf("inspect repository path: %w", err)
	}
	if stat.Mode()&os.ModeSymlink != 0 || !stat.IsDir() {
		return targetRebuild, nil
	}

	isGitRepo, err := c.isGitRepository(ctx, path)
	if err != nil {
		return targetMissing, err
	}
	if !isGitRepo {
		return targetRebuild, nil
	}

	hasHead, err := c.hasHead(ctx, path)
	if err != nil {
		return targetMissing, err
	}
	if !hasHead {
		return targetRebuild, nil
	}

	if c.hasStaleLock(path) {
		return targetRebuild, nil
	}

	isShallow, err := c.isShallowRepository(ctx, path)
	if err != nil {
		return targetMissing, err
	}
	if isShallow != shallow {
		return targetDepthMismatch, nil
	}

	matches, err := c.originMatches(ctx, path, expectedURL)
	if err != nil {
		return targetMissing, err
	}
	if !matches {
		return targetRebuild, nil
	}

	return targetReady, nil
}

// hasStaleLock 判断参考仓库里是否残留 git 锁文件。
// 服务对同一个仓库严格串行同步，残留锁只可能来自上一次异常中断，
// 这类仓库按不干净处理，直接重建。
func (c *Client) hasStaleLock(path string) bool {
	_, err := os.Lstat(filepath.Join(path, ".git", "index.lock"))
	return err == nil
}

func (c *Client) cloneAndEnforce(ctx context.Context, repo Repository, path string, successAction string, failureAction string) (string, string, error) {
	cloneArgs := []string{"clone"}
	if repo.Shallow {
		cloneArgs = append(cloneArgs, "--depth", "1")
	}
	cloneArgs = append(cloneArgs, repo.URL, path)
	if _, err := c.run(ctx, "", cloneArgs...); err != nil {
		// 克隆阶段的“仓库不存在 / 需要凭据”同样属于远端不可用。
		return "", failureAction, classifyRemoteUnavailable(err)
	}
	branch, err := c.enforceRemoteState(ctx, path, repo.Branch)
	if err != nil {
		return branch, failureAction, err
	}
	return branch, successAction, nil
}

func (c *Client) removeControlledTarget(path string) error {
	if err := c.ensureControlledTarget(path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func (c *Client) ensureRootDirectory() error {
	stat, err := os.Lstat(c.root)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(c.root, 0o755); err != nil {
			return fmt.Errorf("prepare repository root: %w", err)
		}
		stat, err = os.Lstat(c.root)
	}
	if err != nil {
		return fmt.Errorf("inspect repository root: %w", err)
	}
	if stat.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("repository root must not be a symlink: %s", c.root)
	}
	if !stat.IsDir() {
		return fmt.Errorf("repository root must be a directory: %s", c.root)
	}
	return nil
}

func (c *Client) ensureControlledTarget(path string) error {
	absRoot, err := filepath.Abs(c.root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve repository path: %w", err)
	}

	absRoot = filepath.Clean(absRoot)
	absPath = filepath.Clean(absPath)
	if !samePath(filepath.Dir(absPath), absRoot) {
		return fmt.Errorf("refusing to remove path outside repository root: %s", path)
	}
	return nil
}

func (c *Client) enforceRemoteState(ctx context.Context, path string, configuredBranch string) (string, error) {
	branch, err := c.resolveBranch(ctx, path, configuredBranch)
	if err != nil {
		return "", err
	}
	return branch, c.enforceResolvedRemoteState(ctx, path, branch)
}

func (c *Client) enforceResolvedRemoteState(ctx context.Context, path string, branch string) error {
	if err := c.discardLocalState(ctx, path, "HEAD"); err != nil {
		return err
	}

	remoteBranch := "origin/" + branch
	// -f 直接丢弃本地改动：参考仓库的语义就是镜像远端，安全中止只会让同步卡死。
	if _, err := c.run(ctx, path, "checkout", "-f", "-B", branch, remoteBranch); err != nil {
		return err
	}
	if err := c.discardLocalState(ctx, path, remoteBranch); err != nil {
		return err
	}

	return nil
}

func (c *Client) discardLocalState(ctx context.Context, path string, target string) error {
	if _, err := c.run(ctx, path, "reset", "--hard", target); err != nil {
		return err
	}
	if _, err := c.run(ctx, path, "clean", "-ffdx"); err != nil {
		return err
	}
	return nil
}

func (c *Client) inspectLocalState(ctx context.Context, path string) (localState, error) {
	branch, err := c.currentBranch(ctx, path)
	if err != nil {
		return localState{}, err
	}
	revision, err := c.revision(ctx, path, "HEAD")
	if err != nil {
		return localState{}, err
	}
	clean, err := c.isLocalStateClean(ctx, path)
	if err != nil {
		return localState{}, err
	}

	return localState{
		branch:   branch,
		revision: revision,
		clean:    clean,
	}, nil
}

func (c *Client) inspectRemoteHead(ctx context.Context, path string, configuredBranch string) (remoteHead, error) {
	if configuredBranch != "" {
		revision, err := c.remoteBranchRevision(ctx, path, configuredBranch)
		return remoteHead{branch: configuredBranch, revision: revision}, err
	}

	out, err := c.runNetwork(ctx, path, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return remoteHead{}, err
	}
	return parseRemoteDefaultHead(out)
}

func (c *Client) isLocalStateClean(ctx context.Context, path string) (bool, error) {
	out, err := c.run(ctx, path, "status", "--porcelain=v1", "--ignored")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

func (c *Client) currentBranch(ctx context.Context, path string) (string, error) {
	out, err := c.run(ctx, path, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		if isCommandControlError(err) || !isGitExitError(err) {
			return "", err
		}
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

func (c *Client) revision(ctx context.Context, path string, ref string) (string, error) {
	out, err := c.run(ctx, path, "rev-parse", "--verify", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c *Client) remoteBranchRevision(ctx context.Context, path string, branch string) (string, error) {
	out, err := c.runNetwork(ctx, path, "ls-remote", "origin", "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	revision := parseLSRemoteRevision(out)
	if revision == "" {
		return "", fmt.Errorf("remote branch %q not found", branch)
	}
	return revision, nil
}

func parseRemoteDefaultHead(out string) (remoteHead, error) {
	var head remoteHead
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "ref:" && fields[2] == "HEAD" {
			head.branch = strings.TrimPrefix(fields[1], "refs/heads/")
			continue
		}
		if len(fields) == 2 && fields[1] == "HEAD" {
			head.revision = fields[0]
		}
	}
	if head.branch == "" {
		return remoteHead{}, errors.New("remote default branch is empty")
	}
	if head.revision == "" {
		return remoteHead{}, errors.New("remote default revision is empty")
	}
	return head, nil
}

func parseLSRemoteRevision(out string) string {
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return ""
	}
	return fields[0]
}

func (c *Client) resolveBranch(ctx context.Context, path string, configuredBranch string) (string, error) {
	if configuredBranch != "" {
		return configuredBranch, nil
	}

	_, _ = c.run(ctx, path, "remote", "set-head", "origin", "--auto")
	out, err := c.run(ctx, path, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", err
	}

	branch := strings.TrimSpace(out)
	branch = strings.TrimPrefix(branch, "origin/")
	if branch == "" {
		return "", errors.New("remote default branch is empty")
	}
	return branch, nil
}

// isShallowRepository 判断本地仓库是否是浅克隆。
func (c *Client) isShallowRepository(ctx context.Context, path string) (bool, error) {
	out, err := c.run(ctx, path, "rev-parse", "--is-shallow-repository")
	if err != nil {
		if isCommandControlError(err) || !isGitExitError(err) {
			return false, err
		}
		return false, nil
	}
	return strings.TrimSpace(out) == "true", nil
}

func (c *Client) isGitRepository(ctx context.Context, path string) (bool, error) {
	out, err := c.run(ctx, path, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		if isCommandControlError(err) || !isGitExitError(err) {
			return false, err
		}
		return false, nil
	}
	return strings.TrimSpace(out) == "true", nil
}

func (c *Client) hasHead(ctx context.Context, path string) (bool, error) {
	_, err := c.run(ctx, path, "rev-parse", "--verify", "HEAD")
	if err != nil {
		if isCommandControlError(err) || !isGitExitError(err) {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

func (c *Client) originMatches(ctx context.Context, path string, expected string) (bool, error) {
	out, err := c.run(ctx, path, "config", "--get", "remote.origin.url")
	if err != nil {
		if isCommandControlError(err) || !isGitExitError(err) {
			return false, err
		}
		return false, nil
	}

	actual := strings.TrimSpace(out)
	return normalizeURL(actual) == normalizeURL(expected), nil
}

func normalizeURL(raw string) string {
	return strings.TrimSuffix(strings.TrimSpace(raw), ".git")
}

func samePath(left string, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func isCommandControlError(err error) bool {
	return errors.Is(err, errGitCommandTimeout) || errors.Is(err, errGitCommandCanceled)
}

func isGitExitError(err error) bool {
	var exitError *exec.ExitError
	return errors.As(err, &exitError)
}

// 网络型 git 命令的重试策略：远端偶发断流不该让一个仓库整轮掉线。
const (
	networkAttempts    = 3
	networkBaseBackoff = 300 * time.Millisecond
)

// permanentNetworkFailures 是重试也不会好转的确定性失败。
var permanentNetworkFailures = []string{
	"Authentication failed",
	"could not read Username",
	"could not read Password",
	"Repository not found",
	"does not appear to be a git repository",
	"Permission denied",
	"index.lock",
	"cannot lock ref",
}

// remoteUnavailableMarkers 是远端被删除或转为私有后 git 固定给出的文本。
// 它们都在 permanentNetworkFailures 里，因此不会进入重试，只做分类。
var remoteUnavailableMarkers = []string{
	"Repository not found",
	"does not appear to be a git repository",
	"could not read Username",
	"could not read Password",
}

// runNetwork 用于 clone 之外的网络型命令，遇到瞬时故障按指数退避重试。
func (c *Client) runNetwork(ctx context.Context, dir string, args ...string) (string, error) {
	backoff := networkBaseBackoff
	var output string
	var lastErr error

	for attempt := 0; attempt < networkAttempts; attempt++ {
		out, err := c.run(ctx, dir, args...)
		if err == nil {
			return out, nil
		}
		output, lastErr = out, err
		if attempt == networkAttempts-1 || !isRetryableNetworkError(err) {
			break
		}

		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}

	return output, classifyRemoteUnavailable(lastErr)
}

// classifyRemoteUnavailable 给「远端仓库不可用」打上 sentinel，让上层用
// errors.Is 判断，而不用去解析错误文本。
func classifyRemoteUnavailable(err error) error {
	if err == nil || !remoteUnavailableMessage(err.Error()) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrRemoteUnavailable, err)
}

func remoteUnavailableMessage(message string) bool {
	for _, marker := range remoteUnavailableMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func isRetryableNetworkError(err error) bool {
	if isCommandControlError(err) || !isGitExitError(err) {
		return false
	}
	return retryableNetworkMessage(err.Error())
}

// retryableNetworkMessage 判断失败文本是否属于可重试的瞬时网络故障。
func retryableNetworkMessage(message string) bool {
	for _, marker := range permanentNetworkFailures {
		if strings.Contains(message, marker) {
			return false
		}
	}
	return true
}

func (c *Client) run(ctx context.Context, dir string, args ...string) (string, error) {
	timeout := c.timeout()
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "git", gitArgs(args)...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if commandCtx.Err() != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return text, fmt.Errorf("%w: git %s after %s", errGitCommandTimeout, strings.Join(args, " "), timeout)
		}
		if errors.Is(commandCtx.Err(), context.Canceled) {
			return text, fmt.Errorf("%w: git %s", errGitCommandCanceled, strings.Join(args, " "))
		}
		return text, fmt.Errorf("git %s stopped: %w", strings.Join(args, " "), commandCtx.Err())
	}
	if err != nil {
		if text == "" {
			return text, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return text, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, text)
	}
	return text, nil
}

// gitArgs 为每条命令补上跨平台必需的行为开关。
// Windows 的 NTFS 不保留可执行位，若不关闭 filemode，仓库会因为权限位差异
// 长期显示为“有本地改动”，既无法跳过也无法对齐。
func gitArgs(args []string) []string {
	prefix := []string{"-c", longPathConfig}
	if runtime.GOOS == "windows" {
		prefix = append(prefix, "-c", fileModeConfig)
	}

	result := make([]string, 0, len(args)+len(prefix))
	result = append(result, prefix...)
	result = append(result, args...)
	return result
}

// GarbageCollect 合并松散对象并清掉不可达对象，用于控制 .git 体积。
// 这一步可能比普通命令慢得多，因此单独放宽超时。
func (c *Client) GarbageCollect(ctx context.Context, path string) error {
	commandCtx, cancel := context.WithTimeout(ctx, c.timeout()*3)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "git", gitArgs([]string{"gc", "--prune=now"})...)
	cmd.Dir = path
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git gc: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// GitDirectorySize 统计 .git 目录占用的字节数。
func (c *Client) GitDirectorySize(path string) (int64, error) {
	root := filepath.Join(path, ".git")

	var total int64
	walkErr := filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
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
