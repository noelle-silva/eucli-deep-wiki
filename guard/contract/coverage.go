package contract

import "fmt"

// Guardian 是「某条用例守住了某条契约事实」的声明。
//
// 每条用例在 Guards 里列出自己守的事实，覆盖率检查把全部声明与契约表比对：
// 契约里有、用例里没有的事实会单独变红，因此「契约里写了但没人守」不会悄悄溜过去。
type Guardian struct {
	// Endpoint 是接口标识；为空表示这条声明不是接口事实。
	Endpoint string
	// Code 是错误标识；为空表示不是错误事实。
	Code string
	// Number 是数字标识；为空表示不是数字事实。
	Number string
	// Action 是瘦身动作结论的取值；为空表示不是这条事实。
	Action string
	// Format 是打包格式的取值；为空表示不是这条事实。
	Format string
	// Side 是被验证的那一侧。
	Side Side
}

// Key 是这条声明的规范化键。
func (guardian Guardian) Key() string {
	switch {
	case guardian.Endpoint != "":
		return fmt.Sprintf("endpoint:%s@%s", guardian.Endpoint, guardian.Side)
	case guardian.Code != "":
		return fmt.Sprintf("code:%s@%s", guardian.Code, guardian.Side)
	case guardian.Action != "":
		return fmt.Sprintf("action:%s@%s", guardian.Action, guardian.Side)
	case guardian.Format != "":
		return fmt.Sprintf("format:%s@%s", guardian.Format, guardian.Side)
	default:
		return fmt.Sprintf("number:%s@%s", guardian.Number, guardian.Side)
	}
}

// Requirement 是一条必须被守住的契约事实。
type Requirement struct {
	Key   string
	Title string
	// Side 是这条事实要求被验证的那一侧。
	Side Side
	// Gap 非空表示这一条不通过真实诱发来守，原因写在这里；报告里会单独列出。
	Gap string
}

// Gaps 是已说明的缺口：契约有、但这一轮没有用真实路径诱发的事实。
//
// 缺口必须写明原因，不能靠「反正没红」蒙过去：报告会把它们单独打印出来，
// 而没写在这里的缺口一律变红。
var Gaps = map[string]string{
	"code:cache_busy@服务端": "服务端只在两次清理真的并发时才会给出 cache_busy；" +
		"为了让窗口稳定存在，需要一份大到能以毫秒计仍在删除的缓存目录，" +
		"这把一条确定性断言换成了时序竞赛，因此只守命令行把 cache_busy 映射成退出码 6。",
	"code:invalid_body@命令行": "命令行的调用层永远自己序列化结构化请求体，用户没有任何入口让它发出" +
		"「不是合法结构化数据」的正文，因此这条标识只可能由服务端产出；" +
		"命令行把它映射成退出码 2 由退出码映射用例守住。",
	"number:MaxFindLimit@服务端": "服务端只有在匹配数超过一万时才会体现出 10000 这条上限；" +
		"铺一万个文件的现场实测要 6 秒以上，与它守住的信息量不成比例。" +
		"命令行写着的同一个数、以及缺省 200 的真收敛都由 number.find.limit 守住。",
}

// Requirements 由契约表推导出全部必须被守住的事实。
func Requirements() []Requirement {
	required := make([]Requirement, 0, len(Endpoints)*2+len(Errors)*2+len(Numbers)*2)

	routed := make(map[string]bool, len(CommandRoutes))
	for _, route := range CommandRoutes {
		routed[route.Endpoint] = true
	}

	for _, endpoint := range Endpoints {
		required = append(required, Requirement{
			Key:   Guardian{Endpoint: endpoint.ID, Side: Server}.Key(),
			Title: fmt.Sprintf("接口 %s（%s）", endpoint.ID, endpoint.Title),
			Side:  Server,
		})
		if routed[endpoint.ID] {
			required = append(required, Requirement{
				Key:   Guardian{Endpoint: endpoint.ID, Side: Client}.Key(),
				Title: fmt.Sprintf("命令行走 %s", endpoint.ID),
				Side:  Client,
			})
		}
	}

	for _, spec := range Errors {
		for _, side := range []Side{Server, Client} {
			key := Guardian{Code: spec.Code, Side: side}.Key()
			required = append(required, Requirement{
				Key:   key,
				Title: fmt.Sprintf("错误标识 %s（%s）", spec.Code, side),
				Side:  side,
				Gap:   Gaps[key],
			})
		}
	}

	for _, number := range Numbers {
		for _, side := range []Side{Server, Client} {
			key := Guardian{Number: number.Key, Side: side}.Key()
			required = append(required, Requirement{
				Key:   key,
				Title: fmt.Sprintf("数字 %s（%s）", number.Key, side),
				Side:  side,
				Gap:   Gaps[key],
			})
		}
	}

	// 枚举型取值：契约把它们的合法集合定死了，因此每一个取值都必须被真实诱发一次。
	for _, action := range RequiredCompactActions {
		key := Guardian{Action: action, Side: Server}.Key()
		required = append(required, Requirement{
			Key:   key,
			Title: fmt.Sprintf("瘦身动作结论 %s", action),
			Side:  Server,
			Gap:   Gaps[key],
		})
	}
	for _, format := range RequiredArchiveFormats {
		key := Guardian{Format: format, Side: Server}.Key()
		required = append(required, Requirement{
			Key:   key,
			Title: fmt.Sprintf("打包格式 %s", format),
			Side:  Server,
			Gap:   Gaps[key],
		})
	}

	return required
}

// Missing 返回契约要求、但没有任何用例声明守住的事实。
func Missing(declared []Guardian) []Requirement {
	seen := make(map[string]bool, len(declared))
	for _, guard := range declared {
		seen[guard.Key()] = true
	}

	missing := make([]Requirement, 0)
	for _, requirement := range Requirements() {
		if requirement.Gap != "" || seen[requirement.Key] {
			continue
		}
		missing = append(missing, requirement)
	}
	return missing
}

// DocumentedGaps 返回已经写明原因的缺口，用于报告里单独列出。
func DocumentedGaps() []Requirement {
	gaps := make([]Requirement, 0)
	for _, requirement := range Requirements() {
		if requirement.Gap != "" {
			gaps = append(gaps, requirement)
		}
	}
	return gaps
}
