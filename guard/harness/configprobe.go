package harness

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// AcceptsConfig 用真实的 eucli-deep-wiki-server 程序判断一份配置能不能通过校验。
//
// 走的是服务端自己的启动路径：带 -once 起一次，只跑一轮同步就退出。
// 配置不合法时它会在加载阶段就失败（非零退出），因此退出码就是「这份配置行不行」
// 的答案——与生产启动读的是同一段代码。
func (world *World) AcceptsConfig(name string, content string) (bool, error) {
	binaries, err := Build()
	if err != nil {
		return false, err
	}

	configPath := filepath.Join(world.Root, "config-candidates", name+".json")
	if err := writeFile(configPath, content); err != nil {
		return false, err
	}

	var output bytes.Buffer
	command := exec.Command(binaries.Server, "-config", configPath, "-once")
	command.Stdout = &output
	command.Stderr = &output
	runErr := command.Run()
	if runErr == nil {
		return true, nil
	}

	var exitError *exec.ExitError
	if errors.As(runErr, &exitError) {
		// 非零退出是「服务端拒绝这份配置」的答案，不是探针本身失败。
		return false, nil
	}
	return false, fmt.Errorf("起服务端判断配置失败: %w；输出=%s", runErr, trim(output.String()))
}

// 现场里给「候选配置」用的数据根名：每次判断都用一份干净目录。
const candidateDataRootName = "candidate-data"

// CandidateConfig 拼一份候选服务端配置：只有必要字段，其余走默认值。
func (world *World) CandidateConfig(apiKey string) (string, error) {
	dataRoot := filepath.Join(world.Root, candidateDataRootName)
	if err := os.MkdirAll(dataRoot, DirectoryPerm); err != nil {
		return "", fmt.Errorf("建立候选数据根: %w", err)
	}
	content := fmt.Sprintf("{\n  \"dataRoot\": %q,\n  \"apiKeys\": [%q]\n}\n", dataRoot, apiKey)
	return content, nil
}
