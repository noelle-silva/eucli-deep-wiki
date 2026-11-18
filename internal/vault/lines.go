package vault

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// LineRange 描述一段行区间，两端都是 1 基且包含端点。
//
// 零值表示「未限定」：Start 为 0 表示从第一行起，End 为 0 表示到末行为止。
// 命令行与服务端共用同一份区间语法，解析与切分都只在这里实现一次。
type LineRange struct {
	Start int
	End   int
}

// IsZero 判断是否没有限定任何行。
func (r LineRange) IsZero() bool {
	return r.Start == 0 && r.End == 0
}

// String 还原成命令行写法，未限定任何行时返回空串。
func (r LineRange) String() string {
	if r.IsZero() {
		return ""
	}
	if r.Start > 0 && r.Start == r.End {
		return strconv.Itoa(r.Start)
	}

	start := ""
	if r.Start > 0 {
		start = strconv.Itoa(r.Start)
	}
	end := ""
	if r.End > 0 {
		end = strconv.Itoa(r.End)
	}
	return start + "-" + end
}

// ParseLineRange 解析行区间写法：
//
//	"362-520" 第 362 到 520 行；"362-" 第 362 行到末尾；
//	"-520"    首行到第 520 行；"362"  仅第 362 行。
func ParseLineRange(raw string) (LineRange, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return LineRange{}, nil
	}

	dash := strings.Index(trimmed, "-")
	if dash < 0 {
		line, err := parseLineNumber(trimmed)
		if err != nil {
			return LineRange{}, err
		}
		return LineRange{Start: line, End: line}, nil
	}

	start, err := parseOptionalLineNumber(trimmed[:dash])
	if err != nil {
		return LineRange{}, err
	}
	end, err := parseOptionalLineNumber(trimmed[dash+1:])
	if err != nil {
		return LineRange{}, err
	}
	if start == 0 && end == 0 {
		return LineRange{}, invalidLineRange(trimmed, "区间两端不能都为空")
	}
	if start > 0 && end > 0 && start > end {
		return LineRange{}, invalidLineRange(trimmed, "起始行不能大于结束行")
	}
	return LineRange{Start: start, End: end}, nil
}

func parseLineNumber(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, invalidLineRange(raw, "行号不能为空")
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil || value < 1 {
		return 0, invalidLineRange(raw, "行号必须是从 1 开始的整数")
	}
	return value, nil
}

func parseOptionalLineNumber(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	return parseLineNumber(raw)
}

func invalidLineRange(raw string, reason string) error {
	return fmt.Errorf("%w: %q（%s）；应写成 362-520、362-、-520 或 362", ErrInvalidLineRange, raw, reason)
}

// countLines 统计内容的行数；末尾换行不会凭空多出一行。
func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}

	count := bytes.Count(content, []byte{'\n'})
	if content[len(content)-1] != '\n' {
		count++
	}
	return count
}

// lineOffset 返回第 line 行的起始偏移；超出末行时返回内容长度。
func lineOffset(content []byte, line int) int {
	if line <= 1 {
		return 0
	}

	number := 1
	for index, value := range content {
		if value != '\n' {
			continue
		}
		number++
		if number == line {
			return index + 1
		}
	}
	return len(content)
}

// sliceLines 按行区间切出内容，返回切出的字节与首末行号（1 基，含）。
//
// 请求区间完全落在末行之后时返回空内容，首末行号如实说明停在哪里，
// 调用方据此给出「文件只有 N 行」的提示，而不是假装成功。
func sliceLines(content []byte, span LineRange) (chunk []byte, first int, last int) {
	total := countLines(content)
	if total == 0 {
		return nil, 0, 0
	}

	first = 1
	if span.Start > 0 {
		first = span.Start
	}
	last = total
	if span.End > 0 && span.End < total {
		last = span.End
	}

	if first > total {
		return nil, total + 1, total
	}
	if last < first {
		return nil, first, first - 1
	}

	start := lineOffset(content, first)
	end := lineOffset(content, last+1)
	return content[start:end], first, last
}

// clampToBytes 把内容截到最后一个完整行以内。
//
// 返回截断后的内容、新的结束行号与是否发生了截断。第一行就超过上限时
// 返回空内容与 last = first-1：宁可什么都不给，也不给半行残句。
func clampToBytes(chunk []byte, first int, limit int64, last int) ([]byte, int, bool) {
	if limit <= 0 || int64(len(chunk)) <= limit {
		return chunk, last, false
	}

	cut := bytes.LastIndexByte(chunk[:limit], '\n')
	if cut < 0 {
		return nil, first - 1, true
	}

	kept := chunk[:cut+1]
	return kept, first + countLines(kept) - 1, true
}
