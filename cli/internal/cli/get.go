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
		return 2
	}

	span, err := params.ParseLineRange(*lines)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *maxBytes < 0 {
		fmt.Fprintln(stderr, "--max-bytes 必须是非负整数")
		return 2
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	repo := flags.Arg(0)
	targets := flags.Args()[1:]
	request := apiclient.FileOptions{Lines: span.String(), MaxBytes: *maxBytes}

	outcomes := gather(len(targets), 0, func(index int) (apiclient.File, error) {
		fileOptions := request
		fileOptions.Path = targets[index]
		return client.File(context.Background(), repo, fileOptions)
	})

	if options.asJSON {
		return writeFilesJSON(stdout, stderr, targets, outcomes)
	}

	multiple := len(targets) > 1
	requested := !span.IsZero() || *maxBytes > 0
	code := 0
	truncated := false

	for index, result := range outcomes {
		if result.err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", targets[index], result.err)
			if !truncated {
				truncated = true
				code = exitCodeFor(result.err)
			}
			continue
		}

		if multiple {
			fmt.Fprintf(stdout, "==> %s <==\n", result.value.Path)
		}
		writeFileContent(stdout, result.value)
		writeRangeHint(stderr, targets[index], result.value, multiple, requested)
	}

	if truncated {
		return code
	}
	return 0
}

// writeFilesJSON 输出结构化结果：单文件保持一段对象，多文件包成 files 数组。
//
// 一个都没取到时走统一的错误输出，与其它命令同形；部分成功时把失败原因
// 放进 payload 的 failures，失败的事实不会因为「另外几个读到了」而消失。
func writeFilesJSON(stdout io.Writer, stderr io.Writer, targets []string, outcomes []outcome[apiclient.File]) int {
	files := make([]apiclient.File, 0, len(outcomes))
	failures := make([]string, 0, len(outcomes))
	var firstErr error

	for index, result := range outcomes {
		if result.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", targets[index], result.err))
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		files = append(files, result.value)
	}

	if firstErr != nil && len(files) == 0 {
		reportError(stderr, firstErr, true)
		return exitCodeFor(firstErr)
	}

	var payload any = files
	switch {
	case len(targets) == 1:
		payload = files[0]
	case len(failures) > 0:
		payload = map[string]any{"files": files, "failures": failures}
	}
	if writeCode := writeJSON(stdout, stderr, payload); writeCode != 0 {
		return writeCode
	}
	if firstErr != nil {
		return exitCodeFor(firstErr)
	}
	return 0
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
