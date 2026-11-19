package cli

import (
	"errors"
	"strings"
	"testing"

	"eucli-dw/internal/apiclient"
)

// TestFailureSpecsCoverContract 守住契约第四节：每个失败标识都要有自己的说明与退出码。
func TestFailureSpecsCoverContract(t *testing.T) {
	t.Parallel()

	contract := map[string]int{
		"duplicate_name":     exitConflict,
		"sync_busy":          exitConflict,
		"cache_busy":         exitConflict,
		"invalid_url":        exitUsage,
		"invalid_name":       exitUsage,
		"invalid_history":    exitUsage,
		"invalid_branch":     exitUsage,
		"invalid_body":       exitUsage,
		"confirm_required":   exitUsage,
		"detached_head":      exitUsage,
		"remote_unreachable": exitNetwork,
		"index_write_failed": exitFailure,
		"state_write_failed": exitFailure,
		"git_failed":         exitFailure,
		"remove_failed":      exitFailure,
	}

	for code, want := range contract {
		spec, ok := failureSpecs[code]
		if !ok {
			t.Fatalf("契约里的 %q 没有落地", code)
		}
		if spec.exit != want {
			t.Fatalf("%s 的退出码 = %d, want %d", code, spec.exit, want)
		}
		if spec.message == "" {
			t.Fatalf("%s 没有说明", code)
		}
		if got := exitCodeFor(&apiclient.APIError{StatusCode: 500, Code: code}); got != want {
			t.Fatalf("exitCodeFor(%s) = %d, want %d", code, got, want)
		}
	}
}

// TestExitCodeFallsBackToStatus 说明「契约没列的错误」仍然按状态码分类，不会全部塌成 1。
func TestExitCodeFallsBackToStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		err    error
		status int
		want   int
	}{
		{name: "未找到", status: 404, want: exitNotFound},
		{name: "鉴权", status: 401, want: exitAuth},
		{name: "冲突", status: 409, want: exitConflict},
		{name: "过大", status: 413, want: exitRejected},
		{name: "其它", status: 500, want: exitFailure},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := &apiclient.APIError{StatusCode: testCase.status, Code: "unlisted_code"}
			if got := exitCodeFor(err); got != testCase.want {
				t.Fatalf("exitCodeFor(%d) = %d, want %d", testCase.status, got, testCase.want)
			}
		})
	}

	if got := exitCodeFor(nil); got != exitOK {
		t.Fatalf("exitCodeFor(nil) = %d", got)
	}
	if got := exitCodeFor(errors.New("boom")); got != exitFailure {
		t.Fatalf("普通错误 = %d, want %d", got, exitFailure)
	}
}

// TestDescribeFailureKeepsUnknownErrors 说明没有契约说明的错误不会被改写，原文照旧。
func TestDescribeFailureKeepsUnknownErrors(t *testing.T) {
	t.Parallel()

	unknown := &apiclient.APIError{StatusCode: 400, Code: "some_other_code", Message: "服务端说明"}
	if got := describeFailure(unknown); got != unknown.Error() {
		t.Fatalf("describeFailure() = %q", got)
	}
}

// TestHelpListsEveryManagementCommand 守住帮助与实际命令一致：列出来的都要能跑。
func TestHelpListsEveryManagementCommand(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runCommand([]string{"help"})
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}

	wanted := []string{
		"repo add", "repo list", "repo rm", "repo unblock", "repo compact", "repo check",
		"cache list", "cache clean",
		"--sizes", "--drop-data", "--dry-run", "--yes", "--no-reload", "--history", "--branch",
	}
	for _, want := range wanted {
		if !strings.Contains(stdout, want) {
			t.Fatalf("帮助里缺 %q", want)
		}
	}
}

// TestGroupHelpMatchesSubcommands 说明命令组本身也有帮助，且列出全部子命令。
func TestGroupHelpMatchesSubcommands(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args []string
		want []string
	}{
		{
			args: []string{"repo"},
			want: []string{"用法: eucli-dw repo", "add", "list", "rm", "unblock", "compact", "check"},
		},
		{
			args: []string{"repo", "--help"},
			want: []string{"用法: eucli-dw repo"},
		},
		{
			args: []string{"cache"},
			want: []string{"用法: eucli-dw cache", "list", "clean"},
		},
		{
			args: []string{"cache", "-h"},
			want: []string{"用法: eucli-dw cache"},
		},
	}

	for _, testCase := range cases {
		code, stdout, stderr := runCommand(testCase.args)
		if len(testCase.args) == 1 {
			// 只给命令组名属于用法不全，帮助照打，但退出码是 2。
			if code != exitUsage {
				t.Fatalf("%v code = %d, want %d", testCase.args, code, exitUsage)
			}
		} else if code != exitOK {
			t.Fatalf("%v code = %d, want 0（stderr = %q）", testCase.args, code, stderr)
		}

		for _, want := range testCase.want {
			if !strings.Contains(stdout, want) {
				t.Fatalf("%v 的输出缺 %q: %q", testCase.args, want, stdout)
			}
		}
	}
}

// TestHelpKeepsNoLocalDeploymentWording 守住骨架阶段删掉的旧表述不许回潮：
// 命令行不再读服务端配置、不再直接操作部署目录。
func TestHelpKeepsNoLocalDeploymentWording(t *testing.T) {
	t.Parallel()

	banned := []string{"service-config", "服务配置文件", "部署目录", "数据根", "仓库名单文件", "同步状态文件"}
	if strings.Contains(usageText, "本地命令") {
		t.Fatalf("帮助里仍有「本地命令」的说法")
	}
	for _, word := range banned {
		if strings.Contains(usageText, word) {
			t.Fatalf("帮助里出现旧表述 %q", word)
		}
	}
}

// TestUnknownRepositorySubcommandExplains 说明打错子命令时给出用法，而不是静默失败。
func TestUnknownRepositorySubcommandExplains(t *testing.T) {
	t.Parallel()

	code, _, stderr := runCommand([]string{"repo", "nope"})
	if code != exitUsage {
		t.Fatalf("code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "未知 repo 子命令") || !strings.Contains(stderr, "用法") {
		t.Fatalf("stderr = %q", stderr)
	}
}
