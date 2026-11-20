package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"eucli-dw/internal/apiclient"
	"eucli-dw/internal/params"
)

// runGet 读取一个或多个文件，可按行切段、按字节截断。
//
// 单文件时 stdout 只有正文，可以直接管道；多文件时每段前面加一行
// `==> 路径 <==`，与 head 的多文件输出同形。所有提示都走 stderr。
func runGet(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("get", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw get [参数] <仓库> <路径>...")
	options := &clientFlags{}
	options.register(flags, true)
	lines := flags.String("lines", "", "只取这一段行，如 362-520、362-、-520 或 362")
	maxBytes := flags.Int64("max-bytes", 0, "最多输出多少字节，超出时退到最后一个完整行")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}
	if flags.NArg() < 2 {
		fmt.Fprintln(stderr, "用法: eucli-dw get [参数] <仓库> <路径>...")
		return exitUsage
	}

	span, err := params.ParseLineRange(*lines)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	if *maxBytes < 0 {
		fmt.Fprintln(stderr, "--max-bytes 必须是非负整数")
		return exitUsage
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	repo := flags.Arg(0)
	targets := flags.Args()[1:]
	request := apiclient.FileOptions{Lines: span.String(), MaxBytes: *maxBytes}

	batch := fetchSegments(targets, func(target string) (apiclient.File, error) {
		fileOptions := request
		fileOptions.Path = target
		return client.File(context.Background(), repo, fileOptions)
	})

	if options.asJSON {
		return writeSegmentsJSON(stdout, stderr, "files", batch)
	}

	requested := !span.IsZero() || *maxBytes > 0
	return renderSegments(stdout, stderr, batch, func(out io.Writer, name string, file apiclient.File, withLabel bool) {
		writeFileContent(out, file)
		writeRangeHint(stderr, name, file, withLabel, requested)
	})
}

// writeFileContent 输出正文；末尾缺换行时补一个，避免下一段与它粘在一起。
func writeFileContent(stdout io.Writer, file apiclient.File) {
	if _, err := io.WriteString(stdout, file.Content); err != nil {
		return
	}
	if file.Content == "" || strings.HasSuffix(file.Content, "\n") {
		return
	}
	_, _ = io.WriteString(stdout, "\n")
}

// writeRangeHint 说明这次拿到了哪一段、后面还有没有。
//
// 只有调用方确实要求了切段或限长才提示：没提要求时「给了整份文件」不需要解释。
// 这些信息写在 stderr，stdout 只放正文，管道下游不需要理解「截断」是什么。
func writeRangeHint(stderr io.Writer, label string, file apiclient.File, withLabel bool, requested bool) {
	if !requested {
		return
	}

	prefix := ""
	if withLabel {
		prefix = label + ": "
	}

	if file.FirstLine > file.TotalLines {
		fmt.Fprintf(stderr, "%s文件只有 %d 行，请求的起始行超出末尾\n", prefix, file.TotalLines)
		return
	}
	if file.LastLine >= file.TotalLines && file.Truncated == "" {
		return
	}

	if file.FirstLine > file.LastLine {
		fmt.Fprintf(stderr, "%s第 %d 行就超过 --max-bytes，未输出任何内容\n", prefix, file.FirstLine)
		return
	}

	// 行区间与字节截断都会留下后续内容，区别只在措辞。
	how := fmt.Sprintf("已取第 %d-%d 行", file.FirstLine, file.LastLine)
	if file.Truncated == apiclient.TruncatedByBytes {
		how = fmt.Sprintf("已按 --max-bytes 截断到第 %d 行（从第 %d 行起）", file.LastLine, file.FirstLine)
	}
	fmt.Fprintf(stderr, "%s%s，共 %d 行；继续用 --lines %d-\n", prefix, how, file.TotalLines, file.LastLine+1)
}
