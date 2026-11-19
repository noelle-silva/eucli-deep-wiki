// Package release 是服务端版本事实的唯一来源。
//
// 版本号写在模块内的 release.json 里并在构建时编进产物；自我介绍卡
// （.fast-window-dev-protocol/fw-app.json）的 versionSource 指向同一个文件，
// 因此平台显示的版本与程序自己打印的版本永远是同一份事实，不会各写一份再对不上。
//
// 改版本号只改 release.json：代码里没有任何版本号的字面量。
package release

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// SourcePath 是版本文件相对服务端模块根的位置。
//
// 自我介绍卡（.fast-window-dev-protocol/fw-app.json）按应用根取路径，那里的写法是
// "server/internal/release/release.json"——同一份文件，只是基准目录不同。
const SourcePath = "internal/release/release.json"

//go:embed release.json
var source []byte

// Info 是版本事实。
type Info struct {
	Version string `json:"version"`
}

// formalVersion 是版本号的形状：三段十进制数，与平台核对版本时的口径一致。
var formalVersion = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// Load 解析并校验编进产物的版本事实。
//
// 校验失败属于构建期错误（改坏了 release.json），启动时快速失败而不是悄悄用一个占位版本。
func Load() (Info, error) {
	return parse(source)
}

// parse 解析并校验一份版本事实。
func parse(payload []byte) (Info, error) {
	var info Info
	if err := json.Unmarshal(payload, &info); err != nil {
		return Info{}, fmt.Errorf("解析版本文件 %s 失败: %w", SourcePath, err)
	}

	version := strings.TrimSpace(info.Version)
	if !formalVersion.MatchString(version) {
		return Info{}, fmt.Errorf("版本文件 %s 的 version 无效：必须是三段版本号（例如 0.1.0），实际为 %q",
			SourcePath, info.Version)
	}
	return Info{Version: version}, nil
}
