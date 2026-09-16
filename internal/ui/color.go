package ui

import (
	"io"
	"os"
)

// ANSI 转义序列。
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
)

// ColorsEnabled 判断是否应该输出颜色。
//
// 规则：
//   - 设置 NO_COLOR(非空)时始终关闭，遵循 https://no-color.org
//   - 设置 FORCE_COLOR 或 CLICOLOR_FORCE=1 时始终开启(便于重定向到文件后查看)
//   - 否则仅在输出为终端时开启
func ColorsEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" || os.Getenv("CLICOLOR_FORCE") == "1" {
		return true
	}
	return IsTerminal(w)
}

// Style 负责给终端文本上色。禁用时所有方法原样返回输入。
type Style struct {
	enabled bool
}

// NewStyle 根据输出目标与环境变量创建一个 Style。
func NewStyle(w io.Writer) *Style {
	return &Style{enabled: ColorsEnabled(w)}
}

// Enabled 报告颜色是否启用。
func (s *Style) Enabled() bool { return s != nil && s.enabled }

// paint 用给定序列包裹文本；禁用或文本为空时原样返回。
func (s *Style) paint(code, text string) string {
	if !s.Enabled() || text == "" {
		return text
	}
	return code + text + ansiReset
}

// Bold 用于强调标签。
func (s *Style) Bold(text string) string { return s.paint(ansiBold, text) }

// Dim 用于次要信息，例如 Token 汇总与诊断输出。
func (s *Style) Dim(text string) string { return s.paint(ansiDim, text) }

// Red 用于错误信息。
func (s *Style) Red(text string) string { return s.paint(ansiRed, text) }

// Green 用于成功提示。
func (s *Style) Green(text string) string { return s.paint(ansiGreen, text) }

// Yellow 用于交互提示。
func (s *Style) Yellow(text string) string { return s.paint(ansiYellow, text) }

// Cyan 用于生成的命令。
func (s *Style) Cyan(text string) string { return s.paint(ansiCyan, text) }

// Label 渲染 "Label: 内容" 形式的一行，标签加粗。
func (s *Style) Label(label, text string) string {
	return s.Bold(label) + " " + text
}

// ErrorLine 渲染错误行：标签加粗、整体标红。
func (s *Style) ErrorLine(label, text string) string {
	if !s.Enabled() {
		return label + " " + text
	}
	return ansiBold + ansiRed + label + ansiReset + " " + s.Red(text)
}
