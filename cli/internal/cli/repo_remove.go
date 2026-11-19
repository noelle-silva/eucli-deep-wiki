package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
)

// runRepoRemove 把仓库从服务端名单里摘除。
//
// 默认只摘名单、保留磁盘数据——「默认安全」：不加开关绝不会删数据。
// --drop-data 才连磁盘数据一起删，此时把契约里的 keepData 置为假。
func runRepoRemove(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("repo rm", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw repo rm [参数] <仓库>")
	options := &clientFlags{}
	options.register(flags, true)
	dropData := flags.Bool("drop-data", false, "连磁盘数据一起删；不加只摘名单、保留数据")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}
	if flags.NArg() != 1 || strings.TrimSpace(flags.Arg(0)) == "" {
		fmt.Fprintln(stderr, "用法: eucli-dw repo rm [参数] <仓库>")
		return exitUsage
	}
	name := strings.TrimSpace(flags.Arg(0))

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	result, err := client.RemoveRepository(context.Background(), name, !*dropData)
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, result)
	}

	if !result.Removed {
		fmt.Fprintf(stdout, "%s 不在名单里，未做改动\n", result.Name)
		return exitOK
	}

	// 三种情形分开说：本来就没有副本 / 副本留着 / 副本被删掉。
	// 只按「操作后还在不在」推断，会把「本来就没落盘」错说成「数据被删了」。
	switch {
	case !result.DataExisted:
		fmt.Fprintf(stdout, "已从名单摘除 %s，磁盘上没有它的副本\n", result.Name)
	case result.DataKept:
		fmt.Fprintf(stdout, "已从名单摘除 %s，磁盘数据保留\n", result.Name)
	default:
		fmt.Fprintf(stdout, "已从名单摘除 %s，磁盘数据已删除\n", result.Name)
	}
	return exitOK
}
