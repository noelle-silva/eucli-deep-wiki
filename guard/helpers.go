package guard

import (
	"fmt"

	"eucli-deep-wiki-contract-guard/contract"
	"eucli-deep-wiki-contract-guard/harness"
)

// requireJSON 断言一个给定的状态码与 JSON 正文，返回解析后的对象。
func requireJSON(reporter *Reporter, fact string, message harness.Message, wantStatus int) (map[string]any, bool) {
	if message.Status != wantStatus {
		reporter.Mismatch(contract.Server, fact, fmt.Sprintf("HTTP %d", wantStatus), message.Describe())
		return nil, false
	}
	if message.JSON == nil {
		reporter.Mismatch(contract.Server, fact, "JSON 对象正文", message.Describe())
		return nil, false
	}
	return message.JSON, true
}

// arrayField 取一个数组字段；缺失或类型不对时报告并返回 nil。
func arrayField(reporter *Reporter, fact string, object map[string]any, name string) []any {
	value, ok := object[name]
	if !ok {
		reporter.Mismatch(contract.Server, fact, "存在", "缺失")
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		reporter.Mismatch(contract.Server, fact, "数组", fmt.Sprintf("%T", value))
		return nil
	}
	return items
}

// objectField 取一个对象字段。
func objectField(reporter *Reporter, fact string, object map[string]any, name string) map[string]any {
	value, ok := object[name]
	if !ok {
		reporter.Mismatch(contract.Server, fact, "存在", "缺失")
		return nil
	}
	nested, ok := value.(map[string]any)
	if !ok {
		reporter.Mismatch(contract.Server, fact, "对象", fmt.Sprintf("%T", value))
		return nil
	}
	return nested
}

// findNamed 在对象数组里按 name 字段找一条。
func findNamed(items []any, name string) (map[string]any, bool) {
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if textOf(object, "name") == name {
			return object, true
		}
	}
	return nil, false
}

// requireRepo 从名单总览里取出某个仓库；取不到时报告。
func requireRepo(reporter *Reporter, items []any, name string) map[string]any {
	item, ok := findNamed(items, name)
	if !ok {
		reporter.Mismatch(contract.Server, "GET /v1/repos 名单里的仓库", "包含 "+name, "缺失")
		return nil
	}
	return item
}

// textOf 读一个字符串字段，缺失时返回空串。
func textOf(object map[string]any, name string) string {
	value, ok := object[name]
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return text
}

// boolOf 读一个布尔字段。
func boolOf(object map[string]any, name string) (bool, bool) {
	value, ok := object[name]
	if !ok {
		return false, false
	}
	flag, ok := value.(bool)
	return flag, ok
}

// numberOf 读一个数字字段。
func numberOf(object map[string]any, name string) (float64, bool) {
	value, ok := object[name]
	if !ok {
		return 0, false
	}
	number, ok := value.(float64)
	return number, ok
}
