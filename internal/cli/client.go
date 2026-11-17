package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"eucli-deep-wiki/internal/apiclient"
	"eucli-deep-wiki/internal/settings"
)

// clientFlags 是查询类命令共用的连接参数。
type clientFlags struct {
	configPath string
	server     string
	apiKey     string
	asJSON     bool
	timeout    int
}

func (f *clientFlags) register(flags *flag.FlagSet, withJSON bool) {
	flags.StringVar(&f.configPath, "config", "", "客户端配置文件路径")
	flags.StringVar(&f.server, "server", "", "临时覆盖服务地址")
	flags.StringVar(&f.apiKey, "key", "", "临时覆盖 key")
	if withJSON {
		flags.BoolVar(&f.asJSON, "json", false, "输出原始 JSON")
	}
	flags.IntVar(&f.timeout, "timeout", 0, "请求超时秒数，0 表示使用默认值")
}

// defaultServiceConfig 依次尝试：环境变量、可执行文件同级的部署配置、
// 当前目录的 config/ 与 configs/。安装到 PATH 后从任意目录都能找到部署配置。
func defaultServiceConfig() string {
	if configured := strings.TrimSpace(os.Getenv("EUCLI_DW_SERVICE_CONFIG")); configured != "" {
		return configured
	}

	candidates := make([]string, 0, 3)
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "config", "config.json"))
	}
	candidates = append(candidates,
		filepath.Join("config", "config.json"),
		filepath.Join("configs", "config.json"),
	)

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return candidates[len(candidates)-1]
}

// reportError 输出错误；带 --json 时给出结构化错误，便于程序判断。
func reportError(stderr io.Writer, err error, asJSON bool) {
	if !asJSON {
		fmt.Fprintf(stderr, "失败: %v\n", err)
		return
	}

	payload := map[string]any{"ok": false, "message": err.Error()}
	var apiErr *apiclient.APIError
	if errors.As(err, &apiErr) {
		payload["status"] = apiErr.StatusCode
		payload["code"] = apiErr.Code
		payload["message"] = apiErr.Message
	} else if isNetworkError(err) {
		payload["code"] = "network_error"
	}

	encoder := json.NewEncoder(stderr)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(payload)
}

// isNetworkError 判断是否为连接或超时类错误。
func isNetworkError(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// client 按「命令行参数 → 配置文件」的顺序确定连接信息。
func (f *clientFlags) client() (*apiclient.Client, error) {
	path, err := settings.ResolvePath(f.configPath)
	if err != nil {
		return nil, err
	}

	stored, err := settings.Load(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if stored == nil {
		stored = &settings.Settings{}
	}

	server := f.server
	if server == "" {
		server = stored.Server
	}
	apiKey := f.apiKey
	if apiKey == "" {
		apiKey = stored.APIKey
	}

	if server == "" || apiKey == "" {
		return nil, fmt.Errorf("尚未配置连接信息\n请先执行:\n  eucli-dw config set server <url>\n  eucli-dw config set key <key>\n配置文件: %s", path)
	}

	client, buildErr := apiclient.New(server, apiKey)
	if buildErr != nil {
		return nil, buildErr
	}
	if f.timeout > 0 {
		client = client.WithTimeout(time.Duration(f.timeout) * time.Second)
	}
	return client, nil
}
