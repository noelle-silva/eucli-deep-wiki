package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"eucli-deep-wiki/internal/apiclient"
	"eucli-deep-wiki/internal/vault"
)

// runTree 列出目录；--depth 一次展开多层，--dirs-only 只看骨架。
func runTree(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("tree", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw tree [参数] <仓库> [目录]")
	options := &clientFlags{}
	options.register(flags, true)
	depth := flags.Int("depth", vault.DefaultTreeDepth,
		fmt.Sprintf("展开多少层，1 表示只列直接子项，最多 %d", vault.MaxTreeDepth))
	dirsOnly := flags.Bool("dirs-only", false, "只列目录，用来看仓库骨架")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}
	if flags.NArg() < 1 {
		fmt.Fprintln(stderr, "用法: eucli-dw tree [参数] <仓库> [目录]")
		return 2
	}
	if *depth < 1 {
		fmt.Fprintln(stderr, "--depth 至少为 1")
		return 2
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	repo := flags.Arg(0)
	target := ""
	if flags.NArg() > 1 {
		target = flags.Arg(1)
	}

	tree, err := client.Tree(context.Background(), repo, apiclient.TreeOptions{
		Path:     target,
		Depth:    *depth,
		DirsOnly: *dirsOnly,
	})
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, tree)
	}

	for _, entry := range tree.Entries {
		if entry.Type == "dir" {
			fmt.Fprintf(stdout, "%s/\n", entry.Path)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%d\n", entry.Path, entry.Size)
	}
	if *depth > vault.MaxTreeDepth {
		fmt.Fprintf(stderr, "（--depth 已按上限 %d 处理）\n", vault.MaxTreeDepth)
	}
	if tree.Truncated {
		fmt.Fprintf(stderr, "（结果已截断：共 %d 项；可加 --dirs-only 或从子目录继续）\n", len(tree.Entries))
	}
	return 0
}
