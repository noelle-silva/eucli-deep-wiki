package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// runPull 下载快照包。
func runPull(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("pull", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := &clientFlags{}
	options.register(flags, false)
	output := flags.String("o", "", "输出文件路径")
	format := flags.String("format", "zip", "打包格式：zip 或 tar.gz")

	if err := parseArgs(flags, args); err != nil {
		return 2
	}
	if flags.NArg() == 0 {
		fmt.Fprintln(stderr, "用法: eucli-dw pull <仓库>[:路径] ... [-o 文件] [--format zip|tar.gz]")
		return 2
	}

	if *format != "zip" && *format != "tar.gz" {
		fmt.Fprintln(stderr, "format 只支持 zip 或 tar.gz")
		return 2
	}

	target := *output
	if target == "" {
		target = defaultArchiveName(flags.Args(), *format)
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	temporary := target + ".part"
	file, err := os.Create(temporary)
	if err != nil {
		fmt.Fprintf(stderr, "创建文件失败: %v\n", err)
		return 1
	}

	err = client.Archive(context.Background(), flags.Args(), *format, file)
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(temporary)
		fmt.Fprintf(stderr, "下载失败: %v\n", err)
		return 1
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		fmt.Fprintf(stderr, "写入失败: %v\n", closeErr)
		return 1
	}

	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		fmt.Fprintf(stderr, "保存失败: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "已保存 %s\n", target)
	return 0
}

// runBundle 下载带历史的 bundle。
func runBundle(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("bundle", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := &clientFlags{}
	options.register(flags, false)
	output := flags.String("o", "", "输出文件路径")

	if err := parseArgs(flags, args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "用法: eucli-dw bundle <仓库> [-o 文件]")
		return 2
	}

	repository := flags.Arg(0)
	target := *output
	if target == "" {
		target = repository + ".bundle"
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	temporary := target + ".part"
	file, err := os.Create(temporary)
	if err != nil {
		fmt.Fprintf(stderr, "创建文件失败: %v\n", err)
		return 1
	}

	err = client.Bundle(context.Background(), repository, file)
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(temporary)
		fmt.Fprintf(stderr, "下载失败: %v\n", err)
		return 1
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		fmt.Fprintf(stderr, "写入失败: %v\n", closeErr)
		return 1
	}

	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		fmt.Fprintf(stderr, "保存失败: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "已保存 %s\n", target)
	return 0
}

func defaultArchiveName(args []string, format string) string {
	base := "archive"
	if len(args) == 1 {
		base = strings.SplitN(args[0], ":", 2)[0]
		if base == "" {
			base = "archive"
		}
	}
	return base + "." + format
}
