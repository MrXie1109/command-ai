package executor

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
	"time"
)

// skipUnlessUnix 让平台相关的断言只在 Unix 上运行。
func skipUnlessUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("该断言依赖 Unix shell")
	}
}

func TestRunCapturesStdout(t *testing.T) {
	skipUnlessUnix(t)
	res, err := New().Run("echo hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(res.Stdout) != "hello" {
		t.Errorf("stdout = %q", res.Stdout)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
}

func TestRunCapturesStderr(t *testing.T) {
	skipUnlessUnix(t)
	res, err := New().Run("echo oops 1>&2")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(res.Stderr) != "oops" {
		t.Errorf("stderr = %q", res.Stderr)
	}
}

func TestRunNonZeroExitCodeIsNotAnError(t *testing.T) {
	skipUnlessUnix(t)
	res, err := New().Run("exit 3")
	if err != nil {
		t.Fatalf("非 0 退出码不应返回 error: %v", err)
	}
	if res.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", res.ExitCode)
	}
}

func TestRunEmptyCommand(t *testing.T) {
	if _, err := New().Run("   "); err == nil {
		t.Error("空命令应报错")
	}
}

func TestRunTeesToWriters(t *testing.T) {
	skipUnlessUnix(t)
	var out bytes.Buffer
	e := New()
	e.Stdout = &out
	res, err := e.Run("echo tee")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "tee") {
		t.Errorf("写入 Stdout 的实时输出缺失: %q", out.String())
	}
	if !strings.Contains(res.Stdout, "tee") {
		t.Errorf("捕获的 stdout 缺失: %q", res.Stdout)
	}
}

func TestRunTimeout(t *testing.T) {
	skipUnlessUnix(t)
	e := New()
	e.Timeout = 200 * time.Millisecond
	start := time.Now()
	_, err := e.Run("sleep 5")
	if err == nil {
		t.Fatal("超时应返回错误")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("超时未生效，耗时 %v", elapsed)
	}
}

func TestRunDoesNotInheritStdin(t *testing.T) {
	skipUnlessUnix(t)
	// 从空 stdin 读取应立即得到 EOF 而不是挂起。
	done := make(chan struct{})
	go func() {
		defer close(done)
		New().Run("cat")
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("命令因等待 stdin 而挂起")
	}
}

func TestCombined(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
		stderr string
		want   string
	}{
		{"both empty", "", "", ""},
		{"stdout only", "a", "", "a"},
		{"stderr only", "", "b", "b"},
		{"both", "a", "b", "ab"},
	}
	for _, c := range cases {
		r := &Result{Stdout: c.stdout, Stderr: c.stderr}
		if got := r.Combined(); got != c.want {
			t.Errorf("%s: Combined() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestShellCommandPerPlatform(t *testing.T) {
	name, args := shellCommand("true")
	if name == "" {
		t.Fatal("解释器为空")
	}
	if len(args) == 0 {
		t.Fatal("缺少参数")
	}
	if runtime.GOOS == "windows" {
		if args[0] != "/C" {
			t.Errorf("Windows 应使用 /C, got %v", args)
		}
		return
	}
	if args[0] != "-c" {
		t.Errorf("Unix 应使用 -c, got %v", args)
	}
}
