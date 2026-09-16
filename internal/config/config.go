// Package config 负责配置文件的读写。
//
// 配置文件默认为 <数据目录>/config.yaml，权限 0600(Unix)。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"

	"github.com/MrXie1109/command-ai/internal/i18n"
)

// 配置项的默认值。
const (
	DefaultBaseURL  = "https://api.deepseek.com"
	DefaultModel    = "deepseek-flash"
	DefaultLanguage = "auto"
)

// Config 对应 config.yaml 的内容。
type Config struct {
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
	Language string `yaml:"language"` // auto | zh | en
	Verbose  bool   `yaml:"verbose"`
}

// New 返回一份带默认值的配置。
func New() *Config {
	return &Config{
		BaseURL:  DefaultBaseURL,
		Model:    DefaultModel,
		Language: DefaultLanguage,
	}
}

// DefaultPath 返回默认的配置文件路径。
//
// 优先使用环境变量 COMMAND_AI_CONFIG；否则使用
// $XDG_CONFIG_HOME/command-ai/config.yaml，在未设置时回落到
// $HOME/.config/command-ai/config.yaml。
func DefaultPath() string {
	if p := os.Getenv("COMMAND_AI_CONFIG"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			// 极端情况下退回当前目录，保证程序仍可运行。
			return "config.yaml"
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "command-ai", "config.yaml")
}

// EffectivePath 返回实际生效的配置文件路径。
//
// 优先使用 COMMAND_AI_HOME(便于隔离与测试)，其次 DefaultPath。
func EffectivePath() string {
	if home := os.Getenv("COMMAND_AI_HOME"); home != "" {
		return filepath.Join(home, "config.yaml")
	}
	return DefaultPath()
}

// Load 读取配置文件。文件不存在时返回默认配置(不报错)。
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	cfg := New()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf(i18n.T("config.read_failed"), err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf(i18n.T("config.parse_failed"), path, err)
	}
	cfg.normalize()
	return cfg, nil
}

// normalize 补齐缺省字段。
func (c *Config) normalize() {
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	if c.Model == "" {
		c.Model = DefaultModel
	}
	if c.Language == "" {
		c.Language = DefaultLanguage
	}
}

// Save 把配置写入 path，目录与文件权限分别为 0700 / 0600。
func (c *Config) Save(path string) error {
	if path == "" {
		path = DefaultPath()
	}
	c.normalize()

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf(i18n.T("config.marshal_failed"), err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf(i18n.T("config.mkdir_failed"), err)
	}

	// 先写临时文件再改名，避免写入过程中断导致配置损坏。
	tmp := path + ".tmp"
	if err := writeFile0600(tmp, data); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf(i18n.T("config.save_failed"), err)
	}
	// Rename 会保留临时文件的权限，这里再确保一次。
	if err := os.Chmod(path, 0o600); err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf(i18n.T("config.chmod_failed"), err)
	}
	return nil
}

// writeFile0600 以 0600 权限写入文件。
func writeFile0600(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf(i18n.T("config.create_failed"), path, err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf(i18n.T("config.write_failed"), path, err)
	}
	return nil
}

// Masked 返回脱敏后的 API Key，仅保留前 4 位。
func (c *Config) Masked() string {
	return MaskKey(c.APIKey)
}

// MaskKey 对 API Key 脱敏，用于任何可能被打印或记录的场合。
func MaskKey(key string) string {
	if key == "" {
		return i18n.T("config.key_unset")
	}
	if len(key) <= 4 {
		return "****"
	}
	return key[:4] + "****"
}
