package harness

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Outcome 是一次真实命令行调用的结果。
type Outcome struct {
	// Args 是真实传给命令行的全部参数。
	Args []string
	// Exit 是进程退出码。
	Exit int
	// Stdout 与 Stderr 是它的两份输出。
	Stdout string
	Stderr string
	// Requests 是这次调用真正发出去的请求（按顺序）。
	Requests []Request
}

// Describe 把一次调用压成一行，放进失败说明里。
func (outcome Outcome) Describe() string {
	return fmt.Sprintf("退出码 %d；stderr=%q；请求=%d 个", outcome.Exit, trim(outcome.Stderr), len(outcome.Requests))
}

// Command 是真实命令行的驱动器。
//
// 它执行的是构建出来的 eucli-dw 程序本身：连接信息来自命令行自己的配置文件，
// 它不知道服务端是怎么装起来的，也不读服务端的任何东西。
type Command struct {
	binary      string
	configPath  string
	downloadDir string
	recorder    *Recorder
}

// newCommand 建立一个命令行驱动器；downloadDir 用来兜住下载类命令的落点。
func newCommand(binary string, configPath string, downloadDir string) (*Command, error) {
	if err := os.MkdirAll(downloadDir, DirectoryPerm); err != nil {
		return nil, fmt.Errorf("建立下载目录: %w", err)
	}
	return &Command{binary: binary, configPath: configPath, downloadDir: downloadDir}, nil
}

// ConfigPath 是命令行自己的配置文件位置。
func (command *Command) ConfigPath() string {
	return command.configPath
}

// OutputPath 给下载类命令一个落点，避免往工作区写文件。
func (command *Command) OutputPath(name string) string {
	return filepath.Join(command.downloadDir, name)
}

// Run 执行一次命令行调用。
//
// args 是命令与它自己的参数；extra 追加在后面，用于临时覆盖连接参数。
// 连接配置一律用 --config 指到本次现场的配置文件，命令行因此只读它自己那份。
func (command *Command) Run(args []string, extra ...string) Outcome {
	full := make([]string, 0, len(args)+len(extra)+2)
	full = append(full, args...)
	full = append(full, extra...)
	full = append(full, "--config", command.configPath)
	return command.run(full)
}

// Help 跑一次帮助，用于读命令自己写出来的参数上限与缺省值。
func (command *Command) Help(args ...string) Outcome {
	return command.run(append(args, "-h"))
}

// run 执行一次程序调用。
func (command *Command) run(full []string) Outcome {
	var stdout, stderr bytes.Buffer
	process := exec.Command(command.binary, full...)
	process.Dir = command.downloadDir
	process.Stdout = &stdout
	process.Stderr = &stderr

	if command.recorder != nil {
		command.recorder.Reset()
	}
	err := process.Run()

	outcome := Outcome{Args: full, Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		var exitError *exec.ExitError
		if ok := asExitError(err, &exitError); ok {
			outcome.Exit = exitError.ExitCode()
		} else {
			outcome.Exit = -1
			outcome.Stderr += fmt.Sprintf("\n无法执行命令行: %v", err)
		}
	}
	if command.recorder != nil {
		outcome.Requests = command.recorder.Requests()
	}
	return outcome
}

// asExitError 把错误转成进程退出错误。
func asExitError(err error, target **exec.ExitError) bool {
	if exitError, ok := err.(*exec.ExitError); ok {
		*target = exitError
		return true
	}
	return false
}

// trim 把多行输出压成一行，便于放进断言消息。
func trim(value string) string {
	trimmed := []rune(value)
	if len(trimmed) > 200 {
		trimmed = append(trimmed[:200], '…')
	}
	result := string(trimmed)
	replaced := make([]rune, 0, len(result))
	for _, character := range result {
		if character == '\n' || character == '\r' || character == '\t' {
			replaced = append(replaced, ' ')
			continue
		}
		replaced = append(replaced, character)
	}
	return string(replaced)
}
