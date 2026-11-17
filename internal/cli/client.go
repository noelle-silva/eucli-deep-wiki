package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"

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
