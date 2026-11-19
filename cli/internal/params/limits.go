package params

// 各查询参数的上限与默认值。
//
// 取值与服务端严格一致：帮助文本写出来的数、超限提示里的数、服务端实际生效的
// 数必须是同一个数。服务端那份定义在它自己的模块里，这里是命令行的一份，
// 两者只靠契约对齐，没有共享代码。
const (
	// DefaultTreeDepth 是 --depth 未指定时展开的层数。
	DefaultTreeDepth = 1
	// MaxTreeDepth 是 --depth 的硬上限。
	MaxTreeDepth = 8

	// DefaultFindLimit 是 --limit 未指定时返回的条数。
	DefaultFindLimit = 200
	// MaxFindLimit 是 --limit 的硬上限。
	MaxFindLimit = 10000

	// MaxSearchContext 是一处命中前后各带多少行上下文的上限。
	MaxSearchContext = 10
	// MaxSearchLimit 是 --limit 的硬上限。
	MaxSearchLimit = 100
)

// NormalizeFindLimit 把 --limit 收敛到默认值与硬上限之间，用于把
// 「实际生效多少条」解释清楚。服务端会做同样的收敛，结果一致。
func NormalizeFindLimit(limit int) int {
	if limit <= 0 {
		return DefaultFindLimit
	}
	if limit > MaxFindLimit {
		return MaxFindLimit
	}
	return limit
}
