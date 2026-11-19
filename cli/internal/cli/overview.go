package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"eucli-dw/internal/apiclient"
)

// manifestNames 是概览里尝试展示的清单文件，按优先级排列。
var manifestNames = []string{
	"go.mod",
	"package.json",
	"Cargo.toml",
	"pyproject.toml",
	"requirements.txt",
	"pom.xml",
	"build.gradle",
	"Gemfile",
	"composer.json",
	"CMakeLists.txt",
	"Makefile",
}

// overviewSection 是概览里要取的一段内容。
type overviewSection struct {
	path  string
	lines int
}

// runOverview 一次给出仓库概览：元数据 + 目录骨架 + README 开头 + 清单文件。
//
// 它是「先看仓库长什么样」的入口：两次串行请求拿到目录，之后的内容请求并发发出，
// 把原先需要 tree、get README、get 清单的多次往返压成一条命令。
func runOverview(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("overview", flag.ContinueOnError)
	flags.SetOutput(stderr)
	setupHelp(flags, "eucli-dw overview [参数] <仓库>")
	options := &clientFlags{}
	options.register(flags, true)
	depth := flags.Int("depth", 2, "目录展开多少层")
	readmeLines := flags.Int("readme-lines", 40, "README 展示多少行，0 表示不展示")
	manifestCount := flags.Int("manifests", 3, "最多展示几个清单文件，0 表示不展示")
	manifestLines := flags.Int("manifest-lines", 40, "每个清单文件展示多少行")

	if err := parseArgs(flags, args); err != nil {
		return parseExitCode(err)
	}
	if flags.NArg() < 1 {
		fmt.Fprintln(stderr, "用法: eucli-dw overview [参数] <仓库>")
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

	ctx := context.Background()
	repo := flags.Arg(0)

	meta, err := client.Meta(ctx, repo)
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}
	tree, err := client.Tree(ctx, repo, apiclient.TreeOptions{Depth: *depth})
	if err != nil {
		reportError(stderr, err, options.asJSON)
		return exitCodeFor(err)
	}

	sections := make([]overviewSection, 0, 1+*manifestCount)
	if readme := findReadme(tree.Entries); readme != "" && *readmeLines > 0 {
		sections = append(sections, overviewSection{path: readme, lines: *readmeLines})
	}
	if *manifestLines > 0 {
		for _, manifest := range pickManifests(tree.Entries, *manifestCount) {
			sections = append(sections, overviewSection{path: manifest, lines: *manifestLines})
		}
	}

	files := fetchSections(ctx, client, repo, sections)
	if options.asJSON {
		return writeOverviewJSON(stdout, stderr, meta, tree, sections, files)
	}

	renderOverview(stdout, stderr, meta, tree, sections, files)
	return 0
}

// fetchSections 并发取出各段内容；结果按 sections 顺序落位。
func fetchSections(ctx context.Context, client *apiclient.Client, repo string, sections []overviewSection) []outcome[apiclient.File] {
	return gather(len(sections), 0, func(index int) (apiclient.File, error) {
		section := sections[index]
		return client.File(ctx, repo, apiclient.FileOptions{
			Path:  section.path,
			Lines: fmt.Sprintf("1-%d", section.lines),
		})
	})
}

// renderOverview 输出给人读的概览；分段标题用 `==` 包裹，便于一眼定位。
func renderOverview(
	stdout io.Writer,
	stderr io.Writer,
	meta apiclient.RepoMeta,
	tree apiclient.Tree,
	sections []overviewSection,
	files []outcome[apiclient.File],
) {
	fmt.Fprintf(stdout, "%s\n", meta.Name)

	history := "完整历史"
	if meta.Shallow {
		history = "浅克隆"
	}
	fmt.Fprintf(stdout, "分支 %s  提交 %s  %s  体积 %s  同步 %s\n",
		branchOf(meta), revisionOf(meta), history, sizeOf(meta.GitBytes), formatTime(meta.LastSyncAt))

	if markers := blockingMarkers(meta); markers != "" {
		fmt.Fprintf(stdout, "状态 %s\n", markers)
	}

	fmt.Fprintf(stdout, "\n== 目录（%d 层，%d 项） ==\n", tree.Depth, len(tree.Entries))
	for _, entry := range tree.Entries {
		if entry.Type == "dir" {
			fmt.Fprintf(stdout, "%s/\n", entry.Path)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%d\n", entry.Path, entry.Size)
	}
	if tree.Truncated {
		fmt.Fprintln(stderr, "（目录已截断，可加 --dirs-only 或减小 --depth）")
	}

	for index, section := range sections {
		fmt.Fprintf(stdout, "\n== %s", section.path)
		if files[index].err != nil {
			fmt.Fprintf(stdout, "（读取失败：%v） ==\n", files[index].err)
			continue
		}

		file := files[index].value
		if file.LastLine < file.TotalLines {
			fmt.Fprintf(stdout, "（前 %d 行，共 %d 行） ==\n", file.LastLine, file.TotalLines)
		} else {
			fmt.Fprintf(stdout, "（共 %d 行） ==\n", file.TotalLines)
		}
		writeFileContent(stdout, file)
	}
}

// writeOverviewJSON 输出结构化概览，便于 Agent 直接消费。
func writeOverviewJSON(
	stdout io.Writer,
	stderr io.Writer,
	meta apiclient.RepoMeta,
	tree apiclient.Tree,
	sections []overviewSection,
	files []outcome[apiclient.File],
) int {
	documents := make([]apiclient.File, 0, len(files))
	failures := make([]string, 0, len(files))
	for index, result := range files {
		if result.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", sections[index].path, result.err))
			continue
		}
		documents = append(documents, result.value)
	}

	payload := map[string]any{
		"repo":      meta,
		"tree":      tree,
		"documents": documents,
	}
	if len(failures) > 0 {
		payload["failures"] = failures
	}
	return writeJSON(stdout, stderr, payload)
}

// findReadme 在根层挑出 README：优先 markdown，其次任意 readme。
func findReadme(entries []apiclient.Entry) string {
	candidates := make([]apiclient.Entry, 0, 2)
	for _, entry := range entries {
		if entry.Depth != 1 || entry.Type != "file" {
			continue
		}
		if strings.Contains(strings.ToLower(entry.Name), "readme") {
			candidates = append(candidates, entry)
		}
	}
	if len(candidates) == 0 {
		return ""
	}

	sort.SliceStable(candidates, func(left int, right int) bool {
		leftRank, rightRank := readmeRank(candidates[left].Name), readmeRank(candidates[right].Name)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return candidates[left].Name < candidates[right].Name
	})
	return candidates[0].Path
}

// readmeRank 越小越优先：README.md 排最前，其余 markdown 次之，再是其它格式。
func readmeRank(name string) int {
	lowered := strings.ToLower(name)
	switch {
	case lowered == "readme.md":
		return 0
	case strings.HasSuffix(lowered, ".md"):
		return 1
	case strings.HasSuffix(lowered, ".markdown"), strings.HasSuffix(lowered, ".rst"):
		return 2
	default:
		return 3
	}
}

// pickManifests 按优先级挑出根层的清单文件，返回仓库里的真实文件名。
func pickManifests(entries []apiclient.Entry, limit int) []string {
	if limit <= 0 {
		return nil
	}

	present := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.Depth != 1 || entry.Type != "file" {
			continue
		}
		present[strings.ToLower(entry.Name)] = entry.Name
	}

	picked := make([]string, 0, limit)
	for _, name := range manifestNames {
		if len(picked) >= limit {
			break
		}
		if actual, ok := present[strings.ToLower(name)]; ok {
			picked = append(picked, actual)
		}
	}
	return picked
}
