package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"eucli-deep-wiki/internal/settings"
)

// runConfig 处理 config 子命令组。
func runConfig(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "用法: eucli-dw config <set|show|path> [参数]")
		return 2
	}

	switch args[0] {
	case "set":
		return runConfigSet(args[1:], stdout, stderr)
	case "show":
		return runConfigShow(args[1:], stdout, stderr)
	case "path":
		return runConfigPath(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "未知 config 子命令: %s\n", args[0])
		return 2
	}
}

func runConfigSet(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("config set", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "客户端配置文件路径")

	if err := parseArgs(flags, args); err != nil {
		return 2
	}
	if flags.NArg() < 2 {
		fmt.Fprintln(stderr, "用法: eucli-dw config set <server|key> <值>")
		return 2
	}

	field := flags.Arg(0)
	value := flags.Arg(1)

	path, err := settings.ResolvePath(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "解析配置路径失败: %v\n", err)
		return 1
	}

	loaded, err := settings.Load(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "读取配置失败: %v\n", err)
		return 1
	}
	if loaded == nil {
		loaded = &settings.Settings{}
	}

	switch field {
	case "server":
		if err := settings.ValidateServer(value); err != nil {
			fmt.Fprintf(stderr, "地址无效: %v\n", err)
			return 1
		}
		loaded.Server = value
	case "key":
		if err := settings.ValidateAPIKey(value); err != nil {
			fmt.Fprintf(stderr, "key 无效: %v\n", err)
			return 1
		}
		loaded.APIKey = value
	default:
		fmt.Fprintf(stderr, "未知字段: %s，只支持 server 或 key\n", field)
		return 2
	}

	if err := loaded.Save(path); err != nil {
		fmt.Fprintf(stderr, "写入配置失败: %v\n", err)
		return 1
	}

	if field == "key" {
		fmt.Fprintf(stdout, "已写入 %s（key 以明文保存在本机，仅本人可读）\n", path)
		return 0
	}
	fmt.Fprintf(stdout, "已写入 %s\n", path)
	return 0
}

func runConfigShow(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("config show", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "客户端配置文件路径")
	asJSON := flags.Bool("json", false, "输出原始 JSON")

	if err := parseArgs(flags, args); err != nil {
		return 2
	}

	path, err := settings.ResolvePath(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "解析配置路径失败: %v\n", err)
		return 1
	}

	loaded, err := settings.Load(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "读取配置失败: %v\n", err)
		return 1
	}
	if loaded == nil {
		loaded = &settings.Settings{}
	}

	if *asJSON {
		return writeJSON(stdout, stderr, map[string]string{
			"server": loaded.Server,
			"apiKey": maskKey(loaded.APIKey),
			"path":   path,
		})
	}

	server := loaded.Server
	if server == "" {
		server = "（未设置）"
	}
	fmt.Fprintf(stdout, "配置文件: %s\n", path)
	fmt.Fprintf(stdout, "服务地址: %s\n", server)
	fmt.Fprintf(stdout, "key:      %s\n", maskKey(loaded.APIKey))
	return 0
}

func runConfigPath(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("config path", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "客户端配置文件路径")

	if err := parseArgs(flags, args); err != nil {
		return 2
	}

	path, err := settings.ResolvePath(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "解析配置路径失败: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, path)
	return 0
}

// maskKey 只露出首尾各四位，避免整串 key 出现在屏幕与日志里。
func maskKey(key string) string {
	if key == "" {
		return "（未设置）"
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}
