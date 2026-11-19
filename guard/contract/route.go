package contract

import (
	"regexp"
	"strings"
)

// Route 是用例实际请求的路径模板：把 {name} 换成一个具体仓库名。
func (e Endpoint) Route(name string) string {
	return strings.ReplaceAll(e.Path, placeholderName, name)
}

// 路由模板里的占位写法。
const (
	placeholderName = "{name}"
	// placeholderPattern 匹配模板里的任何占位段，供正则化时整体替换。
	placeholderPattern = `\{[a-z]+\}`
	// placeholderSegment 是一段「非空、不含斜杠」的路径，用来替代占位段。
	placeholderSegment = `[^/]+`
)

// PathPattern 是路由模板对应的正则，用来核对命令行实际请求的路径。
//
// 实现分两步：先把整条模板当字面文本转义，再把转义后的占位段换回通配段。
// 这样 /v1/repos/{name} 只匹配 /v1/repos/<一段名字>，而 /v1/repos/{name}/sync
// 不会误配到 /v1/repos/{name}/compact。
func (e Endpoint) PathPattern() *regexp.Regexp {
	masked := regexp.MustCompile(placeholderPattern).ReplaceAllString(e.Path, "PLACEHOLDER")
	quoted := regexp.QuoteMeta(masked)
	return regexp.MustCompile("^" + strings.ReplaceAll(quoted, "PLACEHOLDER", placeholderSegment) + "$")
}
