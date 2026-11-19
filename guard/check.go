// Package guard 是两侧契约的守护者：它谁也不被谁依赖，却同时认识两侧，
// 用真实的服务端与真实的命令行核对同一份契约。
//
// 用例按「接口一族」「数字一族」「正文与响应头一族」「错误一族」「命令行一族」
// 「归属一族」组织。每条用例只守一小类事实，因此一处偏移只会让相关的那几条
// 变红，并且报错会直接说清「哪一侧、哪个事实、期望什么、实际什么」。
package guard

import (
	"fmt"
	"strings"

	"eucli-deep-wiki-contract-guard/contract"
	"eucli-deep-wiki-contract-guard/harness"
)

// Deviation 是一处偏移：只描述事实，不做猜测。
type Deviation struct {
	// Side 是这一处偏移落在哪一侧。
	Side contract.Side
	// Fact 是契约里的事实名。
	Fact string
	// Expected 是契约要求的。
	Expected string
	// Actual 是实现实际给出的。
	Actual string
}

// String 是单条偏移的标准表达：哪一侧、哪个事实、期望什么、实际什么。
func (deviation Deviation) String() string {
	return fmt.Sprintf("[%s] %s：期望 %s，实际 %s", deviation.Side, deviation.Fact, deviation.Expected, deviation.Actual)
}

// Reporter 收集一条用例里出现的全部偏移；它不中断，因此一次运行能看到所有问题。
type Reporter struct {
	deviations []Deviation
}

// Deviations 返回收集到的偏移。
func (reporter *Reporter) Deviations() []Deviation {
	return reporter.deviations
}

// Mismatch 记一处偏移。
func (reporter *Reporter) Mismatch(side contract.Side, fact string, expected string, actual string) {
	reporter.deviations = append(reporter.deviations, Deviation{
		Side: side, Fact: fact, Expected: expected, Actual: actual,
	})
}

// Equal 比较两个值；不相等时记一处偏移。expected 为空串时按「必须存在」表达。
func (reporter *Reporter) Equal(side contract.Side, fact string, expected any, actual any) {
	if fmt.Sprint(expected) == fmt.Sprint(actual) {
		return
	}
	reporter.Mismatch(side, fact, fmt.Sprintf("%v", expected), fmt.Sprintf("%v", actual))
}

// Fields 要求一个 JSON 对象带着契约点名的全部字段。
func (reporter *Reporter) Fields(side contract.Side, fact string, object map[string]any, names []string) {
	if object == nil {
		reporter.Mismatch(side, fact, "一个 JSON 对象", "不是一个 JSON 对象")
		return
	}
	for _, name := range names {
		if _, ok := object[name]; !ok {
			reporter.Mismatch(side, fact+" 的字段 "+name, "存在", "缺失")
		}
	}
}

// FieldAbsent 要求一个 JSON 对象不带某个字段。
func (reporter *Reporter) FieldAbsent(side contract.Side, fact string, object map[string]any, name string) {
	if _, ok := object[name]; ok {
		reporter.Mismatch(side, fact+" 的字段 "+name, "不出现", "出现了")
	}
}

// Header 要求响应带某个头且值符合预期；expected 为空串表示只要求存在且非空。
func (reporter *Reporter) Header(side contract.Side, fact string, message harness.Message, name string, expected string) {
	actual := message.Header.Get(name)
	switch {
	case actual == "":
		reporter.Mismatch(side, fact+" 的响应头 "+name, "存在", "缺失")
	case expected != "" && actual != expected:
		reporter.Mismatch(side, fact+" 的响应头 "+name, expected, actual)
	}
}

// Outcome 断言「这次响应本应是这条错误」，返回它是否成立，便于调用点决定是否继续断言。
func (reporter *Reporter) Outcome(fact string, message harness.Message, expectedStatus int, expectedCode string) bool {
	expected := fmt.Sprintf("HTTP %d + code=%s", expectedStatus, expectedCode)
	actual := fmt.Sprintf("HTTP %d + code=%s", message.Status, message.Code())
	if message.Status == expectedStatus && message.Code() == expectedCode {
		if message.JSON == nil {
			reporter.Mismatch(contract.Server, fact, "JSON 对象", message.Describe())
			return false
		}
		reporter.Fields(contract.Server, fact, message.JSON, []string{"code", "message"})
		return true
	}
	reporter.Mismatch(contract.Server, fact, expected, actual+"；"+message.Describe())
	return false
}

// Check 是一条用例。
type Check struct {
	// ID 是稳定的用例标识，用来定向跑单条。
	ID string
	// Family 是它所属的类别。
	Family contract.Family
	// Title 是它守的事实，一句话。
	Title string
	// World 是这条用例需要的现场。
	World harness.Kind
	// Run 是断言本体；它只通过 Reporter 报告，不自己抛错。
	Run func(world *harness.World, reporter *Reporter)
	// Guards 是这条用例守住的契约事实。
	Guards []contract.Guardian
}

// Result 是一条用例的运行结果。
type Result struct {
	Check      Check
	Deviations []Deviation
}

// Passed 表示这条用例没有偏移。
func (result Result) Passed() bool {
	return len(result.Deviations) == 0
}

// Summary 是一次完整验证的结果。
type Summary struct {
	Results []Result
}

// Passed 表示全部用例与契约覆盖都通过。
func (summary Summary) Passed() bool {
	for _, result := range summary.Results {
		if !result.Passed() {
			return false
		}
	}
	return true
}

// Failures 返回全部失败的用例。
func (summary Summary) Failures() []Result {
	failures := make([]Result, 0)
	for _, result := range summary.Results {
		if !result.Passed() {
			failures = append(failures, result)
		}
	}
	return failures
}

// Count 返回用例总数与通过数。
func (summary Summary) Count() (total int, passed int) {
	for _, result := range summary.Results {
		if result.Passed() {
			passed++
		}
	}
	return len(summary.Results), passed
}

// guardsOf 汇总全部用例的守护声明。
func (summary Summary) guardsOf() []contract.Guardian {
	guards := make([]contract.Guardian, 0)
	for _, result := range summary.Results {
		guards = append(guards, result.Check.Guards...)
	}
	return guards
}

// coverageCheck 是最后一块门禁：契约里有、用例里没有的事实必须变红。
func coverageCheck(declared []contract.Guardian) Result {
	check := Check{
		ID:     "coverage.contract",
		Family: "契约覆盖",
		Title:  "契约里的每条事实都有用例守着",
	}
	reporter := &Reporter{}
	missing := contract.Missing(declared)
	for _, requirement := range missing {
		reporter.Mismatch(requirement.Side, requirement.Title, "有用例守住", "没有任何用例守住")
	}
	return Result{Check: check, Deviations: reporter.Deviations()}
}

// describeGuards 把一条用例守的事实压成一行，便于报告。
func describeGuards(guards []contract.Guardian) string {
	parts := make([]string, 0, len(guards))
	for _, guard := range guards {
		switch {
		case guard.Endpoint != "":
			parts = append(parts, fmt.Sprintf("%s@%s", guard.Endpoint, guard.Side))
		case guard.Code != "":
			parts = append(parts, fmt.Sprintf("%s@%s", guard.Code, guard.Side))
		default:
			parts = append(parts, fmt.Sprintf("%s@%s", guard.Number, guard.Side))
		}
	}
	return strings.Join(parts, "、")
}
