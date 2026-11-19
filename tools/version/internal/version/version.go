// Package version 维护应用版本号。
//
// 版本只有一处出处：服务端模块里的 release.json。这个包是它唯一的读写入口，
// 打包工具也读同一份文件，保证「发布出去的版本」与「程序自称的版本」不会分家。
package version

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReleaseFile 是版本事实相对应用根的固定位置。
const ReleaseFile = "server/internal/release/release.json"

// Bump 是允许的提升档位。
type Bump string

const (
	BumpPatch Bump = "patch"
	BumpMinor Bump = "minor"
	BumpMajor Bump = "major"
)

// ParseBump 把外部给的档位收敛成受支持的值。
func ParseBump(raw string) (Bump, error) {
	switch Bump(strings.TrimSpace(raw)) {
	case BumpPatch:
		return BumpPatch, nil
	case BumpMinor:
		return BumpMinor, nil
	case BumpMajor:
		return BumpMajor, nil
	default:
		return "", fmt.Errorf("不支持的提升档位 %q（只接受 patch / minor / major）", raw)
	}
}

// Current 读出当前版本。
func Current(repoRoot string) (string, error) {
	document, err := read(repoRoot)
	if err != nil {
		return "", err
	}
	return document.Version, nil
}

// Apply 按档位提升版本并写回，返回提升前后的版本。
func Apply(repoRoot string, bump Bump) (before string, after string, err error) {
	document, err := read(repoRoot)
	if err != nil {
		return "", "", err
	}
	numbers, err := parse(document.Version)
	if err != nil {
		return "", "", err
	}
	next := numbers.bump(bump).String()
	document.Version = next

	if err := write(repoRoot, document); err != nil {
		return "", "", err
	}
	return numbers.String(), next, nil
}

type document struct {
	Version string `json:"version"`
	// raw 保留原文件的其余字段，写回时不丢东西。
	raw map[string]json.RawMessage
}

func pathOf(repoRoot string) string {
	return filepath.Join(repoRoot, filepath.FromSlash(ReleaseFile))
}

func read(repoRoot string) (document, error) {
	payload, err := os.ReadFile(pathOf(repoRoot))
	if err != nil {
		return document{}, fmt.Errorf("读取版本文件失败: %w", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return document{}, fmt.Errorf("解析版本文件失败: %w", err)
	}

	var version string
	if raw, ok := fields["version"]; ok {
		if err := json.Unmarshal(raw, &version); err != nil {
			return document{}, fmt.Errorf("版本文件的 version 必须是字符串: %w", err)
		}
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return document{}, fmt.Errorf("版本文件缺少 version: %s", ReleaseFile)
	}
	return document{Version: version, raw: fields}, nil
}

func write(repoRoot string, document document) error {
	fields := document.raw
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	encoded, err := json.Marshal(document.Version)
	if err != nil {
		return fmt.Errorf("编码版本号失败: %w", err)
	}
	fields["version"] = encoded

	payload, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return fmt.Errorf("编码版本文件失败: %w", err)
	}
	payload = append(payload, '\n')

	target := pathOf(repoRoot)
	if err := os.WriteFile(target, payload, 0o644); err != nil {
		return fmt.Errorf("写入版本文件失败: %w", err)
	}
	return nil
}

// numbers 是三段式版本号的内部表示。
type numbers struct {
	major int
	minor int
	patch int
}

func parse(value string) (numbers, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return numbers{}, fmt.Errorf("版本号必须是三段式 x.y.z：%q", value)
	}
	result := numbers{}
	targets := []*int{&result.major, &result.minor, &result.patch}
	for index, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" || strings.TrimLeft(trimmed, "0123456789") != "" {
			return numbers{}, fmt.Errorf("版本号第 %d 段不是非负整数：%q", index+1, part)
		}
		value := 0
		if _, err := fmt.Sscanf(trimmed, "%d", &value); err != nil {
			return numbers{}, fmt.Errorf("版本号第 %d 段无法解析：%q", index+1, part)
		}
		*targets[index] = value
	}
	return result, nil
}

func (n numbers) bump(step Bump) numbers {
	switch step {
	case BumpMajor:
		return numbers{major: n.major + 1}
	case BumpMinor:
		return numbers{major: n.major, minor: n.minor + 1}
	default:
		return numbers{major: n.major, minor: n.minor, patch: n.patch + 1}
	}
}

func (n numbers) String() string {
	return fmt.Sprintf("%d.%d.%d", n.major, n.minor, n.patch)
}
