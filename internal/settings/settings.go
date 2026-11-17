// Package settings 负责 CLI 自己的连接配置：服务器地址与 key。
//
// 查找顺序固定为：显式路径 → EUCLI_DW_CONFIG 环境变量 → 平台默认位置。
// 平台默认只照顾各自系统的惯例，不感知任何本机目录规范；
// 需要特殊布局时用环境变量指向即可。
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// EnvConfigPath 是指向配置文件的环境变量名。
	EnvConfigPath = "EUCLI_DW_CONFIG"
	// FileName 是配置文件名。
	FileName = "config.json"

	appDirName = "eucli-dw"
)

// Settings 是 CLI 连接服务所需的最小信息。
type Settings struct {
	Server string `json:"server"`
	APIKey string `json:"apiKey"`
}

// ResolvePath 按优先级确定配置文件位置。
func ResolvePath(explicit string) (string, error) {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		return filepath.Abs(trimmed)
	}

	if env := strings.TrimSpace(os.Getenv(EnvConfigPath)); env != "" {
		if info, err := os.Stat(env); err == nil && info.IsDir() {
			return filepath.Join(env, FileName), nil
		}
		return env, nil
	}

	return defaultPath()
}

// Load 读取配置；文件不存在时返回 os.ErrNotExist。
func Load(path string) (*Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		return nil, err
	}
	return &loaded, nil
}

// Save 写入配置，并把目录与文件权限收紧到仅本人可读写。
func (s *Settings) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

// ValidateServer 校验服务器地址。
func ValidateServer(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return errors.New("server is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("server is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("server must start with http:// or https://")
	}
	if parsed.Host == "" {
		return errors.New("server must contain a host")
	}
	return nil
}

// ValidateAPIKey 校验 key 的基本形态。
func ValidateAPIKey(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return errors.New("apiKey is required")
	}
	if trimmed != value {
		return errors.New("apiKey must not have surrounding whitespace")
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return errors.New("apiKey must not contain whitespace")
	}
	return nil
}

// EnsureReady 确认配置足以发起请求。
func (s *Settings) EnsureReady() error {
	if err := ValidateServer(s.Server); err != nil {
		return err
	}
	if err := ValidateAPIKey(s.APIKey); err != nil {
		return err
	}
	return nil
}

func defaultPath() (string, error) {
	if runtime.GOOS == "windows" {
		profile := strings.TrimSpace(os.Getenv("USERPROFILE"))
		if profile == "" {
			return "", errors.New("USERPROFILE is not set")
		}
		return filepath.Join(profile, "."+appDirName, FileName), nil
	}

	if base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); base != "" {
		return filepath.Join(base, appDirName, FileName), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", appDirName, FileName), nil
}
