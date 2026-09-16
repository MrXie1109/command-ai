package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/command-ai/command-ai/internal/i18n"
)

func TestNewDefaults(t *testing.T) {
	c := New()
	if c.BaseURL != DefaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, DefaultBaseURL)
	}
	if c.Model != DefaultModel {
		t.Errorf("Model = %q, want %q", c.Model, DefaultModel)
	}
	if c.Verbose {
		t.Error("Verbose 默认为 true，期望 false")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.yaml")

	c := New()
	c.APIKey = "sk-test-1234567890"
	c.Verbose = true
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.APIKey != c.APIKey {
		t.Errorf("APIKey = %q, want %q", got.APIKey, c.APIKey)
	}
	if got.BaseURL != c.BaseURL {
		t.Errorf("BaseURL = %q, want %q", got.BaseURL, c.BaseURL)
	}
	if got.Model != c.Model {
		t.Errorf("Model = %q, want %q", got.Model, c.Model)
	}
	if got.Verbose != c.Verbose {
		t.Errorf("Verbose = %v, want %v", got.Verbose, c.Verbose)
	}
}

func TestSaveFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 使用 ACL，跳过 Unix 权限位断言")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := New().Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("文件权限 = %o, want 600", perm)
	}
}

func TestSaveOverwriteKeepsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 跳过")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := New().Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// 先放宽权限，再保存一次，应被重新收紧到 0600。
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	if err := New().Save(path); err != nil {
		t.Fatalf("Save 第二次: %v", err)
	}
	fi, _ := os.Stat(path)
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("覆盖后权限 = %o, want 600", perm)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("Load 不存在的文件不应报错: %v", err)
	}
	if cfg.BaseURL != DefaultBaseURL || cfg.Model != DefaultModel {
		t.Errorf("应返回默认配置, got %+v", cfg)
	}
}

func TestLoadNormalizesEmptyFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// 只有 api_key 的配置文件，缺失字段应补默认值。
	if err := os.WriteFile(path, []byte("api_key: sk-abc\nbase_url: \"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BaseURL != DefaultBaseURL {
		t.Errorf("空的 base_url 应补默认值, got %q", cfg.BaseURL)
	}
	if cfg.Model != DefaultModel {
		t.Errorf("缺失的 model 应补默认值, got %q", cfg.Model)
	}
	if cfg.APIKey != "sk-abc" {
		t.Errorf("APIKey = %q", cfg.APIKey)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("api_key: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("非法 YAML 应返回错误")
	}
}

func TestMaskKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", i18n.T("config.key_unset")},
		{"abc", "****"},
		{"abcd", "****"},
		{"sk-1234567890", "sk-1****"},
	}
	for _, c := range cases {
		if got := MaskKey(c.in); got != c.want {
			t.Errorf("MaskKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// 脱敏结果绝不能包含完整密钥。
	key := "sk-abcdefghijklmnop"
	if got := MaskKey(key); len(got) >= len(key) {
		t.Errorf("脱敏结果 %q 不应长于原文", got)
	}
}

func TestDefaultPathEnvOverride(t *testing.T) {
	t.Setenv("COMMAND_AI_CONFIG", "/tmp/custom-config.yaml")
	if got := DefaultPath(); got != "/tmp/custom-config.yaml" {
		t.Errorf("DefaultPath() = %q", got)
	}
}

func TestDefaultPathXDG(t *testing.T) {
	t.Setenv("COMMAND_AI_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	want := filepath.Join("/tmp/xdg", "command-ai", "config.yaml")
	if got := DefaultPath(); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}
