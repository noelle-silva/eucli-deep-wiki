package params

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalidLineRange 表示行区间写法不合法。
var ErrInvalidLineRange = errors.New("invalid line range")

// LineRange 描述一段行区间，两端都是 1 基且包含端点。
//
// 零值表示「未限定」：Start 为 0 表示从第一行起，End 为 0 表示到末行为止。
// 服务端有它自己的一份实现负责真正切分内容；这里这份只负责两件事：
// 发请求前判断写法是否合法，以及把写法归一后回写进查询串。
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

	first, last, ranged := strings.Cut(trimmed, "-")
	if !ranged {
		line, err := parseLineNumber(trimmed)
		if err != nil {
			return LineRange{}, err
		}
		return LineRange{Start: line, End: line}, nil
	}

	start, err := parseOptionalLineNumber(first)
	if err != nil {
		return LineRange{}, err
	}
	end, err := parseOptionalLineNumber(last)
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
