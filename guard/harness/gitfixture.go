package harness

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"eucli-deep-wiki-contract-guard/contract"
)

// GitRoles 是现场里全部 git 仓库角色。
var GitRoles = []string{
	contract.RepoGitFull, contract.RepoGitShallow, contract.RepoGitFullHistory,
	contract.RepoGitDetached, contract.RepoGitBadOrigin, contract.RepoGitLockedConfig,
	contract.RepoGitBlackHole,
}

// seedGit 铺真实版本管理现场：一个本地来源仓库、一个裸仓库、若干个不同状态的克隆。
func (world *World) seedGit() error {
	if err := world.writeConfig("60s"); err != nil {
		return err
	}

	// 黑障仓库的地址要等黑障起来才知道，因此它不进静态清单。
	seeds := []repositorySeed{
		{Name: contract.RepoGitFull, URL: "https://example.com/full.git", Branch: "main", History: contract.HistoryShallow},
		{Name: contract.RepoGitShallow, URL: "https://example.com/shallow.git", Branch: "main", History: contract.HistoryShallow},
		{Name: contract.RepoGitFullHistory, URL: "https://example.com/full-history.git", Branch: "main", History: contract.HistoryFull},
		{Name: contract.RepoGitDetached, URL: "https://example.com/detached.git", History: contract.HistoryShallow},
		{Name: contract.RepoGitBadOrigin, URL: "https://example.com/bad-origin.git", History: contract.HistoryShallow},
		{Name: contract.RepoGitLockedConfig, URL: "https://example.com/locked-config.git", History: contract.HistoryShallow},
	}
	if err := world.planIndex(seeds); err != nil {
		return err
	}

	origin, err := world.buildOrigin()
	if err != nil {
		return err
	}

	clones := []struct {
		name    string
		shallow bool
		after   func(dest string) error
	}{
		{name: contract.RepoGitFull},
		{name: contract.RepoGitShallow, shallow: true},
		{name: contract.RepoGitFullHistory},
		{name: contract.RepoGitDetached, after: detachHead},
		{name: contract.RepoGitBadOrigin, after: func(dest string) error {
			return runGit("-C", dest, "remote", "set-url", "origin", origin+"-gone")
		}},
		{name: contract.RepoGitLockedConfig, after: lockConfig},
	}
	for _, clone := range clones {
		dest := world.RepoPath(clone.name)
		if err := world.clone(origin, dest, clone.shallow); err != nil {
			return fmt.Errorf("克隆 %s: %w", clone.name, err)
		}
		if clone.after == nil {
			continue
		}
		if err := clone.after(dest); err != nil {
			return fmt.Errorf("准备 %s: %w", clone.name, err)
		}
	}
	return nil
}

// seedGitBusy 铺「同步真的卡在网上」的现场：一个黑障 + 一份空名单。
//
// 名单先留空：服务端启动时那一轮同步因此立刻结束，现场从空闲开始。
// 需要占住闸门时，由用例通过真实接口把一个远端指向黑障的仓库加进名单，
// 再触发一轮真实同步——那一轮会卡在网络握手上，闸门被真实占住。
func (world *World) seedGitBusy() error {
	if err := world.writeConfig("60s"); err != nil {
		return err
	}
	hole, err := NewBlackHole()
	if err != nil {
		return fmt.Errorf("建立黑障: %w", err)
	}
	world.Hole = hole
	return world.planIndex(nil)
}

// buildOrigin 做出一个有提交的本地来源仓库，并克隆成裸仓库供各份克隆共用。
func (world *World) buildOrigin() (string, error) {
	source := filepath.Join(world.Root, "git-source")
	if err := writeFile(filepath.Join(source, "file.txt"), "first\n"); err != nil {
		return "", err
	}
	if err := runGit("init", "-q", "-b", "main", source); err != nil {
		return "", err
	}
	if err := runGit("-C", source, "add", "-A"); err != nil {
		return "", err
	}
	if err := runGit("-C", source, "commit", "-q", "-m", "first"); err != nil {
		return "", err
	}

	origin := filepath.Join(world.Root, "origin.git")
	if err := runGit("clone", "-q", "--bare", source, origin); err != nil {
		return "", err
	}
	return origin, nil
}

// clone 克隆一份现场仓库；本地克隆要变浅必须走 file:// 协议。
func (world *World) clone(origin string, dest string, shallow bool) error {
	if err := os.MkdirAll(filepath.Dir(dest), DirectoryPerm); err != nil {
		return err
	}
	if !shallow {
		return runGit("clone", "-q", origin, dest)
	}

	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(origin)}).String()
	return runGit("clone", "-q", "--depth", "1", fileURL, dest)
}

// detachHead 把 HEAD 从分支上摘下来。
func detachHead(dest string) error {
	return runGit("-C", dest, "checkout", "-q", "--detach", "HEAD")
}

// lockConfig 占住 .git/config：此后任何一次 git config 写入都会失败。
func lockConfig(dest string) error {
	return writeFile(filepath.Join(dest, ".git", "config.lock"), "")
}

// runGit 跑一条真实 git 命令。身份与提示都写死，避免读用户配置、避免等输入。
func runGit(args ...string) error {
	command := exec.Command("git", args...)
	command.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=contract-guard",
		"GIT_AUTHOR_EMAIL=guard@example.invalid",
		"GIT_COMMITTER_NAME=contract-guard",
		"GIT_COMMITTER_EMAIL=guard@example.invalid",
	)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

// GitAvailable 说明这台机器上能不能做真实版本管理现场。
func GitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
