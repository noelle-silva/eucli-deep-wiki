package contract

// Number 是两侧各持一份、必须逐一相等的数字。
//
// 服务端那份是权威口径（真收敛按它做），命令行那份只用于帮助文本与「已按上限
// 处理」提示。两份没有共享代码，因此这些数只能靠契约对齐：用例逐个数比较
// 「服务端常量 == 契约值 == 命令行常量」，任何一侧改歪都会指出是哪一侧。
type Number struct {
	// Key 是契约里的名字。
	Key string
	// Value 是契约规定的取值。
	Value int
	// ServerName 与 ClientName 是两侧各自的标识，报错时直接告诉人去哪儿找。
	ServerName string
	ClientName string
	Note       string
}

// Numbers 是全部需要逐一对齐的数字。
var Numbers = []Number{
	{
		Key: "DefaultTreeDepth", Value: 1,
		ServerName: "vault.DefaultTreeDepth", ClientName: "params.DefaultTreeDepth",
		Note: "目录展开层数的缺省值",
	},
	{
		Key: "MaxTreeDepth", Value: 8,
		ServerName: "vault.MaxTreeDepth", ClientName: "params.MaxTreeDepth",
		Note: "目录展开层数的硬上限",
	},
	{
		Key: "DefaultFindLimit", Value: 200,
		ServerName: "vault.DefaultFindLimit", ClientName: "params.DefaultFindLimit",
		Note: "文件名查找的缺省条数",
	},
	{
		Key: "MaxFindLimit", Value: 10000,
		ServerName: "vault.MaxFindLimit", ClientName: "params.MaxFindLimit",
		Note: "文件名查找的条数硬上限",
	},
	{
		Key: "MaxSearchContext", Value: 10,
		ServerName: "vault.MaxSearchContext", ClientName: "params.MaxSearchContext",
		Note: "检索命中前后各带上下文行数的上限",
	},
	{
		Key: "MaxSearchLimit", Value: 100,
		ServerName: "vault.MaxSearchLimit", ClientName: "params.MaxSearchLimit",
		Note: "内容检索的命中条数上限",
	},
}

// NumberByKey 按契约名字取一个数字；不存在时直接失败。
func NumberByKey(key string) Number {
	for _, number := range Numbers {
		if number.Key == key {
			return number
		}
	}
	panic("contract: unknown number " + key)
}

// LineCase 是一条行区间写法在两侧都必须得出的同一结果。
type LineCase struct {
	Input string
	// Valid 表示这个写法合法。
	Valid bool
	// Start 与 End 是解析结果，两端 1 基且包含；0 表示该端未限定。
	Start int
	End   int
	// Normalized 是归一回写后的写法；Invalid 时为空。
	Normalized string
	// Note 说明这条为什么这样判。
	Note string
}

// LineRangeCases 是行区间写法的行为表：语法由两侧各写一份，结果必须一致。
var LineRangeCases = []LineCase{
	{Input: "", Valid: true, Note: "空串等于未限定"},
	{Input: "362", Valid: true, Start: 362, End: 362, Normalized: "362", Note: "单行"},
	{Input: "362-520", Valid: true, Start: 362, End: 520, Normalized: "362-520", Note: "闭区间"},
	{Input: "362-", Valid: true, Start: 362, Normalized: "362-", Note: "起到末行"},
	{Input: "-520", Valid: true, End: 520, Normalized: "-520", Note: "首行到 520"},
	{Input: " 362 - 520 ", Valid: true, Start: 362, End: 520, Normalized: "362-520", Note: "两侧空白按空白处理"},
	{Input: "007", Valid: true, Start: 7, End: 7, Normalized: "7", Note: "前导零归一"},
	{Input: "5-5", Valid: true, Start: 5, End: 5, Normalized: "5", Note: "区间退化成单行"},
	{Input: "+5", Valid: true, Start: 5, End: 5, Normalized: "5", Note: "正号按整数解析"},
	{Input: "-1", Valid: true, End: 1, Normalized: "-1", Note: "首行到 1"},
	{Input: "1-", Valid: true, Start: 1, Normalized: "1-", Note: "第 1 行到末行"},
	{Input: "0", Valid: false, Note: "行号从 1 开始"},
	{Input: "-", Valid: false, Note: "两端都为空"},
	{Input: "10-5", Valid: false, Note: "起始行不能大于结束行"},
	{Input: "abc", Valid: false, Note: "不是整数"},
	{Input: "1-2-3", Valid: false, Note: "不是一段区间"},
}

// ServerKeyMinLength 是服务端对单个 key 的长度下限。
//
// 命令行那一侧只做基本合法性（非空、无空白），因此它必须接受服务端能接受的
// 任何 key：服务端接受 ⇒ 命令行接受。这条顺序也是契约的一部分。
const ServerKeyMinLength = 16

// 耗时字段的名字与单位必须自洽。
const (
	// FieldCompactDuration 是瘦身接口的耗时字段，单位毫秒。
	FieldCompactDuration = "durationMs"
	// FieldSyncDuration 是同步结果里的耗时字段，既有接口不变，单位纳秒。
	FieldSyncDuration = "duration"
	// FieldCLIDuration 是命令行 JSON 输出里的耗时字段，单位毫秒。
	FieldCLIDuration = "durationMs"
)
