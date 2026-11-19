package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"eucli-dw/internal/apiclient"
	"eucli-dw/internal/settings"
)

// clientFlags 是全部命令共用的连接参数。
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
	flags.IntVar(&f.timeout, "timeout", 0, "这次调用的等待上限秒数；0 表示按命令自己的预算")
}

// client 按「命令行参数 → 配置文件」的顺序确定连接信息。
//
// 地址与 key 都只来自命令行自己的配置：服务端的配置、目录布局与钥匙列表
// 与这里无关。
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
