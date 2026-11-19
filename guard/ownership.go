package guard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"eucli-deep-wiki-contract-guard/contract"
	"eucli-deep-wiki-contract-guard/harness"
)

// guardModule 是本守护者自己的模块路径：两侧的源码与模块定义里都不许出现它。
const guardModule = "eucli-deep-wiki-contract-guard"

// serverModule 与 clientModule 是两侧的模块路径。
const (
	serverModule = "eucli-deep-wiki-server"
	clientModule = "eucli-dw"
)

// OwnershipChecks 守「归属独立」这一组事实。
//
// 它不是接口契约，但它是这个守护者能成立的前提：谁也不依赖它，它才能在两侧
// 单独构建、单独发布时不存在，也不会被带进任何一方的产物。
func OwnershipChecks() []Check {
	return []Check{
		{
			ID:     "ownership.no_reference",
			Family: contract.FamilyOwnership,
			Title:  "两侧源码与模块定义都不引用守护模块",
			Run:    checkNoReference,
		},
		{
			ID:     "ownership.workspace",
			Family: contract.FamilyOwnership,
			Title:  "工作区文件把两侧与守护者组合起来，两侧模块都不需要守护者",
			Run:    checkWorkspace,
		},
		{
			ID:     "ownership.independent_build",
			Family: contract.FamilyOwnership,
			Title:  "两侧各自的构建与依赖闭包都不需要另一侧，也不需要守护模块",
			Run:    checkIndependentBuild,
		},
	}
}

// checkNoReference 扫描两侧源码：一个 import 都不许指向守护模块。
func checkNoReference(_ *harness.World, reporter *Reporter) {
	root, err := appRoot()
	if err != nil {
		reporter.Mismatch(contract.Both, "项目根目录", "能定位到 go.work", err.Error())
		return
	}

	for _, side := range []struct {
		name string
		dir  string
	}{
		{name: serverModule, dir: filepath.Join(root, "server")},
		{name: clientModule, dir: filepath.Join(root, "cli")},
	} {
		fact := side.name + " 引用守护模块"
		if err := scanForReference(side.dir, guardModule); err != nil {
			reporter.Mismatch(contract.Both, fact, "一处都没有", err.Error())
		}
	}
}

// scanForReference 在目录里找一段文本；找不到返回 nil。
func scanForReference(dir string, needle string) error {
	walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if extension := filepath.Ext(path); extension != ".go" && filepath.Base(path) != "go.mod" {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(content), needle) {
			return fmt.Errorf("%s 里出现了 %s", path, needle)
		}
		return nil
	})
	return walkErr
}

// checkWorkspace 核对工作区文件：三个模块都在，两侧的模块定义都不 require 守护者。
func checkWorkspace(_ *harness.World, reporter *Reporter) {
	root, err := appRoot()
	if err != nil {
		reporter.Mismatch(contract.Both, "项目根目录", "能定位到 go.work", err.Error())
		return
	}

	content, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err != nil {
		reporter.Mismatch(contract.Both, "工作区文件", "存在且可读", err.Error())
		return
	}
	text := string(content)
	for _, entry := range []string{"./server", "./cli", "./guard"} {
		if !strings.Contains(text, entry) {
			reporter.Mismatch(contract.Both, "工作区文件里的模块", "包含 "+entry, "缺失")
		}
	}

	for _, side := range []struct {
		name string
		path string
	}{
		{name: serverModule, path: filepath.Join(root, "server", "go.mod")},
		{name: clientModule, path: filepath.Join(root, "cli", "go.mod")},
	} {
		moduleContent, readErr := os.ReadFile(side.path)
		if readErr != nil {
			reporter.Mismatch(contract.Both, side.name+" 的模块定义", "存在且可读", readErr.Error())
			continue
		}
		if strings.Contains(string(moduleContent), guardModule) {
			reporter.Mismatch(contract.Both, side.name+" 的模块定义", "不出现 "+guardModule, "出现了")
		}
	}
}

// checkIndependentBuild 在关掉工作区的前提下分别构建两侧，并看一眼依赖闭包。
func checkIndependentBuild(_ *harness.World, reporter *Reporter) {
	root, err := appRoot()
	if err != nil {
		reporter.Mismatch(contract.Both, "项目根目录", "能定位到 go.work", err.Error())
		return
	}

	for _, side := range []struct {
		name string
		dir  string
		// forbidden 是不许出现在这个模块依赖闭包里的模块。
		forbidden []string
	}{
		{name: serverModule, dir: filepath.Join(root, "server"), forbidden: []string{clientModule, guardModule}},
		{name: clientModule, dir: filepath.Join(root, "cli"), forbidden: []string{serverModule, guardModule}},
	} {
		buildFact := side.name + " 在 GOWORK=off 下构建"
		if output, buildErr := runCommand(side.dir, "go", "build", "./..."); buildErr != nil {
			reporter.Mismatch(contract.Both, buildFact, "成功", buildErr.Error()+"；"+output)
		}

		modulesFact := side.name + " 的依赖闭包"
		output, listErr := runCommand(side.dir, "go", "list", "-m", "all")
		if listErr != nil {
			reporter.Mismatch(contract.Both, modulesFact, "能列出模块", listErr.Error()+"；"+output)
			continue
		}
		for _, forbidden := range side.forbidden {
			if containsModule(output, forbidden) {
				reporter.Mismatch(contract.Both, modulesFact, "不出现 "+forbidden, "出现了")
			}
		}
	}
}

// containsModule 判断 go list -m 的输出里有没有某个模块。
func containsModule(output string, module string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == module {
			return true
		}
	}
	return false
}

// runCommand 在指定目录跑一条命令，环境里关掉工作区：这样构建证明的是
// 「这一侧自己的模块定义够不够」，而不是「工作区里碰巧有别的模块」。
func runCommand(dir string, name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

// appRoot 从当前目录向上找 go.work，定位「这一份源码」的根。
func appRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.work")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("从当前目录向上找不到 go.work")
		}
		dir = parent
	}
}
