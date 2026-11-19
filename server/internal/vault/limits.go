package vault

// 查询参数的上限与默认值。
//
// 这几个数是服务端的权威口径：接口按它们收敛请求，帮助文本与「已按上限处理」
// 的提示也必须写同样的数。命令行模块里有一份独立实现（它自己的 params 包），
// 取值与这里逐一对应——两侧只靠契约对齐，没有共享代码，改任何一个都要两边一起改。
const (
	// DefaultTreeDepth 是未指定层数时展开的层数，与历史行为一致。
	DefaultTreeDepth = 1
	// MaxTreeDepth 是单次请求允许展开的最大层数。
	MaxTreeDepth = 8

	// DefaultFindLimit 是请求未指定条数时使用的默认上限。
	DefaultFindLimit = 200
	// MaxFindLimit 是单次请求可以返回的结果数硬上限。
	MaxFindLimit = 10000

	// MaxSearchContext 是单处命中最多带出的上下文行数（前后各算）。
	MaxSearchContext = 10
	// MaxSearchLimit 是单次检索最多返回的命中条数。
	MaxSearchLimit = 100
)
