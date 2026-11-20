package vault

import "strings"

// ExcludedDirs 是「按名字排除目录」的名单。
//
// 它只回答一件事：这个名字的条目要不要跳过。哪些名字进名单由调用方决定——
// 服务端不认识「依赖」「缓存」这类概念。零值（nil）表示不排除任何目录，因此
// 不提供这份名单时，遍历行为与没有这项能力时完全一致。
type ExcludedDirs map[string]struct{}

// ParseExcludedDirs 解析逗号分隔的目录名列表。
//
// 空项与多余逗号一律忽略；名字按小写归一，因此匹配不区分大小写。
func ParseExcludedDirs(raw string) ExcludedDirs {
	excluded := make(ExcludedDirs)
	for _, item := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(item))
		if name == "" {
			continue
		}
		excluded[name] = struct{}{}
	}
	return excluded
}

// Skips 判断一个条目要不要在遍历里跳过。
//
// 只作用于目录：同名的普通文件照常返回。名字按小写比较，因此大小写不敏感。
func (excluded ExcludedDirs) Skips(name string, isDir bool) bool {
	if !isDir {
		return false
	}
	_, hit := excluded[strings.ToLower(name)]
	return hit
}
