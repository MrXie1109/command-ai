package ui

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/command-ai/command-ai/internal/i18n"
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
		got, err := p.Ask(true)
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
	got, err := p.Ask(true)
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
	got, err := p.Ask(true)
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
	a1, _ := p.Ask(true)
	a2, _ := p.Ask(true)
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

func TestInvalidInputKeepsUnicodeReadable(t *testing.T) {
	// 非法输入回显时不应把非 ASCII 字符转义成 \uXXXX。
	var out bytes.Buffer
	p := NewPrompter(strings.NewReader("中文\nn\n"), &out)
	if _, err := p.Ask(true); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if !strings.Contains(out.String(), "中文") {
		t.Errorf("非法输入应原样回显, got %q", out.String())
	}
	if strings.Contains(out.String(), `\u`) {
		t.Errorf("不应出现 \\uXXXX 转义, got %q", out.String())
	}
}

// 确保 bufio 被使用(Prompter 内部缓冲)，防止将来误删。
var _ = bufio.NewReader

// TestAskWithoutRunRejectsYes 覆盖“模型拒答”时的提示。
func TestAskWithoutRunRejectsYes(t *testing.T) {
	var out bytes.Buffer
	// 先输入 y(应被拒绝)，再输入 n。
	p := NewPrompter(strings.NewReader("y\nn\n"), &out)
	got, err := p.Ask(false)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if got != ActionNo {
		t.Errorf("没有可执行内容时 y 应被拒绝, got %q", got)
	}
	if n := strings.Count(out.String(), "Allow[N/e/r]"); n != 2 {
		t.Errorf("应提示 2 次 Allow[N/e/r], got %d: %q", n, out.String())
	}
	if strings.Contains(out.String(), "Allow[y/N/e/r]") {
		t.Errorf("拒答时不应出现含 y 的提示: %q", out.String())
	}
	if !strings.Contains(out.String(), i18n.T("ui.no_command_hint")) {
		t.Errorf("应提示没有可执行命令, got %q", out.String())
	}
}

// TestAskWithoutRunStillAllowsExplainAndRegen 拒答时 e / r 仍可用。
func TestAskWithoutRunStillAllowsExplainAndRegen(t *testing.T) {
	for in, want := range map[string]Action{
		"e\n": ActionExplain,
		"r\n": ActionRegen,
		"n\n": ActionNo,
		"\n":  ActionNo,
	} {
		p := NewPrompter(strings.NewReader(in), &bytes.Buffer{})
		got, err := p.Ask(false)
		if err != nil {
			t.Fatalf("Ask(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("Ask(false) 输入 %q = %q, want %q", in, got, want)
		}
	}
}

// TestNoColorWhenNotTerminal 输出到缓冲区时不应产生 ANSI 转义。
func TestNoColorWhenNotTerminal(t *testing.T) {
	var out bytes.Buffer
	s := NewStyle(&out)
	if s.Enabled() {
		t.Fatal("非终端不应启用颜色")
	}
	for _, got := range []string{s.Red("x"), s.Cyan("x"), s.Dim("x"), s.Bold("x"), s.Yellow("x")} {
		if got != "x" {
			t.Errorf("禁用颜色时应原样返回, got %q", got)
		}
	}
	if got := s.Label("Command:", "ls"); got != "Command: ls" {
		t.Errorf("Label = %q", got)
	}
	if got := s.ErrorLine("Error:", "boom"); got != "Error: boom" {
		t.Errorf("ErrorLine = %q", got)
	}
}

// TestColorsEnabledEnv 覆盖 NO_COLOR 与 FORCE_COLOR。
func TestColorsEnabledEnv(t *testing.T) {
	var buf bytes.Buffer

	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	if ColorsEnabled(&buf) {
		t.Error("非终端且无强制时不应启用")
	}

	t.Setenv("FORCE_COLOR", "1")
	if !ColorsEnabled(&buf) {
		t.Error("FORCE_COLOR 应强制启用")
	}

	// NO_COLOR 优先级最高。
	t.Setenv("NO_COLOR", "1")
	if ColorsEnabled(&buf) {
		t.Error("NO_COLOR 应压过 FORCE_COLOR")
	}
}

// TestStyleProducesAnsiWhenForced 强制启用时确实写入 ANSI 序列。
func TestStyleProducesAnsiWhenForced(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	var buf bytes.Buffer
	s := NewStyle(&buf)
	if !s.Enabled() {
		t.Fatal("FORCE_COLOR 时应启用")
	}
	if got := s.Red("boom"); !strings.Contains(got, "\033[31m") || !strings.HasSuffix(got, "\033[0m") {
		t.Errorf("Red 应包裹 ANSI 序列, got %q", got)
	}
	if got := s.Cyan("ls"); !strings.Contains(got, "\033[36m") {
		t.Errorf("Cyan 应包含青色序列, got %q", got)
	}
}
