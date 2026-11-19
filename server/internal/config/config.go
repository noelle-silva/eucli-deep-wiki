// Package config 负责服务自身运行参数的加载与校验。
//
// 仓库清单属于可重建的运行数据，由 repoindex 单独维护；这里只保存与
// 具体机器相关的设置，因此本包不含任何仓库字段。
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// minAPIKeyLength 是单个 key 的长度下限。
	minAPIKeyLength = 16
)

// 默认值。四个路径字段都是单层名字，最终都解析到 DataRoot 之下。
const (
	DefaultDataRoot          = "data"
	DefaultRepositoryRoot    = "repository"
	DefaultRepositoryIndex   = "repositories.json"
	DefaultSyncInterval      = "6h"
	DefaultGitCommandTimeout = "20m"
	DefaultMaxParallelSyncs  = 1
	DefaultHTTPAddress       = "127.0.0.1:9997"

	// CacheDirName 是产物缓存的目录名，落在 DataRoot 之下。
	// 生成方（接口层）与清理方（运维能力）都从这里取，不各写一份。
	CacheDirName = "cache"

	// DefaultHistoryMode 是未在索引里声明历史档位时采用的模式。
	DefaultHistoryMode = "shallow"
	// 体积保护阈值，单位 MB。
	DefaultGCThresholdMB = 2048
	DefaultGCGrowthMB    = 300
	DefaultSizeLimitMB   = 5120
)

// 历史档位。
const (
	HistoryShallow = "shallow"
	HistoryFull    = "full"
)

// Config 描述服务的运行形态。
type Config struct {
	DataRoot          string   `json:"dataRoot"`
	RepositoryRoot    string   `json:"repositoryRoot"`
	RepositoryIndex   string   `json:"repositoryIndex"`
	SyncInterval      string   `json:"syncInterval"`
	GitCommandTimeout string   `json:"gitCommandTimeout"`
	MaxParallelSyncs  int      `json:"maxParallelSyncs"`
	HTTPAddress       string   `json:"httpAddress"`
	APIKeys           []string `json:"apiKeys"`

	DefaultHistory string `json:"defaultHistory"`
	GCThresholdMB  int    `json:"gcThresholdMB"`
	GCGrowthMB     int    `json:"gcGrowthMB"`
	SizeLimitMB    int    `json:"sizeLimitMB"`

	SyncIntervalDuration      time.Duration `json:"-"`
	GitCommandTimeoutDuration time.Duration `json:"-"`
}

// RepositoryDir 返回参考仓库的落盘根目录。
func (c *Config) RepositoryDir() string {
	return filepath.Join(c.DataRoot, c.RepositoryRoot)
}

// RepositoryIndexPath 返回仓库索引文件的位置。
func (c *Config) RepositoryIndexPath() string {
	return filepath.Join(c.DataRoot, c.RepositoryIndex)
}

// CacheDir 返回产物缓存的目录。
func (c *Config) CacheDir() string {
	return filepath.Join(c.DataRoot, CacheDirName)
}

// Load 读取并校验配置文件。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(stripBOM(data), &cfg); err != nil {
		return nil, err
	}

	if err := cfg.applyDefaults(); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// stripBOM 去掉 UTF-8 BOM：Windows 编辑器保存的文件常带 BOM，
// 直接交给 json 解析会报 invalid character。
func stripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
}

// SetHTTPAddress 用命令行参数覆盖监听地址，并复用同一套校验。
func (c *Config) SetHTTPAddress(address string) error {
	if err := validateHTTPAddress(address); err != nil {
		return err
	}
	c.HTTPAddress = address
	return nil
}

func (c *Config) applyDefaults() error {
	if strings.TrimSpace(c.DataRoot) == "" {
		c.DataRoot = DefaultDataRoot
	}
	if strings.TrimSpace(c.RepositoryRoot) == "" {
		c.RepositoryRoot = DefaultRepositoryRoot
	}
	if strings.TrimSpace(c.RepositoryIndex) == "" {
		c.RepositoryIndex = DefaultRepositoryIndex
	}
	if strings.TrimSpace(c.SyncInterval) == "" {
		c.SyncInterval = DefaultSyncInterval
	}
	if strings.TrimSpace(c.GitCommandTimeout) == "" {
		c.GitCommandTimeout = DefaultGitCommandTimeout
	}
	if c.MaxParallelSyncs <= 0 {
		c.MaxParallelSyncs = DefaultMaxParallelSyncs
	}
	if strings.TrimSpace(c.DefaultHistory) == "" {
		c.DefaultHistory = DefaultHistoryMode
	}
	if c.GCThresholdMB <= 0 {
		c.GCThresholdMB = DefaultGCThresholdMB
	}
	if c.GCGrowthMB <= 0 {
		c.GCGrowthMB = DefaultGCGrowthMB
	}
	if c.SizeLimitMB <= 0 {
		c.SizeLimitMB = DefaultSizeLimitMB
	}
	if strings.TrimSpace(c.HTTPAddress) == "" {
		c.HTTPAddress = DefaultHTTPAddress
	}

	interval, err := time.ParseDuration(c.SyncInterval)
	if err != nil {
		return fmt.Errorf("parse syncInterval: %w", err)
	}
	c.SyncIntervalDuration = interval

	commandTimeout, err := time.ParseDuration(c.GitCommandTimeout)
	if err != nil {
		return fmt.Errorf("parse gitCommandTimeout: %w", err)
	}
	c.GitCommandTimeoutDuration = commandTimeout

	return nil
}

func (c *Config) validate() error {
	if c.SyncIntervalDuration <= 0 {
		return errors.New("syncInterval must be greater than zero")
	}
	if c.GitCommandTimeoutDuration <= 0 {
		return errors.New("gitCommandTimeout must be greater than zero")
	}
	if err := validateDataRoot(c.DataRoot); err != nil {
		return err
	}
	if err := validateLocalName(c.RepositoryRoot, "repositoryRoot"); err != nil {
		return err
	}
	if err := validateLocalName(c.RepositoryIndex, "repositoryIndex"); err != nil {
		return err
	}
	if err := validateHTTPAddress(c.HTTPAddress); err != nil {
		return err
	}
	if c.DefaultHistory != HistoryShallow && c.DefaultHistory != HistoryFull {
		return fmt.Errorf("defaultHistory must be %q or %q", HistoryShallow, HistoryFull)
	}
	return validateAPIKeys(c.APIKeys)
}

// validateAPIKeys 拒绝空 key、含空白字符的 key 与重复 key。
// 长度下限只是拦住「随手填一个词」的情况，真正的强度靠随机生成。
func validateAPIKeys(keys []string) error {
	if len(keys) == 0 {
		return errors.New("apiKeys must contain at least one key")
	}

	seen := make(map[string]struct{}, len(keys))
	for i, key := range keys {
		if strings.TrimSpace(key) != key || key == "" {
			return fmt.Errorf("apiKeys[%d] must not be empty or have surrounding whitespace", i)
		}
		if strings.ContainsAny(key, " \t\r\n") {
			return fmt.Errorf("apiKeys[%d] must not contain whitespace", i)
		}
		if len(key) < minAPIKeyLength {
			return fmt.Errorf("apiKeys[%d] must be at least %d characters", i, minAPIKeyLength)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("apiKeys[%d] is duplicated", i)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// validateDataRoot 允许绝对路径，也允许单层相对名字。
// 绝对路径用于把运行数据放到源码目录之外；相对名字按启动时的工作目录解析。
func validateDataRoot(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("dataRoot is required")
	}
	if strings.TrimSpace(value) != value {
		return errors.New("dataRoot must not have surrounding whitespace")
	}
	if filepath.IsAbs(value) {
		return nil
	}
	return validateLocalName(value, "dataRoot")
}

// validateLocalName 要求字段是单层名字，避免配置把数据写到工作区之外。
func validateLocalName(value string, field string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not have surrounding whitespace", field)
	}
	if !filepath.IsLocal(value) {
		return fmt.Errorf("%s must be a local path", field)
	}
	if strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("%s must be a single local directory name", field)
	}
	return nil
}

func validateHTTPAddress(address string) error {
	if strings.TrimSpace(address) == "" {
		return errors.New("httpAddress is required")
	}
	if strings.TrimSpace(address) != address {
		return errors.New("httpAddress must not have surrounding whitespace")
	}

	port, err := httpAddressPort(address)
	if err != nil {
		return err
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("httpAddress port must be a number: %w", err)
	}
	if portNumber <= 0 || portNumber > 65535 {
		return errors.New("httpAddress port must be between 1 and 65535")
	}
	return nil
}

func httpAddressPort(address string) (string, error) {
	_, port, err := net.SplitHostPort(address)
	if err == nil {
		if port == "" {
			return "", errors.New("httpAddress port is required")
		}
		return port, nil
	}

	if strings.HasPrefix(address, ":") && strings.Count(address, ":") == 1 {
		port = strings.TrimPrefix(address, ":")
		if port == "" {
			return "", errors.New("httpAddress port is required")
		}
		return port, nil
	}

	return "", fmt.Errorf("httpAddress must be :port or host:port: %w", err)
}
