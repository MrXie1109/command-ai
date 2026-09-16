// Package ui 负责终端交互：Thinking 动画、命令展示、Allow[y/N/e/r] 提示。
package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/command-ai/command-ai/internal/i18n"
)

// Spinner 是一个单行旋转动画。
type Spinner struct {
	w    io.Writer
	text string
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// NewSpinner 创建一个 spinner。text 例如 "Thinking"。
func NewSpinner(w io.Writer, text string) *Spinner {
	return &Spinner{w: w, text: text}
}

// Start 在后台开始播放动画，立即返回。
// 若输出不是终端，则不播放动画，仅在结束时打印一次提示。
func (s *Spinner) Start() {
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	interactive := IsTerminal(s.w)

	go func() {
		defer close(s.done)
		if !interactive {
			return
		}
		// 走马灯序列，参考项目书中的 "Thinking -" / "Thinking /"。
		frames := []string{"-", "\\", "|", "/"}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		fmt.Fprintf(s.w, "\r%s %s", s.text, frames[0])
		for {
			select {
			case <-s.stop:
				// 清除当前行，交由调用方继续输出。
				fmt.Fprint(s.w, "\r\033[K")
				return
			case <-ticker.C:
				i = (i + 1) % len(frames)
				fmt.Fprintf(s.w, "\r%s %s", s.text, frames[i])
			}
		}
	}()
}

// Stop 结束动画并等待后台协程退出。
func (s *Spinner) Stop() {
	if s.stop == nil {
		return
	}
	s.once.Do(func() { close(s.stop) })
	<-s.done
}

// IsTerminal 判断 w 是否为终端文件。
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// Action 是用户在 Allow 提示处的选择。
type Action string

// 用户可选的四种动作。
const (
	ActionYes     Action = "y" // 执行
	ActionNo      Action = "n" // 取消
	ActionExplain Action = "e" // 解释
	ActionRegen   Action = "r" // 重新生成
)

// Prompter 从输入流读取用户的交互选择。
type Prompter struct {
	In  io.Reader
	Out io.Writer

	reader *bufio.Reader
}

// NewPrompter 创建交互器。
func NewPrompter(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{In: in, Out: out, reader: bufio.NewReader(in)}
}

// Ask 显示 "Allow[y/N/e/r] " 并读取一个选择。
//
// 空输入默认为 n（取消）；无法识别的输入会再次提示。
// 输入流结束时返回 ActionNo，保证程序安全退出。
func (p *Prompter) Ask() (Action, error) {
	for {
		fmt.Fprint(p.Out, i18n.T("ui.allow_prompt"))
		line, err := p.reader.ReadString('\n')
		if err != nil && line == "" {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(p.Out)
				return ActionNo, nil
			}
			return ActionNo, err
		}

		switch strings.ToLower(strings.TrimSpace(line)) {
		case string(ActionYes), "yes":
			return ActionYes, nil
		case "", string(ActionNo), "no":
			return ActionNo, nil
		case string(ActionExplain), "explain":
			return ActionExplain, nil
		case string(ActionRegen), "regen", "regenerate":
			return ActionRegen, nil
		default:
			fmt.Fprintf(p.Out, i18n.T("ui.invalid_input")+"\n", i18n.Quote(strings.TrimSpace(line)))
		}
	}
}

// AskFeedback 在“重新生成”时询问可选的补充说明。
// 直接回车表示没有额外反馈。
func (p *Prompter) AskFeedback() (string, error) {
	fmt.Fprint(p.Out, i18n.T("ui.feedback_prompt"))
	line, err := p.reader.ReadString('\n')
	if err != nil && line == "" && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
