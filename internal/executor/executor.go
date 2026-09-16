// Package executor 负责执行 LLM 生成的命令并捕获输出。
//
// 设计说明：本工具不替代 shell，生成的命令要么直接交给平台的命令解释器
// （Unix 下为 sh -c，Windows 下为 cmd /C），要么以受控方式执行。
// 出于“单条命令、无管道/重定向”的约定，这里始终通过解释器执行，
// 以保证 PATH 查找、引号与转义行为符合用户预期。
package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/command-ai/command-ai/internal/i18n"
)

// DefaultTimeout 是命令执行的默认超时时间，0 表示不限制。
const DefaultTimeout = 0

// Result 是一次命令执行的结果。
type Result struct {
	Command  string
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Executor 执行命令。
type Executor struct {
	// Dir 是工作目录，为空表示继承当前进程。
	Dir string
	// Stdout / Stderr 若非空，执行时会同时把输出写入这两个流（用于实时展示）。
	Stdout io.Writer
	Stderr io.Writer
	// Timeout 为 0 表示不限制。
	Timeout time.Duration
}

// New 返回一个默认的执行器。
func New() *Executor {
	return &Executor{Timeout: DefaultTimeout}
}

// shellCommand 根据当前平台返回执行单条命令的解释器与参数。
func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd.exe"
		}
		return comspec, []string{"/C", command}
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return sh, []string{"-c", command}
}

// Run 执行命令并返回结果。
//
// 命令本身正常结束（包括非 0 退出码）时 error 为 nil，退出码记录在 Result 中；
// 仅在无法启动命令或超时的情况下返回 error。
func (e *Executor) Run(command string) (*Result, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, errors.New(i18n.T("executor.empty_command"))
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if e.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, e.Timeout)
		defer cancel()
	}

	name, args := shellCommand(command)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = e.Dir
	cmd.Stdin = nil // 不把终端交给子进程，避免交互式命令挂起

	var stdout, stderr bytes.Buffer
	if e.Stdout != nil {
		cmd.Stdout = io.MultiWriter(&stdout, e.Stdout)
	} else {
		cmd.Stdout = &stdout
	}
	if e.Stderr != nil {
		cmd.Stderr = io.MultiWriter(&stderr, e.Stderr)
	} else {
		cmd.Stderr = &stderr
	}

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	res := &Result{
		Command:  command,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: dur,
	}

	if err != nil {
		// 超时优先级最高：CommandContext 杀进程后返回的同样是 ExitError，
		// 若不先判断 ctx，超时会被误报成普通的非 0 退出码。
		if ctx.Err() == context.DeadlineExceeded {
			return res, fmt.Errorf(i18n.T("executor.timeout"), e.Timeout)
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

// Combined 返回合并后的输出，供历史记录使用。
func (r *Result) Combined() string {
	switch {
	case r.Stdout == "" && r.Stderr == "":
		return ""
	case r.Stderr == "":
		return r.Stdout
	case r.Stdout == "":
		return r.Stderr
	default:
		return r.Stdout + r.Stderr
	}
}
