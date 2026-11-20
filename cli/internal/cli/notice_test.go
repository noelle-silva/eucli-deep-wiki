package cli

import (
	"bytes"
	"strings"
	"testing"
)

// 长期规矩的播报口径：有人在读屏幕时说一次，被管道接走时一个字都不说。
//
// 一线反馈的原话是「连着跑四个 tree，屏幕上就刷了四遍这段 150 字的清单，还挤在
// 标题与正文之间」。所以这里两件事都要守住：批量调用不许重复，非交互不许出声。
func TestNoticeLogSpeaksOncePerProcess(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	notices := &noticeLog{}
	message := "（默认不列出依赖与缓存目录：node_modules；加 --all 改为一并列出）"

	for range 4 {
		notices.noteOnce(&out, true, message)
	}

	if got := strings.Count(out.String(), message); got != 1 {
		t.Fatalf("同一段说明应只出现一次，实际 %d 次：%q", got, out.String())
	}
}

func TestNoticeLogStaysSilentWhenNotInteractive(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	notices := &noticeLog{}

	notices.noteOnce(&out, false, "（任何说明）")

	if out.Len() != 0 {
		t.Fatalf("非交互时不该出声，实际 %q", out.String())
	}
}

func TestNoticeLogKeepsDistinctMessages(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	notices := &noticeLog{}

	notices.noteOnce(&out, true, "甲")
	notices.noteOnce(&out, true, "乙")
	notices.noteOnce(&out, true, "甲")

	if got, want := out.String(), "甲\n乙\n"; got != want {
		t.Fatalf("out = %q, want %q", got, want)
	}
}

func TestNoticeLogIgnoresEmptyMessage(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	notices := &noticeLog{}

	notices.noteOnce(&out, true, "")

	if out.Len() != 0 {
		t.Fatalf("空说明不该占一行，实际 %q", out.String())
	}
}

// 数据出口是缓冲区（管道、文件、脚本）时不算终端；这决定了说明说不说。
func TestIsTerminalTreatsBufferAsPipe(t *testing.T) {
	t.Parallel()

	if isTerminal(&bytes.Buffer{}) {
		t.Fatal("内存缓冲区不该被当成终端")
	}
}
