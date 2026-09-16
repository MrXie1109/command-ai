package prompt

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPathForIsNextToConfig(t *testing.T) {
	got := PathFor("/home/x/.config/command-ai/config.yaml")
	want := filepath.Join("/home/x/.config/command-ai", "template.txt")
	if got != want {
		t.Errorf("PathFor = %q, want %q", got, want)
	}
}

func TestRenderSubstitutesAllPlaceholders(t *testing.T) {
	tpl := "os={{os}} arch={{arch}} shell={{shell}} req={{request}} prev={{previous}} fb={{feedback}}"
	d := Data{
		OS: "linux", Arch: "amd64", Shell: "sh",
		Request: "列出文件", Previous: "ls ~", Feedback: "用长格式",
	}
	got := Render(tpl, d)
	want := "os=linux arch=amd64 shell=sh req=列出文件 prev=ls ~ fb=用长格式"
	if got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

func TestRenderAllowsWhitespaceInBraces(t *testing.T) {
	got := Render("a={{ os }} b={{  arch  }}", Data{OS: "linux", Arch: "arm64"})
	if got != "a=linux b=arm64" {
		t.Errorf("Render = %q", got)
	}
}

func TestRenderLeavesUnknownPlaceholders(t *testing.T) {
	// 未知占位符原样保留，方便用户发现拼写错误，而不是静默变成空字符串。
	got := Render("x={{nope}} y={{os}}", Data{OS: "linux"})
	if got != "x={{nope}} y=linux" {
		t.Errorf("Render = %q", got)
	}
}

func TestRenderEmptyValues(t *testing.T) {
	// 首轮没有 previous/feedback，应替换为空串而不是留下占位符。
	got := Render("[{{previous}}][{{feedback}}]", Data{})
	if got != "[][]" {
		t.Errorf("Render = %q", got)
	}
}

func TestHasPlaceholder(t *testing.T) {
	tpl := "hello {{os}} and {{ request }}"
	if !HasPlaceholder(tpl, "os") || !HasPlaceholder(tpl, "request") {
		t.Error("应识别出 os 与 request")
	}
	if HasPlaceholder(tpl, "feedback") {
		t.Error("不应识别出 feedback")
	}
}

func TestDefaultTemplateRendersForEachPlatform(t *testing.T) {
	// 默认模板必须能渲染出真实环境信息，且不含未替换的内置占位符。
	got := Render(Default(), Data{OS: "linux", Arch: "amd64", Shell: "sh"})
	for _, want := range []string{"linux/amd64", "user shell: sh"} {
		if !strings.Contains(got, want) {
			t.Errorf("默认模板缺少 %q", want)
		}
	}
	for _, name := range []string{"os", "arch", "shell"} {
		if strings.Contains(got, "{{"+name+"}}") {
			t.Errorf("默认模板中 %q 未被替换", name)
		}
	}
}

func TestDefaultKeepsProtocolContract(t *testing.T) {
	// 默认模板是提示词的唯一来源，必须仍然包含协议与关键约束。
	d := Default()
	for _, want := range []string{
		"Command: <a single executable command>",
		"Error: <one short sentence",
		"NOT running inside a shell",
		"$HOME",
		"SAME LANGUAGE",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("默认模板缺少约束 %q", want)
		}
	}
}

func TestLoadFallsBackWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "template.txt")
	content, custom, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if custom {
		t.Error("文件不存在时不应报告为自定义")
	}
	if content != Default() {
		t.Error("文件不存在时应返回内置默认模板")
	}
}

func TestLoadTreatsBlankFileAsDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "template.txt")
	for _, blank := range []string{"", "   \n\n\t\n"} {
		if err := os.WriteFile(path, []byte(blank), 0o600); err != nil {
			t.Fatal(err)
		}
		content, custom, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if custom {
			t.Errorf("空白内容(%q)不应报告为自定义", blank)
		}
		if content != Default() {
			t.Errorf("空白内容(%q)应回退到默认模板", blank)
		}
	}
}

func TestLoadReturnsCustomContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "template.txt")
	if err := os.WriteFile(path, []byte("my custom prompt {{os}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, custom, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !custom {
		t.Error("自定义内容应报告 custom=true")
	}
	if content != "my custom prompt {{os}}" {
		t.Errorf("content = %q", content)
	}
}

func TestEnsureCreatesDefaultOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "template.txt")

	created, err := Ensure(path)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !created {
		t.Error("首次调用应创建文件")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取生成的文件: %v", err)
	}
	if string(data) != Default() {
		t.Error("生成的内容应等于内置默认模板")
	}

	// 已有文件时不得覆盖用户修改。
	if err := os.WriteFile(path, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err = Ensure(path)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if created {
		t.Error("文件已存在时不应报告 created")
	}
	data, _ = os.ReadFile(path)
	if string(data) != "mine" {
		t.Errorf("不应覆盖用户内容, got %q", data)
	}
}

func TestSaveSetsFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 使用 ACL")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "template.txt")
	if err := Save(path, "x"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("模板文件权限 = %o, want 600", perm)
	}
}
