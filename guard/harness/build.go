// Package harness 为契约用例准备「真实现场」：两侧的真实程序、真实进程、
// 真实的磁盘布局，以及一个只做记录、不改请求的转发探针。
//
// 它不 import 任何一侧的包：两侧在它眼里就是两个可执行文件。因此这个包（以及
// 整个守护模块）既不属于服务端也不属于命令行，两侧的构建与产物都不需要它。
package harness

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// 两侧的模块目录与入口包。
const (
	serverModuleDir = "server"
	serverEntry     = "./cmd/eucli-deep-wiki-server"
	clientModuleDir = "cli"
	clientEntry     = "./cmd/eucli-dw"

	serverBinaryName = "eucli-deep-wiki-server.exe"
	clientBinaryName = "eucli-dw.exe"
)

// Binaries 是这次验证用到的两个真实程序。
type Binaries struct {
	// Server 是服务端可执行文件。
	Server string
	// Client 是命令行可执行文件。
	Client string
}

var (
	buildOnce   sync.Once
	builtBinary Binaries
	buildErr    error
	buildDir    string
)

// Build 构建两侧的真实程序，返回它们的路径。
//
// 构建走的是「各自模块、各自动手」这条路：GOWORK=off，因此构建能不能成功只取决于
// 这一侧自己的源码与模块定义——守护模块与另一侧都不参与。
func Build() (*Binaries, error) {
	buildOnce.Do(func() {
		builtBinary, buildErr = buildBinaries()
	})
	if buildErr != nil {
		return nil, buildErr
	}
	copied := builtBinary
	return &copied, nil
}

// CleanupBuild 删掉缓存的构建产物；由调用方在整轮验证结束时调用。
func CleanupBuild() {
	if buildDir != "" {
		_ = os.RemoveAll(buildDir)
		buildDir = ""
	}
}

// buildBinaries 是 Build 的实际动作。
func buildBinaries() (Binaries, error) {
	root, err := AppRoot()
	if err != nil {
		return Binaries{}, err
	}
	dir, err := os.MkdirTemp("", "eucli-contract-guard-bin-")
	if err != nil {
		return Binaries{}, fmt.Errorf("建立构建目录: %w", err)
	}
	buildDir = dir

	serverPath := filepath.Join(dir, serverBinaryName)
	if err := goBuild(filepath.Join(root, serverModuleDir), serverEntry, serverPath); err != nil {
		return Binaries{}, fmt.Errorf("构建服务端: %w", err)
	}

	clientPath := filepath.Join(dir, clientBinaryName)
	if err := goBuild(filepath.Join(root, clientModuleDir), clientEntry, clientPath); err != nil {
		return Binaries{}, fmt.Errorf("构建命令行: %w", err)
	}

	return Binaries{Server: serverPath, Client: clientPath}, nil
}

// goBuild 在一个模块目录里构建一个入口包。
func goBuild(moduleDir string, entry string, output string) error {
	command := exec.Command("go", "build", "-trimpath", "-o", output, entry)
	command.Dir = moduleDir
	command.Env = append(os.Environ(), "GOWORK=off")
	combined, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(combined)))
	}
	return nil
}

// AppRoot 从当前目录向上找 go.work，定位「这一份源码」的根。
func AppRoot() (string, error) {
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
