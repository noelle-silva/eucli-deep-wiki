package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const longPathConfig = "core.longpaths=true"

var (
	errGitCommandTimeout  = errors.New("git command timed out")
	errGitCommandCanceled = errors.New("git command canceled")
)

type targetState int

const (
	targetMissing targetState = iota
	targetReady
	targetRebuild
)

type Repository struct {
	Name   string
	URL    string
	Branch string
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

type Client struct {
	root           string
	commandTimeout time.Duration
}

func New(root string, commandTimeout time.Duration) *Client {
	return &Client{root: root, commandTimeout: commandTimeout}
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

	state, err := c.inspectTarget(ctx, path, repo.URL)
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
	}

	return c.syncReadyTarget(ctx, repo, path)
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

	if _, err := c.run(ctx, path, "fetch", "--prune", "origin"); err != nil {
		return remote.branch, "fetch failed", err
	}
	if err := c.enforceResolvedRemoteState(ctx, path, remote.branch); err != nil {
		return remote.branch, "cleanup failed", err
	}

	return remote.branch, "updated", nil
}

func (c *Client) inspectTarget(ctx context.Context, path string, expectedURL string) (targetState, error) {
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

	matches, err := c.originMatches(ctx, path, expectedURL)
	if err != nil {
		return targetMissing, err
	}
	if !matches {
		return targetRebuild, nil
	}

	return targetReady, nil
}

func (c *Client) cloneAndEnforce(ctx context.Context, repo Repository, path string, successAction string, failureAction string) (string, string, error) {
	if _, err := c.run(ctx, "", "clone", repo.URL, path); err != nil {
		return "", failureAction, err
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
	if _, err := c.run(ctx, path, "checkout", "-B", branch, remoteBranch); err != nil {
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

	out, err := c.run(ctx, path, "ls-remote", "--symref", "origin", "HEAD")
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
	out, err := c.run(ctx, path, "ls-remote", "origin", "refs/heads/"+branch)
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

func (c *Client) run(ctx context.Context, dir string, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "git", gitArgs(args)...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if commandCtx.Err() != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return text, fmt.Errorf("%w: git %s after %s", errGitCommandTimeout, strings.Join(args, " "), c.commandTimeout)
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

func gitArgs(args []string) []string {
	result := make([]string, 0, len(args)+2)
	result = append(result, "-c", longPathConfig)
	result = append(result, args...)
	return result
}
