package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"eucli-deep-wiki/internal/apiclient"
	"eucli-deep-wiki/internal/vault"
)

// runFind 按文件名检索，不读文件内容，因此比内容检索快得多。
//
// 默认要求文件名与模式一致；加 --substring 改成「包含即可」；
// 模式里出现通配符时一律按 glob 匹配。
func runFind(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("find", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw find [参数] <仓库> <文件名或通配符>（默认精确匹配，--substring 改为包含，含 * ? [ 时按通配）")
	options := &clientFlags{}
	options.register(flags, true)
	searchPath := flags.String("path", "", "只在这个目录内查找")
	substring := flags.Bool("substring", false, "按包含匹配，而不是要求文件名完全一致")
	limit := flags.Int("limit", vault.DefaultFindLimit, fmt.Sprintf("最多返回多少条结果，上限 %d", vault.MaxFindLimit))
	offset := flags.Int("offset", 0, "跳过前多少条结果，用于翻页")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}
	if flags.NArg() < 2 {
		fmt.Fprintln(stderr, "用法: eucli-dw find [参数] <仓库> <文件名或通配符>")
		return 2
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	ctx := context.Background()
	repo := flags.Arg(0)
	pattern := flags.Arg(1)

	result, err := client.Find(ctx, repo, apiclient.FindOptions{
		Pattern:   pattern,
		Path:      *searchPath,
		Substring: *substring,
		Limit:     *limit,
		Offset:    *offset,
	})
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, result)
	}

	if len(result.Entries) == 0 {
		fmt.Fprintf(stderr, "没有匹配的文件（已检查 %d 个文件）\n", result.Scanned)
		suggestSubstring(ctx, client, repo, pattern, *searchPath, *substring, *offset, stderr)
		return 0
	}

	for _, entry := range result.Entries {
		fmt.Fprintf(stdout, "%s\t%d\n", entry.Path, entry.Size)
	}
	if result.Truncated {
		limitNote := fmt.Sprintf("本次最多返回 %d 条，单次最多返回 %d 条", vault.NormalizeFindLimit(*limit), vault.MaxFindLimit)
		if *limit > vault.MaxFindLimit {
			limitNote = fmt.Sprintf("单次最多返回 %d 条，--limit 已按上限处理", vault.MaxFindLimit)
		}
		fmt.Fprintf(stderr, "（还有更多结果，可用 --offset %d 继续；已检查 %d 个文件；%s）\n",
			result.Offset+len(result.Entries), result.Scanned, limitNote)
	}
	return 0
}

// suggestSubstring 在精确匹配落空时补一句「其实有包含匹配」。
//
// 精确是默认口径，但「一个都没有」容易让人以为仓库里真没有这个文件。
// 只在默认口径、未翻页、模式不含通配符时补这一句：这些条件下「精确」才是用户
// 的本意，提示不会跑偏；通配符与翻页场景里再塞一句只会更吵。
//
// 这次附加查询失败就什么都不说：提示是附赠信息，不该把一次成功的空结果变成报错。
func suggestSubstring(
	ctx context.Context,
	client *apiclient.Client,
	repo string,
	pattern string,
	scope string,
	alreadySubstring bool,
	offset int,
	stderr io.Writer,
) {
	if alreadySubstring || offset != 0 || strings.ContainsAny(pattern, "*?[") {
		return
	}

	// 只要够提示用就行，取一小批避免为了提示又扫一遍全仓。
	const sampleLimit = 5
	hint, err := client.Find(ctx, repo, apiclient.FindOptions{
		Pattern:   pattern,
		Path:      scope,
		Substring: true,
		Limit:     sampleLimit,
	})
	if err != nil || len(hint.Entries) == 0 {
		return
	}

	amount := fmt.Sprintf("%d 个", len(hint.Entries))
	if hint.Truncated {
		amount = fmt.Sprintf("至少 %d 个", len(hint.Entries))
	}
	fmt.Fprintf(stderr, "另有 %s文件名包含 %q（如 %s），加 --substring 查看\n",
		amount, pattern, hint.Entries[0].Path)
}
