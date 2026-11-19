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
	// MinAPIKeyLength 是单把钥匙的长度下限。
	//
	// 配置文件里的钥匙列表与画像里的口令共用这一个尺子：画像的口令就是接口钥匙，
	// 两处口径一旦分家，画像一生成就会被自己拒绝。
	MinAPIKeyLength = 16

	// 端口取值区间。配置文件的监听地址与画像的端口共用这一个尺子。
	portMinValue = 1
	portMaxValue = 65535
)

// 默认值。四个路径字段都是单层名字，最终都解析到 DataRoot 之下。
const (
	DefaultDataRoot          = "data"
	DefaultRepositoryRoot    = "repository"
	DefaultRepositoryIndex   = "repositories.json"
	DefaultSyncInterval      = "6h"
	DefaultGitCommandTimeout = "20m"
	DefaultMaxParallelSyncs  = 1

	// DefaultPort 是没给出可用监听地址时采用的端口。
	// 它与 DefaultHTTPAddress 里的端口是同一个事实，由用例守住一致性。
	DefaultPort        = 9997
	DefaultHTTPAddress = "127.0.0.1:9997"

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
	DataRoot          string `json:"dataRoot"`
	RepositoryRoot    string `json:"repositoryRoot"`
	RepositoryIndex   string `json:"repositoryIndex"`
	SyncInterval      string `json:"syncInterval"`
	GitCommandTimeout string `json:"gitCommandTimeout"`
	MaxParallelSyncs  int    `json:"maxParallelSyncs"`

	// HTTPAddress 与 APIKeys 是配置文件里的取值。
	//
	// 服务画像（数据目录下的 service-profile.json）生成之后，这两个字段被 AdoptProfile 接管：
	// HTTPAddress 只剩监听宿主还有效，APIKeys 只在「首次生成画像」时作口令初值。
	// 运行期的事实以接管后的值为准，见 live.Snapshot.Config。
	HTTPAddress string   `json:"httpAddress"`
	APIKeys     []string `json:"apiKeys"`

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

// ListenPort 返回 httpAddress 里的端口号；拆不出来时返回 0。
//
// 0 不是合法端口，调用方据此认出「没有可用的端口初值」，不用自己再判一次空。
func (c *Config) ListenPort() int {
	port, err := httpAddressPortNumber(c.HTTPAddress)
	if err != nil {
		return 0
	}
	return port
}

// FirstAPIKey 返回第一把钥匙；没有钥匙时返回空串。
//
// 空串不是合法钥匙，调用方据此认出「没有可用的口令初值」。
func (c *Config) FirstAPIKey() string {
	if len(c.APIKeys) == 0 {
		return ""
	}
	return c.APIKeys[0]
}

// AdoptProfile 让服务画像接管运行期的监听端口与口令。
//
// 画像存在即为权威：接管之后 httpAddress 只剩监听宿主有用（画像不记录宿主），
// apiKeys 只在首次生成画像时当初值。命令行给出的监听地址在它之后套用，
// 因此显式指定仍然最高。
//
// 校验先做完再落值：失败的调用不会留下半新半旧的配置。
func (c *Config) AdoptProfile(port int, key string) error {
	host, _, err := net.SplitHostPort(c.HTTPAddress)
	if err != nil {
		return fmt.Errorf("httpAddress 拆不出监听宿主: %w", err)
	}
	if err := ValidatePort(port); err != nil {
		return fmt.Errorf("画像端口不可用: %w", err)
	}
	if err := ValidateAPIKey(key); err != nil {
		return fmt.Errorf("画像口令不可用: %w", err)
	}

	c.HTTPAddress = net.JoinHostPort(host, strconv.Itoa(port))
	c.APIKeys = []string{key}
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

// ValidateAPIKey 校验单把钥匙：非空、不含空白、长度不低于 MinAPIKeyLength。
//
// 这是「一把可用的接口钥匙」的唯一定义：配置文件的钥匙列表与画像的口令都过它。
// 长度下限只是拦住「随手填一个词」的情况，真正的强度靠随机生成。
func ValidateAPIKey(key string) error {
	if key == "" {
		return errors.New("key must not be empty")
	}
	if strings.TrimSpace(key) != key {
		return errors.New("key must not have surrounding whitespace")
	}
	if strings.ContainsAny(key, " \t\r\n") {
		return errors.New("key must not contain whitespace")
	}
	if len(key) < MinAPIKeyLength {
		return fmt.Errorf("key must be at least %d characters", MinAPIKeyLength)
	}
	return nil
}

// ValidatePort 校验端口取值区间。
//
// 配置文件里的监听地址与画像里的端口共用这一个尺子。
func ValidatePort(port int) error {
	if port < portMinValue || port > portMaxValue {
		return fmt.Errorf("port must be between %d and %d", portMinValue, portMaxValue)
	}
	return nil
}

// validateAPIKeys 校验钥匙列表：逐把过 ValidateAPIKey，再拒绝重复。
//
// 允许整项不写：首次启动会由服务画像生成一把强口令并落盘，之后以画像为准。
// 写了的就必须合法——给了却写错，说明人有意指定过，这时候静默换成随机会更难查。
func validateAPIKeys(keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(keys))
	for i, key := range keys {
		if err := ValidateAPIKey(key); err != nil {
			return fmt.Errorf("apiKeys[%d]: %w", i, err)
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

	_, err := httpAddressPortNumber(address)
	return err
}

// httpAddressPortNumber 取出监听地址里的端口号。
//
// 校验、ListenPort 与「画像首次生成时的端口初值」都从这里取，端口解析只有这一处。
func httpAddressPortNumber(address string) (int, error) {
	port, err := httpAddressPort(address)
	if err != nil {
		return 0, err
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		return 0, fmt.Errorf("httpAddress port must be a number: %w", err)
	}
	if err := ValidatePort(number); err != nil {
		return 0, fmt.Errorf("httpAddress: %w", err)
	}
	return number, nil
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
