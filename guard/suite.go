package guard

import (
	"strings"

	"eucli-deep-wiki-contract-guard/contract"
	"eucli-deep-wiki-contract-guard/harness"
)

// Checks 返回全部用例，顺序按类别排列。
func Checks() []Check {
	checks := make([]Check, 0, 96)
	checks = append(checks, OwnershipChecks()...)
	checks = append(checks, APIChecks()...)
	checks = append(checks, BodyChecks()...)
	checks = append(checks, NumberChecks()...)
	checks = append(checks, ErrorChecks()...)
	checks = append(checks, CommandChecks()...)
	return checks
}

// RunOptions 控制一次验证的范围。
type RunOptions struct {
	// Filter 非空时只跑标识里包含它的用例；此时不跑契约覆盖门禁
	// （覆盖是静态事实，定向跑单条时不该因为它变红）。
	Filter string
}

// Run 跑完选中的用例并给出汇总。
func Run(options RunOptions) Summary {
	results := make([]Result, 0, len(Checks())+1)
	for _, check := range Checks() {
		if options.Filter != "" && !strings.Contains(check.ID, options.Filter) {
			continue
		}
		results = append(results, RunCheck(check))
	}
	if options.Filter == "" {
		results = append(results, CoverageCheck())
	}
	return Summary{Results: results}
}

// CoverageCheck 只做契约覆盖门禁：每一条契约事实都必须有用例声明守住。
func CoverageCheck() Result {
	return coverageCheck(allGuards())
}

// allGuards 汇总全部用例的守护声明。
func allGuards() []contract.Guardian {
	guards := make([]contract.Guardian, 0)
	for _, check := range Checks() {
		guards = append(guards, check.Guards...)
	}
	return guards
}

// RunCheck 跑单条用例：为它开一份隔离现场，跑完即拆。
//
// World 为空表示这条用例不需要现场（例如归属与构建这一组读源码就能判断）。
func RunCheck(check Check) Result {
	reporter := &Reporter{}
	if check.World == "" {
		check.Run(nil, reporter)
		return Result{Check: check, Deviations: reporter.Deviations()}
	}

	world, err := harness.Open(harness.Options{Kind: check.World})
	if err != nil {
		reporter.Mismatch(contract.Both, "验证现场", "能够建立", err.Error())
		return Result{Check: check, Deviations: reporter.Deviations()}
	}
	defer world.Close()

	check.Run(world, reporter)
	return Result{Check: check, Deviations: reporter.Deviations()}
}
