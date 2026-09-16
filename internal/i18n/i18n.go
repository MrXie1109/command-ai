// Package i18n 提供中英文双语文案。
//
// 语言优先级：config.yaml 中的显式设置 > 环境变量(LC_ALL / LC_MESSAGES / LANG)。
// 未设置或为 C/POSIX 时按惯例使用英文。
package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Lang 是界面语言。
type Lang string

// 支持的语言。
const (
	EN Lang = "en"
	ZH Lang = "zh"
)

// LangAuto 表示按环境变量自动判定语言。
const LangAuto = "auto"

// current 是当前界面语言。进程内只有一种界面语言，因此用包级变量承载。
var current = EN

// SetLang 设置当前界面语言。传入无法识别的值时回退到英文。
func SetLang(l Lang) {
	switch l {
	case ZH:
		current = ZH
	default:
		current = EN
	}
}

// Current 返回当前界面语言。
func Current() Lang { return current }

// Parse 解析配置中的语言取值。
//
// 第二个返回值表示该取值是否被识别为一种具体语言。
// 空值、auto 以及无法识别的取值都返回 false，交由 Resolve 回退到环境变量。
func Parse(s string) (Lang, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "zh", "zh_cn", "zh-cn", "zh_hans", "zh-hans", "cn", "chinese", "中文":
		return ZH, true
	case "en", "en_us", "en-us", "en_gb", "english":
		return EN, true
	default:
		return EN, false
	}
}

// Resolve 结合配置值与环境变量得到最终语言。
//
// 配置里显式写了可识别的语言时以配置为准(用户偏好优先)；
// 否则(auto / 空 / 无法识别)按环境变量判定。
func Resolve(setting string) Lang {
	if l, ok := Parse(setting); ok {
		return l
	}
	return Detect()
}

// Quote 用双引号包裹字符串，且不转义其中的非 ASCII 字符。
func Quote(s string) string { return "\"" + s + "\"" }

// Detect 按 POSIX 优先级读取环境变量判定语言。
func Detect() Lang {
	return DetectFrom(os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG"))
}

// DetectFrom 按顺序检查语言环境取值，返回第一个有效值对应的语言。
//
// 取值形如 zh_CN.UTF-8、en_US.UTF-8@euro、C、POSIX。
// 全部为空或无法识别时返回英文(C locale 惯例)。
func DetectFrom(locales ...string) Lang {
	for _, raw := range locales {
		loc := strings.TrimSpace(raw)
		if loc == "" {
			continue
		}
		// 去掉编码与修饰符：zh_CN.UTF-8@euro -> zh_CN
		if i := strings.IndexAny(loc, ".@"); i >= 0 {
			loc = loc[:i]
		}
		loc = strings.ToLower(strings.ReplaceAll(loc, "-", "_"))
		switch loc {
		case "c", "posix":
			// C / POSIX 明确表示“没有本地化”，使用英文。
			return EN
		}
		lang := loc
		if i := strings.Index(lang, "_"); i >= 0 {
			lang = lang[:i]
		}
		if lang == "zh" {
			return ZH
		}
		// 任何其他语言(fr、de、ja…)都回退到英文，因为本项目只提供中英双语。
		return EN
	}
	return EN
}

// T 返回当前语言的文案，并按 args 做格式化。
func T(key string, args ...interface{}) string {
	return Target(current, key, args...)
}

// Target 返回指定语言的文案，便于测试与对照。
//
// 未定义的 key 原样返回，方便在开发期立刻发现问题。
func Target(l Lang, key string, args ...interface{}) string {
	e, ok := catalog[key]
	if !ok {
		return key
	}
	s := e.en
	if l == ZH && e.zh != "" {
		s = e.zh
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// Keys 返回全部已定义的文案 key，供测试校验。
func Keys() []string {
	out := make([]string, 0, len(catalog))
	for k := range catalog {
		out = append(out, k)
	}
	return out
}

type entry struct{ en, zh string }

// catalog 汇总全部用户可见文案。
//
// 注意：Command:、Token:、Thinking、Allow[y/N/e/r] 属于项目书规定的固定输出格式，
// 不随语言变化，因此不在此表中。
var catalog = map[string]entry{
	// ---------- config ----------
	"config.read_failed":    {"failed to read config file: %v", "读取配置文件失败: %v"},
	"config.parse_failed":   {"failed to parse config file %s: %v", "解析配置文件失败 %s: %v"},
	"config.marshal_failed": {"failed to encode config: %v", "序列化配置失败: %v"},
	"config.mkdir_failed":   {"failed to create config directory: %v", "创建配置目录失败: %v"},
	"config.create_failed":  {"failed to create file %s: %v", "创建文件失败 %s: %v"},
	"config.write_failed":   {"failed to write file %s: %v", "写入文件失败 %s: %v"},
	"config.save_failed":    {"failed to write config file: %v", "写入配置文件失败: %v"},
	"config.chmod_failed":   {"failed to set config file permissions: %v", "设置配置文件权限失败: %v"},
	"config.key_unset":      {"(not set)", "(未设置)"},

	// ---------- history ----------
	"history.mkdir_failed":   {"failed to create history directory: %v", "创建历史目录失败: %v"},
	"history.marshal_failed": {"failed to encode history record: %v", "序列化历史记录失败: %v"},
	"history.open_failed":    {"failed to open history file %s: %v", "打开历史文件失败 %s: %v"},
	"history.write_failed":   {"failed to write history record: %v", "写入历史记录失败: %v"},
	"history.readdir_failed": {"failed to read history directory: %v", "读取历史目录失败: %v"},
	"history.scan_failed":    {"failed to read history file %s: %v", "读取历史文件失败 %s: %v"},

	// ---------- llm ----------
	"llm.no_api_key":     {"no API key configured; run: command-ai api-key <key>", "未配置 API Key，请先执行: command-ai api-key <key>"},
	"llm.no_model":       {"no model configured; run: command-ai model <name>", "未配置模型，请先执行: command-ai model <name>"},
	"llm.build_failed":   {"failed to build request: %v", "构造请求失败: %v"},
	"llm.request_failed": {"LLM request failed: %v", "请求 LLM 失败: %v"},
	"llm.read_failed":    {"failed to read response: %v", "读取响应失败: %v"},
	"llm.http_error":     {"LLM returned an error (%d): %s", "LLM 返回错误 (%d): %s"},
	"llm.parse_failed":   {"failed to parse response: %v", "解析响应失败: %v"},
	"llm.api_error":      {"LLM returned an error: %s", "LLM 返回错误: %s"},
	"llm.no_choices":     {"LLM returned no result", "LLM 未返回任何结果"},
	"llm.empty_response": {"(empty response)", "(空响应)"},

	// ---------- executor ----------
	"executor.empty_command": {"command is empty", "命令为空"},
	"executor.timeout":       {"command timed out after %s", "命令执行超时(%s)"},

	// ---------- ui ----------
	"ui.invalid_input":   {"invalid input %s, please enter y / n / e / r", "无效输入 %s，请输入 y / n / e / r"},
	"ui.feedback_prompt": {"Feedback (optional, press Enter to skip): ", "Feedback (可选，回车跳过): "},
	"ui.allow_prompt":    {"Allow[y/N/e/r] ", "Allow[y/N/e/r] "},

	// ---------- usage ----------
	"usage.unknown_period": {"unknown period %s; valid values: today, this-week, this-month, this-year, all",
		"未知的统计周期 %s，可选值: today, this-week, this-month, this-year, all"},
	"usage.period.today":      {"today", "今日"},
	"usage.period.this_week":  {"this week", "本周"},
	"usage.period.this_month": {"this month", "本月"},
	"usage.period.this_year":  {"this year", "本年"},
	"usage.period.all":        {"all-time", "全部"},
	"usage.title_range":       {"%s usage (%s to %s)", "%s用量(%s ~ %s)"},
	"usage.title_plain":       {"%s usage", "%s用量"},
	"usage.line.requests":     {"  Requests:      %d\n", "  请求次数:     %d\n"},
	"usage.line.input":        {"  INPUT tokens:  %d\n", "  INPUT Token:  %d\n"},
	"usage.line.output":       {"  OUTPUT tokens: %d\n", "  OUTPUT Token: %d\n"},
	"usage.line.total":        {"  Total tokens:  %d\n", "  合计 Token:   %d\n"},
	"usage.line.executed":     {"  Executed:      %d\n", "  实际执行:     %d\n"},

	// ---------- cli ----------
	"cli.err":               {"error: %v", "错误: %v"},
	"cli.base_url_scheme":   {"base URL must start with http:// or https://", "Base URL 需以 http:// 或 https:// 开头"},
	"cli.api_key_empty":     {"API key must not be empty", "API Key 不能为空"},
	"cli.model_empty":       {"model name must not be empty", "模型名不能为空"},
	"cli.warn":              {"warning: %v", "警告: %v"},
	"cli.usage_set":         {"usage: command-ai %s <value>", "用法: command-ai %s <值>"},
	"cli.saved_to":          {"saved to %s", "已保存到 %s"},
	"cli.verbose_status":    {"verbose = %t", "verbose = %t"},
	"cli.config_path":       {"config file: %s", "配置文件: %s"},
	"cli.config_language":   {"language: %s (%s)", "语言:     %s (%s)"},
	"cli.empty_request":     {"error: the request is empty", "错误: 需求描述为空"},
	"cli.read_input_failed": {"error: failed to read input: %v", "错误: 读取输入失败: %v"},
	"cli.max_regen":         {"reached the maximum number of regenerations (%d), aborting", "已达到最大重新生成次数 (%d)，退出"},
	"cli.exit_code":         {"(exit code %d)", "(退出码 %d)"},
	"cli.no_command":        {"error: the model did not return an executable command", "错误: 模型没有返回可执行的命令"},
	"cli.token":             {"Token: %d/%d\n", "Token: %d/%d\n"},
	"cli.lang_usage":        {"usage: command-ai lang [zh|en|auto]", "用法: command-ai lang [zh|en|auto]"},
	"cli.lang_invalid":      {"error: language must be zh, en or auto", "错误: 语言只能是 zh、en 或 auto"},
	"cli.lang_set":          {"language = %s", "language = %s"},
	"cli.lang_status":       {"current: %s, from config: %s, from environment: %s", "当前: %s，配置文件: %s，环境变量: %s"},
	"cli.lang_name_en":      {"English", "英文"},
	"cli.lang_name_zh":      {"Chinese", "中文"},

	// verbose 诊断信息
	"cli.vb_endpoint":   {"-> POST %s/chat/completions model=%s", "→ POST %s/chat/completions model=%s"},
	"cli.vb_request":    {"-> request: %s", "→ 需求: %s"},
	"cli.vb_previous":   {"-> previous: %q feedback: %q", "→ 上一条命令: %q 反馈: %q"},
	"cli.vb_tokens":     {"<- tokens: input=%d output=%d total=%d", "← Token: input=%d output=%d total=%d"},
	"cli.vb_explain":    {"<- explain tokens: input=%d output=%d", "← 解释 Token: input=%d output=%d"},
	"cli.vb_cumulative": {"- cumulative: %d LLM call(s), input=%d output=%d", "· 累计: LLM 调用 %d 次, input=%d output=%d"},
	"cli.vb_duration":   {"- took %s, exit code %d", "· 耗时 %s, 退出码 %d"},
	"cli.vb_history":    {"- history directory: %s", "· 历史目录: %s"},
	"cli.vb_recorded":   {"- recorded history/%s.jsonl (choice=%s)", "· 已记录 history/%s.jsonl (choice=%s)"},
	"cli.vb_language":   {"- language: %s (LANG=%s)", "· 语言: %s (LANG=%s)"},
}

func init() {
	// 帮助与用法文本较长，单独注册以保持上表可读。
	catalog["cli.help"] = entry{en: helpEN, zh: helpZH}
}

// LangName 返回语言的自称。
func LangName(l Lang) string {
	if l == ZH {
		return T("cli.lang_name_zh")
	}
	return T("cli.lang_name_en")
}

// EnvHint 返回用于诊断的环境变量取值。
func EnvHint() string {
	if v := os.Getenv("LANG"); v != "" {
		return v
	}
	return "(unset)"
}

const helpEN = `command-ai - generate and run shell commands from natural language

Usage:
  command-ai "request"                   generate a command, then confirm to run it
  command-ai base-url <url>              set the LLM base URL
  command-ai api-key <key>               set the API key
  command-ai model <name>                set the model name
  command-ai lang [zh|en|auto]           set the interface language
  command-ai verbose                     toggle verbose output
  command-ai usage [period]              show token usage
  command-ai config                      show current configuration
  command-ai help                        show this help
  command-ai version                     show the version

Periods:
  today | this-week | this-month | this-year | all   (default: today)

Interaction:
  After a command is generated you are prompted with Allow[y/N/e/r]
    y  run the command
    n  cancel (pressing Enter alone means n)
    e  explain the command in the language of your request, then ask again
    r  regenerate the command (you may add a short feedback message)

Examples:
  command-ai "list the files in the current directory"
  command-ai "show me what is in my home directory"
  command-ai usage this-month

Environment:
  COMMAND_AI_HOME    override the root directory for config and history
  COMMAND_AI_CONFIG  override only the config file path
  LANG / LC_ALL / LC_MESSAGES
                     select the interface language (zh -> Chinese, otherwise English)
`

const helpZH = `command-ai - 用自然语言生成并执行 shell 命令

用法:
  command-ai "需求描述"                  生成命令，确认后执行
  command-ai base-url <url>              设置 LLM Base URL
  command-ai api-key <key>               设置 API Key
  command-ai model <name>                设置模型名称
  command-ai lang [zh|en|auto]           设置界面语言
  command-ai verbose                     切换详细输出模式
  command-ai usage [周期]                查看 Token 用量
  command-ai config                      查看当前配置
  command-ai help                        显示帮助
  command-ai version                     显示版本

统计周期:
  today | this-week | this-month | this-year | all   (默认 today)

交互说明:
  生成命令后会提示 Allow[y/N/e/r]
    y  执行该命令
    n  取消(直接回车等同于 n)
    e  用与需求相同的语言解释该命令，然后再次询问
    r  重新生成命令(可附加一段反馈)

示例:
  command-ai "帮我列出当前目录下的文件"
  command-ai "帮我查看家目录下的文件"
  command-ai usage this-month

环境变量:
  COMMAND_AI_HOME    覆盖配置与历史数据的存放根目录
  COMMAND_AI_CONFIG  仅覆盖配置文件路径
  LANG / LC_ALL / LC_MESSAGES
                     选择界面语言(zh 为中文，其他为英文)
`
