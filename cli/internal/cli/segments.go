package cli

import (
	"fmt"
	"io"
)

// segments 是「一次调用里按命令行顺序排好的若干段」以及它们各自的结果。
//
// 多段是 get 的多路径与 tree 的多目录共同的需要：并发取、按命令行给的顺序输出、
// 单段失败不拖垮整次调用。这套编排只写这一份，两处共用，两个命令的观感因此一致。
type segments[T any] struct {
	labels   []string
	outcomes []outcome[T]
}

// fetchSegments 并发取出每一段，结果按 labels 的顺序落位，与谁先返回无关。
//
// 段数就是命令行给的位置参数个数，天然很小，因此不设并发上限——与 get 一直以来的
// 做法相同：这些请求彼此独立，且都是用户在等的一次调用。
func fetchSegments[T any](labels []string, task func(label string) (T, error)) segments[T] {
	outcomes := gather(len(labels), 0, func(index int) (T, error) {
		return task(labels[index])
	})
	return segments[T]{labels: labels, outcomes: outcomes}
}

// name 是这一段在输出里怎么被称呼：空标签代表仓库根。
func (s segments[T]) name(index int) string {
	if s.labels[index] == "" {
		return "仓库根"
	}
	return s.labels[index]
}

// anyValue 表示至少有一段拿到了结果：只对整次调用成立的说明（例如这次排除了哪些
// 目录名）只在有内容可看时才说，否则只会给失败信息添噪。
func (s segments[T]) anyValue() bool {
	for _, result := range s.outcomes {
		if result.err == nil {
			return true
		}
	}
	return false
}

// firstError 是第一个失败的原因，用于定退出码；全部成功时为 nil。
func (s segments[T]) firstError() error {
	for _, result := range s.outcomes {
		if result.err != nil {
			return result.err
		}
	}
	return nil
}

// failures 把每一段的失败写成「标签: 原因」，顺序与命令行一致。
func (s segments[T]) failures() []string {
	failures := make([]string, 0, len(s.labels))
	for index, result := range s.outcomes {
		if result.err == nil {
			continue
		}
		failures = append(failures, fmt.Sprintf("%s: %v", s.name(index), result.err))
	}
	return failures
}

// renderSegments 走完一批人读输出：成功的段落各交给自己渲染，失败的在 stderr 上
// 一行说清；多段时每段前面写一行 `==> 标签 <==`，与 head 的多文件输出同形。
//
// 单段不写分隔行：只有一个来源时，stdout 里不需要任何额外标记。
// 退出码按第一个失败算——一次失败不影响其它段照常输出。
func renderSegments[T any](
	stdout io.Writer,
	stderr io.Writer,
	batch segments[T],
	render func(out io.Writer, name string, value T, withLabel bool),
) int {
	withLabel := len(batch.labels) > 1
	code := exitOK

	for index, result := range batch.outcomes {
		name := batch.name(index)
		if result.err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, result.err)
			if code == exitOK {
				code = exitCodeFor(result.err)
			}
			continue
		}
		if withLabel {
			fmt.Fprintf(stdout, "==> %s <==\n", name)
		}
		render(stdout, name, result.value, withLabel)
	}
	return code
}

// writeSegmentsJSON 输出一批多段结果的结构化形态：
//
//   - 只有一段时就是那一段的对象；
//   - 多段全部成功时是对象数组；
//   - 有失败时包成 {<field>: [...], failures: [...]}，失败的事实不会因为
//     「另外几段拿到了」而消失；
//   - 一段都没拿到时走统一的错误输出，与其它命令同形。
func writeSegmentsJSON[T any](stdout io.Writer, stderr io.Writer, field string, batch segments[T]) int {
	values := make([]T, 0, len(batch.outcomes))
	for _, result := range batch.outcomes {
		if result.err == nil {
			values = append(values, result.value)
		}
	}

	failures := batch.failures()
	if firstErr := batch.firstError(); firstErr != nil && len(values) == 0 {
		reportError(stderr, firstErr, true)
		return exitCodeFor(firstErr)
	}

	var payload any = values
	switch {
	case len(batch.labels) == 1:
		payload = values[0]
	case len(failures) > 0:
		payload = map[string]any{field: values, "failures": failures}
	}
	if code := writeJSON(stdout, stderr, payload); code != exitOK {
		return code
	}
	return exitCodeFor(batch.firstError())
}
