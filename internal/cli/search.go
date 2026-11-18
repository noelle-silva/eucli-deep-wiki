package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"eucli-deep-wiki/internal/apiclient"
	"eucli-deep-wiki/internal/vault"
)

// runSearch 在仓库内按内容检索，可限定目录、带上下文、翻页。
func runSearch(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("search", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw search [参数] <仓库> <关键词>")
	options := &clientFlags{}
	options.register(flags, true)
	limit := flags.Int("limit", 0, "最多返回多少条命中（单次上限 100，可配合 --offset 翻页）")
	offset := flags.Int("offset", 0, "跳过前多少条命中，用于翻页")
	searchPath := flags.String("path", "", "只在这个目录内检索")
	contextLines := flags.Int("context", 0,
		fmt.Sprintf("每处命中带出前后各多少行上下文，最多 %d", vault.MaxSearchContext))

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}
	if flags.NArg() < 2 {
		fmt.Fprintln(stderr, "用法: eucli-dw search [参数] <仓库> <关键词>")
		return 2
	}
	if *contextLines < 0 {
		fmt.Fprintln(stderr, "--context 不能为负")
		return 2
	}

	client, err := options.client()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	repo := flags.Arg(0)
	query := strings.Join(flags.Args()[1:], " ")

	result, err := client.Search(context.Background(), repo, apiclient.SearchOptions{
		Query:   query,
		Path:    *searchPath,
		Limit:   *limit,
		Offset:  *offset,
		Context: *contextLines,
	})
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	if options.asJSON {
		return writeJSON(stdout, stderr, result)
	}

	if len(result.Matches) == 0 {
		fmt.Fprintf(stderr, "没有命中（已扫描 %d 个文件，偏移 %d）\n", result.Scanned, result.Offset)
		writeOversized(stderr, result)
		return 0
	}

	writeMatches(stdout, result.Matches, *contextLines > 0)
	if *contextLines > vault.MaxSearchContext {
		fmt.Fprintf(stderr, "（--context 已按上限 %d 处理）\n", vault.MaxSearchContext)
	}
	if result.Truncated {
		fmt.Fprintf(stderr, "（还有更多命中，可用 --offset %d 继续；已扫描 %d 个文件）\n",
			result.Offset+len(result.Matches), result.Scanned)
	}
	writeOversized(stderr, result)
	return 0
}

// matchLine 是渲染用的行视图：命中行与上下文行统一成同一份结构，
// 同一行既是命中又是上下文时按命中处理，因为「这里是命中」是更强的信息。
type matchLine struct {
	path    string
	line    int
	text    string
	matched bool
}

// writeMatches 按 grep 的阅读习惯输出：命中行用冒号，上下文行用短横线，
// 行号不连续处用 `--` 断开。相邻命中的上下文重叠不会把命中行吞掉。
func writeMatches(stdout io.Writer, matches []apiclient.Match, withContext bool) {
	lines := collectMatchLines(matches)

	if !withContext {
		for _, line := range lines {
			if !line.matched {
				continue
			}
			fmt.Fprintf(stdout, "%s:%d: %s\n", line.path, line.line, line.text)
		}
		return
	}

	lastPath := ""
	lastLine := 0
	for _, line := range lines {
		if line.path != lastPath || (lastLine != 0 && line.line != lastLine+1) {
			if lastPath != "" {
				fmt.Fprintln(stdout, "--")
			}
			lastPath = line.path
		}

		if line.matched {
			fmt.Fprintf(stdout, "%s:%d: %s\n", line.path, line.line, line.text)
		} else {
			fmt.Fprintf(stdout, "%s-%d- %s\n", line.path, line.line, line.text)
		}
		lastLine = line.line
	}
}

// collectMatchLines 把命中与上下文展开成「每行一条」并按路径、行号排序。
func collectMatchLines(matches []apiclient.Match) []matchLine {
	collected := make(map[string]map[int]*matchLine, len(matches))

	for _, match := range matches {
		byLine, ok := collected[match.Path]
		if !ok {
			byLine = make(map[int]*matchLine, len(match.Before)+len(match.After)+1)
			collected[match.Path] = byLine
		}

		add := func(line int, text string, matched bool) {
			existing, found := byLine[line]
			if !found {
				byLine[line] = &matchLine{path: match.Path, line: line, text: text, matched: matched}
				return
			}
			if matched {
				*existing = matchLine{path: match.Path, line: line, text: text, matched: true}
			}
		}

		for _, line := range match.Before {
			add(line.Line, line.Text, false)
		}
		add(match.Line, match.Text, true)
		for _, line := range match.After {
			add(line.Line, line.Text, false)
		}
	}

	lines := make([]matchLine, 0, len(collected))
	for _, byLine := range collected {
		for _, line := range byLine {
			lines = append(lines, *line)
		}
	}
	sort.Slice(lines, func(left int, right int) bool {
		if lines[left].path != lines[right].path {
			return lines[left].path < lines[right].path
		}
		return lines[left].line < lines[right].line
	})
	return lines
}

// writeOversized 把「哪些文件根本没参与检索」说清楚。
//
// 不说的话，调用方会把「大文件没搜」误当成「大文件里没命中」。
func writeOversized(stderr io.Writer, result apiclient.SearchResult) {
	if result.OversizedCount == 0 {
		return
	}

	fmt.Fprintf(stderr, "（跳过 %d 个超过单文件上限的文件，它们没有参与检索）\n", result.OversizedCount)
	for _, file := range result.Oversized {
		fmt.Fprintf(stderr, "  %s\t%s\n", file.Path, formatBytes(file.Size))
	}
	if remaining := result.OversizedCount - len(result.Oversized); remaining > 0 {
		fmt.Fprintf(stderr, "  另有 %d 个未列出\n", remaining)
	}
}
