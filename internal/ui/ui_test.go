package ui

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestAskReturnsEachAction(t *testing.T) {
	cases := map[string]Action{
		"y\n":          ActionYes,
		"Y\n":          ActionYes,
		"yes\n":        ActionYes,
		"n\n":          ActionNo,
		"N\n":          ActionNo,
		"no\n":         ActionNo,
		"\n":           ActionNo, // 空输入默认为 n
		"e\n":          ActionExplain,
		"E\n":          ActionExplain,
		"explain\n":    ActionExplain,
		"r\n":          ActionRegen,
		"R\n":          ActionRegen,
		"regenerate\n": ActionRegen,
		"  y  \n":      ActionYes,
	}
	for in, want := range cases {
		var out bytes.Buffer
		p := NewPrompter(strings.NewReader(in), &out)
		got, err := p.Ask()
		if err != nil {
			t.Errorf("输入 %q 报错: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("输入 %q -> %q, want %q", in, got, want)
		}
		if !strings.Contains(out.String(), "Allow[y/N/e/r]") {
			t.Errorf("输入 %q 时未显示提示: %q", in, out.String())
		}
	}
}

func TestAskRepromptsOnInvalidInput(t *testing.T) {
	var out bytes.Buffer
	p := NewPrompter(strings.NewReader("x\ny\n"), &out)
	got, err := p.Ask()
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if got != ActionYes {
		t.Errorf("非法输入后应继续提问, got %q", got)
	}
	if n := strings.Count(out.String(), "Allow[y/N/e/r]"); n != 2 {
		t.Errorf("应提示 2 次, got %d: %q", n, out.String())
	}
}

func TestAskOnEOFDefaultsToNo(t *testing.T) {
	var out bytes.Buffer
	p := NewPrompter(strings.NewReader(""), &out)
	got, err := p.Ask()
	if err != nil {
		t.Fatalf("EOF 不应报错: %v", err)
	}
	if got != ActionNo {
		t.Errorf("EOF 应安全地按取消处理, got %q", got)
	}
}

func TestAskFeedback(t *testing.T) {
	var out bytes.Buffer
	p := NewPrompter(strings.NewReader("不要太危险\n"), &out)
	fb, err := p.AskFeedback()
	if err != nil {
		t.Fatalf("AskFeedback: %v", err)
	}
	if fb != "不要太危险" {
		t.Errorf("feedback = %q", fb)
	}

	// 直接回车表示无反馈。
	p2 := NewPrompter(strings.NewReader("\n"), &bytes.Buffer{})
	fb2, err := p2.AskFeedback()
	if err != nil {
		t.Fatalf("AskFeedback: %v", err)
	}
	if fb2 != "" {
		t.Errorf("空反馈应为空字符串, got %q", fb2)
	}
}

func TestPrompterSharesBufferedInput(t *testing.T) {
	// 同一个 Prompter 连续读取两行，不应因缓冲而丢数据。
	p := NewPrompter(strings.NewReader("e\ny\n"), &bytes.Buffer{})
	a1, _ := p.Ask()
	a2, _ := p.Ask()
	if a1 != ActionExplain || a2 != ActionYes {
		t.Errorf("连续读取得到 %q, %q", a1, a2)
	}
}

func TestSpinnerStartStopIsIdempotentSafe(t *testing.T) {
	var out bytes.Buffer
	s := NewSpinner(&out, "Thinking")
	s.Start()
	time.Sleep(30 * time.Millisecond)
	s.Stop()
	s.Stop() // 重复 Stop 不应 panic
	// 非终端下不输出控制字符。
	if strings.Contains(out.String(), "\r") {
		t.Errorf("非终端输出不应包含回车控制符: %q", out.String())
	}
}

func TestIsTerminalOnBuffer(t *testing.T) {
	if IsTerminal(&bytes.Buffer{}) {
		t.Error("bytes.Buffer 不应被判定为终端")
	}
}

func TestQuotedKeepsUnicode(t *testing.T) {
	// 使用自实现的引号包裹，避免 %q 把中文转义成 \uXXXX。
	if got := quoted("你好"); got != "\"你好\"" {
		t.Errorf("quoted = %q", got)
	}
}

// 确保 bufio 被使用（Prompter 内部缓冲），防止将来误删。
var _ = bufio.NewReader
