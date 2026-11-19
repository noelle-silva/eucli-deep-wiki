package guard

import (
	"encoding/json"
	"fmt"
	"strings"

	"eucli-deep-wiki-contract-guard/contract"
	"eucli-deep-wiki-contract-guard/harness"
)

// CommandChecks 守「命令行一族」：命令行走哪个接口、带哪个参数、把回来的字段
// 解析成什么、失败时退出码落在哪一档。
//
// 每条命令一行用例：一行歪了只会红这一条，报错直接点名是哪条命令的哪一项。
func CommandChecks() []Check {
	checks := make([]Check, 0, len(contract.CommandRoutes)+16)
	for _, route := range contract.CommandRoutes {
		checks = append(checks, commandRouteCheck(route))
	}
	checks = append(checks, ParseChecks()...)
	checks = append(checks, Check{
		ID: "cli.key.rules", Family: contract.FamilyCommands,
		Title: "钥匙校验：服务端接受的一定不会被命令行拒绝",
		World: harness.KindStandard,
		Run:   checkKeyRules,
	})
	return checks
}

// commandRouteCheck 是一条命令的接口对应用例。
func commandRouteCheck(route contract.CommandRoute) Check {
	guards := []contract.Guardian{{Endpoint: route.Endpoint, Side: contract.Client}}
	if route.Code != "" {
		guards = append(guards, contract.Guardian{Code: route.Code, Side: contract.Client})
	}
	return Check{
		ID:     "cli.route." + route.Key,
		Family: contract.FamilyCommands,
		Title:  fmt.Sprintf("命令行「%s」走 %s", route.ID, route.Endpoint),
		World:  worldKind(route.World),
		Run: func(world *harness.World, reporter *Reporter) {
			runCommandRoute(world, reporter, route)
		},
		Guards: guards,
	}
}

// runCommandRoute 跑一次真实命令，核对它调用的接口与参数。
func runCommandRoute(world *harness.World, reporter *Reporter, route contract.CommandRoute) {
	endpoint := contract.MustEndpoint(route.Endpoint)

	args := make([]string, 0, len(route.Args))
	for _, arg := range route.Args {
		switch arg {
		case "{repo}":
			args = append(args, route.Repo)
		case "{out}":
			args = append(args, world.Command.OutputPath(route.Key+".download"))
		default:
			args = append(args, arg)
		}
	}

	outcome := world.Command.Run(args)
	reporter.Equal(contract.Client, "命令行「"+route.ID+"」的退出码", route.Exit, outcome.Exit)

	matched := requestsFor(outcome.Requests, endpoint)
	if len(matched) == 0 {
		reporter.Mismatch(contract.Client, "命令行「"+route.ID+"」调用的接口",
			endpoint.Method+" "+endpoint.Path, "实际请求："+describeRequests(outcome.Requests))
		return
	}

	request := matched[0]
	for _, name := range route.Query {
		if !request.HasQuery(name) {
			reporter.Mismatch(contract.Client, "命令行「"+route.ID+"」的查询参数 "+name, "出现", "缺失；"+request.Describe())
		}
	}
	for name, expected := range route.QueryValues {
		reporter.Equal(contract.Client, "命令行「"+route.ID+"」的查询参数 "+name, expected, request.Query.Get(name))
	}
	for _, name := range route.QueryAbsent {
		if request.HasQuery(name) {
			reporter.Mismatch(contract.Client, "命令行「"+route.ID+"」的查询参数 "+name, "不出现", "出现了；"+request.Describe())
		}
	}
	for _, name := range route.Body {
		if !request.BodyField(name) {
			reporter.Mismatch(contract.Client, "命令行「"+route.ID+"」的请求体字段 "+name, "出现", "缺失；"+request.Describe())
		}
	}
	for name, expected := range route.BodyValues {
		value, present := request.BodyValue(name)
		if !present {
			reporter.Mismatch(contract.Client, "命令行「"+route.ID+"」的请求体字段 "+name, expected, "缺失")
			continue
		}
		reporter.Equal(contract.Client, "命令行「"+route.ID+"」的请求体字段 "+name, expected, fmt.Sprint(value))
	}
	if route.Code != "" {
		reporter.Equal(contract.Client, "命令行「"+route.ID+"」落到的错误标识", route.Code, request.ResponseCode)
	}
}

// checkKeyRules 钥匙校验与地址校验。
//
// 契约给两侧定的是**不同严格度**：服务端有长度下限，命令行只做基本合法性。
// 因此这里守的是一条有方向的包含关系——服务端接受的钥匙，命令行必须也接受；
// 命令行接受而服务端不接受，是契约允许的（命令行只挡住明显不合法的写法）。
//
// 两条真实路径分别取证：命令行走自己的 config set；服务端用真实的
// eucli-deep-wiki-server 起一次，看它接受不接受这份配置。
func checkKeyRules(world *harness.World, reporter *Reporter) {
	cases := []struct {
		name string
		key  string
	}{
		{name: "恰好满足服务端长度下限", key: strings.Repeat("a", contract.ServerKeyMinLength)},
		{name: "比服务端下限短一个字符", key: strings.Repeat("a", contract.ServerKeyMinLength-1)},
		{name: "含空白字符", key: "key with space 0123456789"},
		{name: "空 key", key: ""},
	}

	for _, testCase := range cases {
		clientAccepted := world.Command.Run([]string{"config", "set", "key", testCase.key}).Exit == contract.ExitOK

		content, err := world.CandidateConfig(testCase.key)
		if err != nil {
			reporter.Mismatch(contract.Both, "现场：候选服务端配置", "能拼出来", err.Error())
			continue
		}
		serverAccepted, probeErr := world.AcceptsConfig(slug(testCase.name), content)
		if probeErr != nil {
			reporter.Mismatch(contract.Both, "现场：用真实服务端判断配置", "能得到结论", probeErr.Error())
			continue
		}

		// 唯一的硬约束：服务端接受的，命令行必须接受。
		// 反过来不要求：契约说命令行只做基本合法性，比服务端更松。
		if serverAccepted && !clientAccepted {
			reporter.Mismatch(contract.Client, "钥匙（"+testCase.name+"）被命令行拒绝",
				"命令行也接受（服务端已接受）", "命令行拒绝")
		}
	}

	for _, address := range []string{"http://127.0.0.1:9997", "https://wiki.example.com"} {
		outcome := world.Command.Run([]string{"config", "set", "server", address})
		reporter.Equal(contract.Client, "命令行对服务地址的校验（明文与加密都照用）"+address, contract.ExitOK, outcome.Exit)
	}
}

// requestsFor 挑出一次命令行调用里打到某个接口的请求。
func requestsFor(requests []harness.Request, endpoint contract.Endpoint) []harness.Request {
	pattern := endpoint.PathPattern()
	matched := make([]harness.Request, 0, len(requests))
	for _, request := range requests {
		if request.Method == endpoint.Method && pattern.MatchString(request.Path) {
			matched = append(matched, request)
		}
	}
	return matched
}

// describeRequests 把一次调用的请求压成一行，放进失败说明。
func describeRequests(requests []harness.Request) string {
	if len(requests) == 0 {
		return "一个都没有"
	}
	parts := make([]string, 0, len(requests))
	for index, request := range requests {
		if index == 5 {
			parts = append(parts, "…")
			break
		}
		parts = append(parts, request.Describe())
	}
	return strings.Join(parts, "；")
}

// worldKind 把契约里的世界名换成现场种类。
func worldKind(name string) harness.Kind {
	kind, ok := harness.Kinds[name]
	if !ok {
		panic("guard: unknown world " + name)
	}
	return kind
}

// slug 把命令形态变成稳定的用例标识片段。
func slug(value string) string {
	builder := strings.Builder{}
	builder.Grow(len(value))
	lastDash := false
	for _, character := range strings.ToLower(value) {
		switch {
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			builder.WriteRune(character)
			lastDash = false
		case character == '.':
			builder.WriteRune('.')
			lastDash = false
		default:
			if !lastDash && builder.Len() > 0 {
				builder.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

// jsonObject 把一次命令行的 JSON 输出解析成对象。
func jsonObject(outcome harness.Outcome) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(outcome.Stdout), &payload); err != nil {
		return nil, fmt.Errorf("输出不是 JSON 对象: %v；原文=%s", err, trimForMessage(outcome.Stdout))
	}
	return payload, nil
}

// trimForMessage 截短一段文本，便于放进断言消息。
func trimForMessage(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) > 300 {
		return trimmed[:300] + "…"
	}
	return trimmed
}
