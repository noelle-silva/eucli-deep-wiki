package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"eucli-deep-wiki/internal/config"
)

// runCache 管理 bundle 缓存目录。
func runCache(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 || wantsHelp(args[:1]) {
		printCacheUsage(stdout)
		if len(args) == 0 {
			return 2
		}
		return 0
	}

	switch args[0] {
	case "list":
		return runCacheList(args[1:], stdout, stderr)
	case "clean":
		return runCacheClean(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "未知 cache 子命令: %s\n", args[0])
		printCacheUsage(stderr)
		return 2
	}
}

func printCacheUsage(out io.Writer) {
	fmt.Fprint(out, `用法: eucli-dw cache <子命令> [参数]

子命令:
  list [--json]      列出缓存文件与总占用
  clean [--yes]      清空缓存（不加 --yes 时只预演）
`)
}

func runCacheList(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("cache list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw cache list [参数]")
	serviceConfig := flags.String("service-config", defaultServiceConfig(), "服务配置文件路径")
	asJSON := flags.Bool("json", false, "输出原始 JSON")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	dir, code := cacheDir(*serviceConfig, stderr)
	if code != 0 {
		return code
	}

	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "读取缓存目录失败: %v\n", err)
		return 1
	}

	type item struct {
		Name  string `json:"name"`
		Bytes int64  `json:"bytes"`
	}

	items := make([]item, 0, len(entries))
	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		items = append(items, item{Name: entry.Name(), Bytes: info.Size()})
		total += info.Size()
	}

	if *asJSON {
		return writeJSON(stdout, stderr, map[string]any{
			"path":  dir,
			"files": items,
			"bytes": total,
		})
	}

	if len(items) == 0 {
		fmt.Fprintf(stdout, "缓存为空（%s）\n", dir)
		return 0
	}
	for _, entry := range items {
		fmt.Fprintf(stdout, "%s\t%d MB\n", entry.Name, entry.Bytes>>20)
	}
	fmt.Fprintf(stdout, "合计 %d MB，位于 %s\n", total>>20, dir)
	return 0
}

func runCacheClean(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("cache clean", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw cache clean [--yes]")
	serviceConfig := flags.String("service-config", defaultServiceConfig(), "服务配置文件路径")
	confirm := flags.Bool("yes", false, "确认清空")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}

	dir, code := cacheDir(*serviceConfig, stderr)
	if code != 0 {
		return code
	}

	if !*confirm {
		size, _ := directorySize(dir)
		fmt.Fprintf(stdout, "将清空 %s（当前 %d MB），确认请加 --yes\n", dir, size>>20)
		return 0
	}

	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintf(stderr, "清空缓存失败: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "已清空 %s\n", dir)
	return 0
}

func cacheDir(serviceConfig string, stderr io.Writer) (string, int) {
	cfg, err := config.Load(serviceConfig)
	if err != nil {
		fmt.Fprintf(stderr, "读取服务配置失败: %v\n", err)
		return "", 1
	}
	return filepath.Join(cfg.DataRoot, "cache"), 0
}
