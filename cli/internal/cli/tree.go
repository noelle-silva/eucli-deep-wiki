package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"eucli-dw/internal/apiclient"
	"eucli-dw/internal/params"
)

// runTree 列出目录；--depth 一次展开多层，--dirs-only 只看骨架。
//
// 位置参数是「仓库 + 任意多个目录」：一次给几个目录就并发取几个，按命令行给的
// 顺序输出，段与段之间用 `==> 目录 <==` 分隔（与 get 的多路径同形）。不给目录
// 等同于只看仓库根。
func runTree(args []string, stdout io.Writer, stderr io.Writer) int {
	const usage = "eucli-dw tree [参数] <仓库> [目录...]"

	flags := flag.NewFlagSet("tree", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, usage)
	options := &clientFlags{}
	options.register(flags, true)
	depth := flags.Int("depth", params.DefaultTreeDepth,
		fmt.Sprintf("展开多少层，1 表示只列直接子项，最多 %d", params.MaxTreeDepth))
	dirsOnly := flags.Bool("dirs-only", false, "只列目录，用来看仓库骨架")
	excluded := &excludeFlags{}
	excluded.register(flags)

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}
	if flags.NArg() < 1 {
		fmt.Fprintf(stderr, "用法: %s\n", usage)
		return exitUsage
	}
	if *depth < 1 {
		fmt.Fprintln(stderr, "--depth 至少为 1")
		return exitUsage
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	repo := flags.Arg(0)

	// 不给目录就是只看仓库根：空路径代表根，仍然走同一套多段编排。
	targets := flags.Args()[1:]
	if len(targets) == 0 {
		targets = []string{""}
	}

	request := apiclient.TreeOptions{Depth: *depth, DirsOnly: *dirsOnly}
	batch := fetchSegments(targets, func(target string) (apiclient.Tree, error) {
		treeOptions := request
		treeOptions.Path = target
		return listTree(context.Background(), client, repo, treeOptions, excluded)
	})

	if options.asJSON {
		return writeSegmentsJSON(stdout, stderr, "trees", batch)
	}

	code := renderSegments(stdout, stderr, batch, func(out io.Writer, name string, tree apiclient.Tree, withLabel bool) {
		writeEntries(out, tree.Entries)
		noteTreeTruncated(stderr, name, tree, withLabel)
	})

	noteClamped(stderr, "depth", *depth, params.MaxTreeDepth)
	if batch.anyValue() {
		excluded.announce(stderr)
	}
	return code
}

// noteTreeTruncated 说明这一段被服务端压到了什么程度：结果被截断时给出继续看的
// 办法。多段输出时带上这一段的标签，否则不知道说的是哪一段。
func noteTreeTruncated(stderr io.Writer, name string, tree apiclient.Tree, withLabel bool) {
	if !tree.Truncated {
		return
	}

	prefix := ""
	if withLabel {
		prefix = name + ": "
	}
	fmt.Fprintf(stderr, "%s（结果已截断：共 %d 项；可加 --dirs-only 或从子目录继续）\n", prefix, len(tree.Entries))
}
