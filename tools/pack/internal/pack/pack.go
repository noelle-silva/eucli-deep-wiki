// Package pack 把服务端打成可交给商店化流程的基础成品包。
//
// 它只负责一件事：编译出服务端可执行文件，连同随包配置压成一个压缩包。
// 商店清单、图标归位、版本号补写都不在这里做——那是协议工具的职责，
// 两边分工的唯一接缝就是这个压缩包。
package pack

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// 随包文件的固定事实。应用身份与可执行文件名必须与自我介绍卡一致。
const (
	AppID          = "eucli-deep-wiki"
	ExecutableName = "eucli-deep-wiki-server.exe"

	serverDir      = "server"
	serverCommand  = "./cmd/eucli-deep-wiki-server"
	protocolDir    = ".fast-window-dev-protocol"
	bundleConfig   = "config.json"
	versionFile    = "server/internal/release/release.json"
	artifactSubdir = "dist"
)

// Artifact 是一次打包的结果。字段名即协议动作里 artifact 的取值路径。
type Artifact struct {
	ZipPath string `json:"zipPath"`
	ZipName string `json:"zipName"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

// Options 是一次打包的输入。
type Options struct {
	// RepoRoot 是应用根目录，里面有 server/ 与 .fast-window-dev-protocol/。
	RepoRoot string
	// OutputDir 是压缩包落点；为空时落在协议目录的 dist/ 下。
	OutputDir string
	// LogWriter 接收编译过程输出；为空时丢弃。它绝不会写到标准输出，
	// 因为标准输出要独占给最终的结果。
	LogWriter io.Writer
}

// Package 编译服务端并打出基础成品包。
func Package(options Options) (Artifact, error) {
	repo, err := resolveRepo(options.RepoRoot)
	if err != nil {
		return Artifact{}, err
	}

	version, err := readVersion(repo)
	if err != nil {
		return Artifact{}, err
	}

	configPath := filepath.Join(repo, protocolDir, bundleConfig)
	if _, err := os.Stat(configPath); err != nil {
		return Artifact{}, fmt.Errorf("随包配置不可用 %s: %w", configPath, err)
	}

	workDir, err := os.MkdirTemp("", "eucli-deep-wiki-pack-")
	if err != nil {
		return Artifact{}, fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(workDir)

	binaryPath := filepath.Join(workDir, ExecutableName)
	if err := compile(repo, binaryPath, options.LogWriter); err != nil {
		return Artifact{}, err
	}

	outDir := options.OutputDir
	if outDir == "" {
		outDir = filepath.Join(repo, protocolDir, artifactSubdir)
	}
	outDir, err = filepath.Abs(outDir)
	if err != nil {
		return Artifact{}, fmt.Errorf("解析产出目录失败: %w", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return Artifact{}, fmt.Errorf("创建产出目录失败: %w", err)
	}

	zipName := fmt.Sprintf("%s_%s_%s-%s.zip", AppID, version, runtime.GOOS, archLabel())
	zipPath := filepath.Join(outDir, zipName)

	entries := []bundleEntry{
		{source: binaryPath, name: ExecutableName},
		{source: configPath, name: bundleConfig},
	}
	if err := writeZip(zipPath, entries); err != nil {
		return Artifact{}, err
	}

	digest, err := fileDigest(zipPath)
	if err != nil {
		return Artifact{}, err
	}

	return Artifact{ZipPath: zipPath, ZipName: zipName, SHA256: digest, Version: version}, nil
}

// bundleEntry 是包内的一项：从哪读、在包里叫什么。
type bundleEntry struct {
	source string
	name   string
}

func resolveRepo(given string) (string, error) {
	repo := given
	if repo == "" {
		// 约定：在 tools/pack 目录里运行，应用根是它的上两级。
		working, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("读取当前目录失败: %w", err)
		}
		repo = filepath.Dir(filepath.Dir(working))
	}
	absolute, err := filepath.Abs(repo)
	if err != nil {
		return "", fmt.Errorf("解析应用根失败: %w", err)
	}
	if _, err := os.Stat(filepath.Join(absolute, serverDir)); err != nil {
		return "", fmt.Errorf("应用根里找不到 %s: %w", serverDir, err)
	}
	return absolute, nil
}

// readVersion 以版本文件为唯一事实源，不在别处重复版本号。
func readVersion(repo string) (string, error) {
	path := filepath.Join(repo, filepath.FromSlash(versionFile))
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取版本文件失败: %w", err)
	}
	var document struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(payload, &document); err != nil {
		return "", fmt.Errorf("解析版本文件失败: %w", err)
	}
	version := strings.TrimSpace(document.Version)
	if version == "" {
		return "", fmt.Errorf("版本文件缺少 version: %s", versionFile)
	}
	return version, nil
}

// compile 在服务端模块里单独构建，不读工作区、不看另一侧源码。
func compile(repo, output string, logWriter io.Writer) error {
	command := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", output, serverCommand)
	command.Dir = filepath.Join(repo, serverDir)
	command.Env = append(os.Environ(), "GOWORK=off", "CGO_ENABLED=0")
	command.Stdout = logWriter
	command.Stderr = logWriter
	if err := command.Run(); err != nil {
		return fmt.Errorf("编译服务端失败: %w", err)
	}
	return nil
}

func writeZip(target string, entries []bundleEntry) error {
	file, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("创建压缩包失败: %w", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	for _, entry := range entries {
		if err := addToZip(writer, entry); err != nil {
			writer.Close()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("写入压缩包失败: %w", err)
	}
	return nil
}

func addToZip(writer *zip.Writer, entry bundleEntry) error {
	source, err := os.Open(entry.source)
	if err != nil {
		return fmt.Errorf("读取包内文件失败 %s: %w", entry.source, err)
	}
	defer source.Close()

	header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
	header.SetMode(0o644)
	target, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("写入包内条目失败 %s: %w", entry.name, err)
	}
	if _, err := io.Copy(target, source); err != nil {
		return fmt.Errorf("复制包内条目失败 %s: %w", entry.name, err)
	}
	return nil
}

// archLabel 用商店里既有的叫法表达目标架构（与平台上其他成品包同名同序）。
func archLabel() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	default:
		return runtime.GOARCH
	}
}

func fileDigest(path string) (string, error) {	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("读取成品失败: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("计算校验值失败: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
