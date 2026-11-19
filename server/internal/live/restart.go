package live

import (
	"path/filepath"

	"eucli-deep-wiki-server/internal/config"
)

// 改了但必须重启才能生效的字段名，输出顺序固定，便于比对。
const (
	fieldHTTPAddress     = "httpAddress"
	fieldDataRoot        = "dataRoot"
	fieldRepositoryRoot  = "repositoryRoot"
	fieldRepositoryIndex = "repositoryIndex"
)

// startupChanges 列出改了但必须重启才能生效的字段。
//
// 这几个字段在启动时就被固化：监听地址由 http.Server 持有（端口归画像，宿主归配置文件），数据根目录决定
// 索引文件与状态文件的位置，仓库目录与索引文件名决定实际读写位置。
// 其余字段都在重载后立即生效，因此不会出现在这里。
//
// 比较的是「已经由画像接管之后」的生效地址：只改配置文件里的端口不会出现在这里，
// 那个端口已经不管事了，提示重启等于说谎。
func startupChanges(startup *config.Config, current *config.Config) []string {
	changes := make([]string, 0, 4)
	if startup.HTTPAddress != current.HTTPAddress {
		changes = append(changes, fieldHTTPAddress)
	}
	if absolutePath(startup.DataRoot) != absolutePath(current.DataRoot) {
		changes = append(changes, fieldDataRoot)
	}
	if startup.RepositoryRoot != current.RepositoryRoot {
		changes = append(changes, fieldRepositoryRoot)
	}
	if startup.RepositoryIndex != current.RepositoryIndex {
		changes = append(changes, fieldRepositoryIndex)
	}
	return changes
}

// absolutePath 把相对的数据根目录换算成绝对路径再比较，
// 免得 "data" 与 "./data" 被判成两个不同取值。
func absolutePath(value string) string {
	absolute, err := filepath.Abs(value)
	if err != nil {
		return filepath.Clean(value)
	}
	return absolute
}
