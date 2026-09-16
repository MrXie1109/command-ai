// Package prompt 管理用户可编辑的对话模板。
//
// 模板文件 template.txt 与 config.yaml 同级。它会作为「生成命令」这一步的
// system 消息发送给模型，因此用户无需改代码即可调整提示词。
//
// 设计要点：
//   - 文件不存在或内容为空时回退到内置默认模板，删掉文件即可恢复原状
//   - 模板中的 {{占位符}} 会在发送前被替换
//   - 内置默认模板与首次生成的 template.txt 内容完全一致，只有一份定义
package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Filename 是模板文件名。
const Filename = "template.txt"

// Data 是渲染模板时可用的占位符取值。
type Data struct {
	OS       string // 运行时操作系统，例如 linux
	Arch     string // 运行时架构，例如 amd64
	Shell    string // 命令将要交给的解释器，例如 sh / cmd.exe
	Request  string // 用户本次的自然语言需求
	Previous string // 上一次的命令或拒答原因，首轮为空
	Feedback string // 用户按 r 时填写的反馈，可为空
}

// placeholderNames 是全部受支持的占位符名字，用于帮助输出与校验。
var placeholderNames = []string{"os", "arch", "shell", "request", "previous", "feedback"}

// Placeholders 返回全部占位符(含花括号)，供帮助信息使用。
func Placeholders() []string {
	out := make([]string, 0, len(placeholderNames))
	for _, n := range placeholderNames {
		out = append(out, "{{"+n+"}}")
	}
	return out
}

// PathFor 返回与给定配置文件同级的模板路径。
func PathFor(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), Filename)
}

// Default 返回内置的默认模板。
//
// 它同时是首次运行写入 template.txt 的内容，所以文件里看到的就是实际生效的
// 提示词；用户可以直接在此基础上修改。
func Default() string {
	return `You are a shell command generator for a CLI tool named command-ai.

Environment: {{os}}/{{arch}}, user shell: {{shell}}

Reply with EXACTLY ONE of these two forms, and nothing else:

Command: <a single executable command>
Error: <one short sentence explaining why no command can be given>

Use "Command:" only when the request can be expressed as one executable command:
- Keep the command on the SAME line as the prefix.
- Exactly one command. No explanation, no markdown code fences, no leading "$" or ">".
- You are NOT running inside a shell and must NOT invoke one yourself. Never emit
  "sh -c ...", "bash -c ...", "cmd /C ..." or any other nested shell wrapper;
  command-ai already hands your command to the platform shell.
- Never use shell-only sugar or aliases. Write "$HOME" instead of "~", and
  "%USERPROFILE%" on Windows. No history expansion, no interactive built-ins.
- Prefer common tools that exist on the target platform.

Use "Error:" whenever you cannot produce a single command:
- The request is not a computer task (chit-chat, greetings, insults, opinions).
- It needs an interactive session, a shell built-in, or several dependent steps.
- It is unsafe or you must refuse it.
Write the reason in the SAME LANGUAGE as the user's request, in one short sentence.
Never disguise a sentence, a placeholder, or an English apology as a "Command:".

Examples:
  "list files in my home directory"  -> Command: ls $HOME
  "show disk usage"                  -> Command: df -h
  "what is the meaning of life"      -> Error: 这不是可以用单条命令完成的任务。
  "open an interactive python shell" -> Error: 交互式会话无法用单条命令完成。
  "I hate you"                       -> Error: 我无法执行这个请求。
`
}

// placeholderRe 匹配 {{name}}，允许花括号内有空白。
var placeholderRe = regexp.MustCompile(`\{\{\s*([a-zA-Z_]+)\s*\}\}`)

// Render 把模板中的占位符替换为 data 中的取值。
//
// 无法识别的占位符原样保留，便于用户发现拼写错误。
func Render(tpl string, data Data) string {
	values := map[string]string{
		"os":       data.OS,
		"arch":     data.Arch,
		"shell":    data.Shell,
		"request":  data.Request,
		"previous": data.Previous,
		"feedback": data.Feedback,
	}
	return placeholderRe.ReplaceAllStringFunc(tpl, func(m string) string {
		name := strings.ToLower(placeholderRe.FindStringSubmatch(m)[1])
		v, ok := values[name]
		if !ok {
			return m // 未知占位符保持原样
		}
		return v
	})
}

// HasPlaceholder 报告模板是否使用了某个占位符。
func HasPlaceholder(tpl, name string) bool {
	for _, m := range placeholderRe.FindAllStringSubmatch(tpl, -1) {
		if strings.EqualFold(m[1], name) {
			return true
		}
	}
	return false
}

// Load 读取模板文件。
//
// 返回值 custom 表示内容来自用户文件；文件不存在或只有空白时返回内置默认
// 模板，custom 为 false。
func Load(path string) (content string, custom bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), false, nil
		}
		return "", false, fmt.Errorf("读取模板文件失败 %s: %w", path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		// 空文件视为未自定义，避免用户清空文件后得到空提示词。
		return Default(), false, nil
	}
	return string(data), true, nil
}

// Ensure 在模板文件不存在时写入内置默认模板。
//
// 返回 created 表示本次是否新建。写入失败不算致命错误，调用方可忽略并
// 继续使用内置模板。
func Ensure(path string) (created bool, err error) {
	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	} else if !os.IsNotExist(statErr) {
		return false, fmt.Errorf("检查模板文件失败 %s: %w", path, statErr)
	}
	if err := Save(path, Default()); err != nil {
		return false, err
	}
	return true, nil
}

// Save 以 0600 权限写入模板文件，父目录按需创建(0700)。
func Save(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建模板目录失败: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("写入模板文件失败 %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return fmt.Errorf("写入模板文件失败 %s: %w", path, err)
	}
	return nil
}
