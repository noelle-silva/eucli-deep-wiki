package guard

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"eucli-deep-wiki-contract-guard/contract"
)

// WriteReport 输出一次验证的通过/失败汇总。
//
// 先按类别列每条用例，再给汇总；失败的事实逐条展开（哪一侧、哪个事实、
// 期望什么、实际什么），最后单独列出「契约有但这一轮没有真实诱发」的缺口。
func WriteReport(writer io.Writer, summary Summary) error {
	total, passed := summary.Count()

	fmt.Fprintln(writer, "契约守护：用真实服务端与真实命令行核对同一份契约")
	fmt.Fprintln(writer, strings.Repeat("=", 64))

	for _, family := range familiesOf(summary) {
		fmt.Fprintf(writer, "\n[%s]\n", family)
		for _, result := range summary.Results {
			if familyOf(result.Check) != family {
				continue
			}
			status := "PASS"
			if !result.Passed() {
				status = "FAIL"
			}
			fmt.Fprintf(writer, "  %s  %-34s %s\n", status, result.Check.ID, result.Check.Title)
			for _, deviation := range result.Deviations {
				fmt.Fprintf(writer, "        - %s\n", deviation)
			}
		}
	}

	fmt.Fprintln(writer, "\n"+strings.Repeat("-", 64))
	fmt.Fprintf(writer, "汇总：通过 %d / 共 %d 条用例\n", passed, total)
	fmt.Fprintf(writer, "守护的契约事实：接口 %d 个、错误标识 %d 条、数字 %d 个\n",
		len(contract.Endpoints), len(contract.Errors), len(contract.Numbers))

	gaps := contract.DocumentedGaps()
	if len(gaps) > 0 {
		fmt.Fprintf(writer, "\n已说明的缺口（%d 条，未用真实路径诱发）：\n", len(gaps))
		for _, gap := range gaps {
			fmt.Fprintf(writer, "  - %s：%s\n", gap.Title, gap.Gap)
		}
	}

	failures := summary.Failures()
	if len(failures) == 0 {
		fmt.Fprintln(writer, "\n结论：全部通过")
		return nil
	}

	fmt.Fprintf(writer, "\n结论：%d 条用例不通过\n", len(failures))
	for _, result := range failures {
		fmt.Fprintf(writer, "  - %s（%s）\n", result.Check.ID, result.Check.Title)
		for _, deviation := range result.Deviations {
			fmt.Fprintf(writer, "      %s\n", deviation)
		}
	}
	return nil
}

// familiesOf 返回这次结果里出现的类别，按契约里的展示顺序排列。
func familiesOf(summary Summary) []contract.Family {
	seen := map[contract.Family]bool{}
	order := make([]contract.Family, 0, len(contract.Families))
	for _, family := range contract.Families {
		for _, result := range summary.Results {
			if familyOf(result.Check) == family && !seen[family] {
				seen[family] = true
				order = append(order, family)
			}
		}
	}

	extra := make([]contract.Family, 0)
	for _, result := range summary.Results {
		family := familyOf(result.Check)
		if !seen[family] {
			seen[family] = true
			extra = append(extra, family)
		}
	}
	sort.SliceStable(extra, func(left int, right int) bool { return extra[left] < extra[right] })
	return append(order, extra...)
}

// familyOf 取用例的类别；没有类别的用例归到「其它」。
func familyOf(check Check) contract.Family {
	if check.Family == "" {
		return "其它"
	}
	return check.Family
}

// WriteCoverage 输出「每条契约事实由哪几条用例守着」的完整对照。
//
// 清单由契约表与用例的守护声明直接推导，不手写：因此它不会随着用例改名而失真，
// 也不会出现「文档里说守住了、代码里其实没有」。
func WriteCoverage(writer io.Writer, checks []Check) error {
	byRequirement := make(map[string][]string, len(checks)*4)
	for _, check := range checks {
		for _, guard := range check.Guards {
			byRequirement[guard.Key()] = append(byRequirement[guard.Key()], check.ID)
		}
	}

	for _, family := range familiesForCoverage() {
		fmt.Fprintf(writer, "\n[%s]\n", family.title)
		for _, requirement := range family.requirements {
			guards := byRequirement[requirement.Key]
			switch {
			case len(guards) > 0:
				fmt.Fprintf(writer, "  %-46s %s\n", requirement.Key, strings.Join(guards, "、"))
				if requirement.Gap != "" {
					// 同一事实在两侧可能一侧守住、一侧列缺口，说明照常打印。
					fmt.Fprintf(writer, "  %-46s 另一侧已说明的缺口：%s\n", "", requirement.Gap)
				}
			case requirement.Gap != "":
				fmt.Fprintf(writer, "  %-46s 已说明的缺口：%s\n", requirement.Key, requirement.Gap)
			default:
				fmt.Fprintf(writer, "  %-46s 未守住（契约覆盖门禁会把它判红）\n", requirement.Key)
			}
		}
	}
	return nil
}

// coverageFamily 是覆盖清单的一个分组。
type coverageFamily struct {
	title        string
	requirements []contract.Requirement
}

// familiesForCoverage 按「接口 / 错误 / 数字」分组，组内保持契约表的顺序。
func familiesForCoverage() []coverageFamily {
	endpoints := make([]contract.Requirement, 0, len(contract.Endpoints)*2)
	codes := make([]contract.Requirement, 0, len(contract.Errors)*2)
	numbers := make([]contract.Requirement, 0, len(contract.Numbers)*2)

	for _, requirement := range contract.Requirements() {
		switch {
		case strings.HasPrefix(requirement.Key, "endpoint:"):
			endpoints = append(endpoints, requirement)
		case strings.HasPrefix(requirement.Key, "code:"):
			codes = append(codes, requirement)
		default:
			numbers = append(numbers, requirement)
		}
	}

	return []coverageFamily{
		{title: "接口（每侧一条）", requirements: endpoints},
		{title: "错误标识（服务端产出 + 命令行映射）", requirements: codes},
		{title: "数字（两侧各持一份）", requirements: numbers},
	}
}
