// Package serviceprofile 是服务端画像（门牌号与口令）的唯一读写入口。
//
// 画像固定位于数据目录根下的 service-profile.json，以固定结构承载监听端口与访问钥匙：
// 平台按这个位置与结构查看、编辑它，服务端内部的监听与鉴权都只通过本包读写这份事实。
//
// 画像存在即为权威：配置文件里的 httpAddress 与 apiKeys 降级为「首次生成画像时的初值」，
// 不再决定运行期的端口与钥匙。画像不存在时按这份初值生成并落盘；内容不合法时启动直接
// 失败并说清原因，绝不静默改成默认值——那会让平台显示的口令与真正生效的口令分家。
package serviceprofile

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"eucli-deep-wiki-server/internal/config"
)

// FileName 是画像文件名，固定落在数据目录根下。
const FileName = "service-profile.json"

// randomKeyBytes 是随机口令的熵：32 字节随机数写成 64 个十六进制字符。
const randomKeyBytes = 32

// Profile 是一份通过校验的画像：运行期只认这两个事实。
type Profile struct {
	Port int
	Key  string
}

// Seed 是首次生成画像时来自配置文件的初值。
//
// 零值表示没有可用的初值：端口 0 与空口令都由生成规则补上默认端口与随机口令。
type Seed struct {
	Port int
	Key  string
}

// document 是画像文件的固定结构：字段与平台约定完全一致，一个不多一个不少。
//
// 两个字段都用指针：缺字段与「字段写成了零值」必须能分开，前者是不合法，后者是取值越界。
type document struct {
	Port *int    `json:"port"`
	Key  *string `json:"key"`
}

// Path 返回画像在数据目录下的位置。
//
// 平台按同一个位置读它（应用目录下的 data/service-profile.json），因此这里是唯一一处拼路径。
func Path(dataDir string) string {
	return filepath.Join(dataDir, FileName)
}

// Load 读取并校验画像；文件缺失时错误链保留 os.ErrNotExist。
func Load(dataDir string) (Profile, error) {
	path := Path(dataDir)
	payload, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, fmt.Errorf("读取服务画像 %s 失败: %w", path, err)
	}
	return parse(path, payload)
}

// Ensure 读取画像；文件缺失时按初值生成一份、落盘，并照它开工。
//
// 文件存在但内容不合法时直接返回错误，不做任何修补：画像是口令的权威，
// 静默改成默认值会让平台看到的口令与真正生效的口令不一致。
func Ensure(dataDir string, seed Seed) (Profile, error) {
	profile, err := Load(dataDir)
	if err == nil {
		return profile, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Profile{}, err
	}

	profile, err = generate(seed)
	if err != nil {
		return Profile{}, err
	}
	if err := write(dataDir, profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// generate 按规则生成画像：端口取初值的端口（不可用则默认端口），
// 口令取初值的口令（不可用则随机生成一把足够长的）。
func generate(seed Seed) (Profile, error) {
	profile := Profile{Port: config.DefaultPort, Key: seed.Key}
	if usablePort(seed.Port) {
		profile.Port = seed.Port
	}
	if usableKey(profile.Key) {
		return profile, nil
	}

	key, err := randomKey()
	if err != nil {
		return Profile{}, err
	}
	profile.Key = key
	return profile, nil
}

// usablePort 与 usableKey 判断初值能不能直接用：口径与画像校验完全相同。
func usablePort(port int) bool { return config.ValidatePort(port) == nil }

func usableKey(key string) bool { return config.ValidateAPIKey(key) == nil }

// randomKey 生成一把足够长的随机口令。
func randomKey() (string, error) {
	buffer := make([]byte, randomKeyBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("生成随机口令失败: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

// write 把画像落盘。
//
// 先写临时文件再改名：半份画像比没有画像更糟——画像是口令的权威，坏掉的画像会让服务端
// 起不来，而且没法靠配置文件修好。数据目录不存在时一并建出来，与索引、状态文件同一套口径。
func write(dataDir string, profile Profile) error {
	path := Path(dataDir)
	payload, err := json.MarshalIndent(document{Port: &profile.Port, Key: &profile.Key}, "", "  ")
	if err != nil {
		return fmt.Errorf("生成服务画像失败: %w", err)
	}
	payload = append(payload, '\n')

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("创建数据目录 %s 失败: %w", dataDir, err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, payload, 0o600); err != nil {
		return fmt.Errorf("写入服务画像 %s 失败: %w", path, err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("写入服务画像 %s 失败: %w", path, err)
	}
	return nil
}

// parse 解析并校验画像内容。
//
// 报错只说位置与规矩，不回显口令：口令进日志等于把钥匙写在门上。
func parse(path string, payload []byte) (Profile, error) {
	var decoded document
	if err := json.Unmarshal(stripBOM(payload), &decoded); err != nil {
		return Profile{}, fmt.Errorf("服务画像 %s 不是合法的 JSON: %w", path, err)
	}
	if decoded.Port == nil {
		return Profile{}, fmt.Errorf("服务画像 %s 缺少 port 字段（必须是 1-65535 的整数）", path)
	}
	if err := config.ValidatePort(*decoded.Port); err != nil {
		return Profile{}, fmt.Errorf("服务画像 %s 的 port 无效: %w", path, err)
	}
	if decoded.Key == nil {
		return Profile{}, fmt.Errorf("服务画像 %s 缺少 key 字段（至少 %d 个字符）", path, config.MinAPIKeyLength)
	}
	if err := config.ValidateAPIKey(*decoded.Key); err != nil {
		return Profile{}, fmt.Errorf("服务画像 %s 的 key 无效: %w", path, err)
	}
	return Profile{Port: *decoded.Port, Key: *decoded.Key}, nil
}

// stripBOM 去掉 UTF-8 BOM：Windows 编辑器保存的文件常带 BOM，
// 直接交给 json 解析会报 invalid character。
func stripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
}
