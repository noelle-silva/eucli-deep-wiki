package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"eucli-deep-wiki/internal/apiclient"
	"eucli-deep-wiki/internal/settings"
)

// clientFlags 是查询类命令共用的连接参数。
type clientFlags struct {
	configPath string
	server     string
	apiKey     string
	asJSON     bool
}

func (f *clientFlags) register(flags *flag.FlagSet, withJSON bool) {
	flags.StringVar(&f.configPath, "config", "", "客户端配置文件路径")
	flags.StringVar(&f.server, "server", "", "临时覆盖服务地址")
	flags.StringVar(&f.apiKey, "key", "", "临时覆盖 key")
	if withJSON {
		flags.BoolVar(&f.asJSON, "json", false, "输出原始 JSON")
	}
}

// defaultServiceConfig 优先使用可执行文件同级的部署配置，
// 这样在部署目录里直接运行不必带参数；开发时再回退到仓库内路径。
func defaultServiceConfig() string {
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "config", "config.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "configs/config.json"
}

// reportError 输出错误；带 --json 时给出结构化错误，便于程序判断。
func reportError(stderr io.Writer, err error, asJSON bool) {
	if !asJSON {
		fmt.Fprintf(stderr, "读取失败: %v\n", err)
		return
	}

	payload := map[string]any{"ok": false, "message": err.Error()}
	var apiErr *apiclient.APIError
	if errors.As(err, &apiErr) {
		payload["status"] = apiErr.StatusCode
		payload["code"] = apiErr.Code
		payload["message"] = apiErr.Message
	}

	encoder := json.NewEncoder(stderr)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(payload)
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
	return apiclient.New(server, apiKey)
}
