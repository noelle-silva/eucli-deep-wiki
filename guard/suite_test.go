package guard_test

import (
	"testing"

	contractguard "eucli-deep-wiki-contract-guard"
	"eucli-deep-wiki-contract-guard/contract"
	"eucli-deep-wiki-contract-guard/harness"
)

// TestContract 把每条用例变成一个子测试：定向跑单条时只跑那一条，
// 报错消息就是「哪一侧、哪个事实、期望什么、实际什么」。
func TestContract(t *testing.T) {
	for _, check := range contractguard.Checks() {
		check := check
		t.Run(check.ID, func(t *testing.T) {
			t.Parallel()
			result := contractguard.RunCheck(check)
			for _, deviation := range result.Deviations {
				t.Errorf("%s", deviation)
			}
		})
	}
}

// TestContractCoverage 是静态门禁：契约里有、用例里没有的事实一律变红。
func TestContractCoverage(t *testing.T) {
	result := contractguard.CoverageCheck()
	for _, deviation := range result.Deviations {
		t.Errorf("%s", deviation)
	}
}

// TestCheckIDsUnique 保证每条用例的标识唯一：重复标识会让报告里出现两条
// 一模一样的行，一处偏移就看不出到底是哪一条。
func TestCheckIDsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, check := range contractguard.Checks() {
		if seen[check.ID] {
			t.Errorf("用例标识重复：%s（%s）", check.ID, check.Title)
			continue
		}
		seen[check.ID] = true
	}
}

// TestCommandKeysUnique 保证命令表里每条命令的稳定标识唯一。
func TestCommandKeysUnique(t *testing.T) {
	seen := map[string]string{}
	for _, route := range contract.CommandRoutes {
		if previous, exists := seen[route.Key]; exists {
			t.Errorf("命令表里「%s」与「%s」用了同一个标识 %s", previous, route.ID, route.Key)
			continue
		}
		seen[route.Key] = route.ID
	}
}

// TestWorldNamesResolvable 保证契约里点名的每一种现场都能被真的开出来。
//
// 世界名是「契约 → 现场」的唯一接缝：写错一个名字，相关用例会在运行时才炸。
func TestWorldNamesResolvable(t *testing.T) {
	for _, name := range contract.Worlds {
		if _, ok := harness.Kinds[name]; !ok {
			t.Errorf("契约里的世界名 %q 在现场映射表里不存在", name)
		}
	}
	for _, check := range contractguard.Checks() {
		if check.World == "" {
			continue
		}
		if _, ok := harness.Kinds[string(check.World)]; !ok {
			t.Errorf("用例 %s 需要的现场 %q 没有对应实现", check.ID, check.World)
		}
	}
	for _, route := range contract.CommandRoutes {
		if _, ok := harness.Kinds[route.World]; !ok {
			t.Errorf("命令「%s」声明的现场 %q 没有对应实现", route.ID, route.World)
		}
	}
}
