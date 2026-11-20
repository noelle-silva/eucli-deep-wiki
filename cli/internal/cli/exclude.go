package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"eucli-dw/internal/apiclient"
)

// dependencyDirs 是「确定属于第三方依赖或缓存」的目录名：tree 与 overview 默认
// 不展示它们，也不进入它们内部遍历。
//
// 这份清单只属于命令行：什么算依赖是使用习惯，不是服务端的知识。服务端只提供
// 「按目录名排除」这一个通用能力，具体名字由调用方给。按目录名精确匹配、大小写
// 不敏感，只作用于目录——同名的普通文件照常出现。
//
// 清单不开放给用户配置：现在没有这个需求。
var dependencyDirs = []string{
	"node_modules", "bower_components", "jspm_packages",
	"vendor", "godeps",
	".venv", "venv", "__pycache__", ".tox", ".mypy_cache", ".pytest_cache", ".ruff_cache",
	"target", ".gradle", ".cargo",
	".next", ".nuxt", ".turbo", ".parcel-cache",
	"pods",
	".cache", ".sass-cache",
}

// excludeFlags 是「默认压掉依赖噪声」这件事的唯一住址：开关、清单、以及本地那
// 一遍过滤都在这里，tree 与 overview 共用同一份，不各写一套。
type excludeFlags struct {
	all bool
}

// register 把 --all 收进参数集合，解析方式与其它参数完全一致。
func (f *excludeFlags) register(flags *flag.FlagSet) {
	flags.BoolVar(&f.all, "all", false, "连依赖与缓存目录一起列出（默认排除 node_modules 等）")
}

// names 是这次要排除的目录名，同时也是发给服务端的 exclude 参数。
// --all 时为空，即不排除任何东西，回到没有这类默认行为的样子。
func (f *excludeFlags) names() []string {
	if f.all {
		return nil
	}
	return dependencyDirs
}

// announce 说明这次排除了哪些目录名。
//
// 默认压掉一批东西是命令替用户做的决定，做过的决定必须说出来，否则「为什么这里
// 没有 node_modules」只能靠猜。说明走 stderr，stdout 只放数据。
func (f *excludeFlags) announce(stderr io.Writer) {
	names := f.names()
	if len(names) == 0 {
		return
	}
	fmt.Fprintf(stderr, "（默认不列出依赖与缓存目录：%s；加 --all 改为一并列出）\n", strings.Join(names, " "))
}

// keep 留下允许出现的条目：命中清单的目录、以及它们的全部后代都不出现。
//
// 服务端拿到同一个 exclude 参数后会在遍历阶段就跳过这些目录，条目配额因此不被
// 依赖吃满；这里再收一遍，是让「默认看不到依赖」这件事在服务端还没跟上时同样
// 成立——省不下配额，但看到的内容不因服务端版本而不同。
func (f *excludeFlags) keep(entries []apiclient.Entry) []apiclient.Entry {
	names := f.names()
	if len(names) == 0 {
		return entries
	}

	excluded := make(map[string]struct{}, len(names))
	for _, name := range names {
		excluded[strings.ToLower(name)] = struct{}{}
	}

	kept := make([]apiclient.Entry, 0, len(entries))
	for _, entry := range entries {
		if !insideExcludedDir(entry, excluded) {
			kept = append(kept, entry)
		}
	}
	return kept
}

// insideExcludedDir 判断条目是否落在被排除的目录里。
//
// 目录看全部路径段；文件只看目录部分——文件自己那一段是文件名，不是目录名，
// 所以一个叫 node_modules 的普通文件照常留下。
func insideExcludedDir(entry apiclient.Entry, excluded map[string]struct{}) bool {
	segments := strings.Split(entry.Path, "/")
	if entry.Type != "dir" {
		segments = segments[:len(segments)-1]
	}
	for _, segment := range segments {
		if _, matched := excluded[strings.ToLower(segment)]; matched {
			return true
		}
	}
	return false
}

// listTree 取一段目录骨架，并按本次生效的排除清单收一遍再交出去。
//
// tree 与 overview 都从这里拿目录：排除只在数据入口做一次，下游（条目计数、
// 挑 README、挑清单文件、两种输出）看到的都是同一份，不会出现「一处排了、
// 另一处没排」。
func listTree(
	ctx context.Context,
	client *apiclient.Client,
	repo string,
	options apiclient.TreeOptions,
	excluded *excludeFlags,
) (apiclient.Tree, error) {
	options.Exclude = excluded.names()

	tree, err := client.Tree(ctx, repo, options)
	if err != nil {
		return apiclient.Tree{}, err
	}
	tree.Entries = excluded.keep(tree.Entries)
	return tree, nil
}
