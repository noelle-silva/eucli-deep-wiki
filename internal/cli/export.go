package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// runPull 下载快照包。
func runPull(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("pull", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw "+flags.Name()+" [参数]")
	options := &clientFlags{}
	options.register(flags, true)
	output := flags.String("o", "", "输出文件路径")
	format := flags.String("format", "zip", "打包格式：zip 或 tar.gz")
	progress := flags.Bool("progress", false, "下载过程中在 stderr 输出进度")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
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
		return exitFailure
	}

	temporary := target + ".part"
	file, err := os.Create(temporary)
	if err != nil {
		fmt.Fprintf(stderr, "创建文件失败: %v\n", err)
		return exitFailure
	}

	var sink io.Writer = file
	var reporter *progressWriter
	if *progress {
		reporter = &progressWriter{target: file, out: stderr, last: time.Now()}
		sink = reporter
	}

	err = client.Archive(context.Background(), flags.Args(), *format, sink)
	if reporter != nil {
		reporter.done()
	}
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(temporary)
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
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

	return reportSaved(stdout, stderr, target, options.asJSON)
}

// runBundle 下载带历史的 bundle。
func runBundle(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("bundle", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw "+flags.Name()+" [参数]")
	options := &clientFlags{}
	options.register(flags, true)
	output := flags.String("o", "", "输出文件路径")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
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
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
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

// progressWriter 每两秒向 stderr 汇报已写入的字节数。
type progressWriter struct {
	target io.Writer
	out    io.Writer
	total  int64
	last   time.Time
}

func (p *progressWriter) Write(data []byte) (int, error) {
	written, err := p.target.Write(data)
	p.total += int64(written)
	if time.Since(p.last) >= 2*time.Second {
		p.last = time.Now()
		fmt.Fprintf(p.out, "\r已下载 %d MB", p.total>>20)
	}
	return written, err
}

func (p *progressWriter) done() {
	fmt.Fprintf(p.out, "\r已下载 %d MB\n", p.total>>20)
}

// reportSaved 回报保存结果，附文件大小，便于确认不是空包。
func reportSaved(stdout io.Writer, stderr io.Writer, path string, asJSON bool) int {
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(stderr, "读取结果文件失败: %v\n", err)
		return 1
	}

	if asJSON {
		return writeJSON(stdout, stderr, map[string]any{"saved": path, "bytes": info.Size()})
	}
	fmt.Fprintf(stdout, "已保存 %s（%d 字节）\n", path, info.Size())
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
