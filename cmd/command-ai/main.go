// Command command-ai 用自然语言驱动 AI 生成并执行 shell 命令。
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/command-ai/command-ai/internal/config"
	"github.com/command-ai/command-ai/internal/executor"
	"github.com/command-ai/command-ai/internal/history"
	"github.com/command-ai/command-ai/internal/llm"
	"github.com/command-ai/command-ai/internal/ui"
	"github.com/command-ai/command-ai/internal/usage"
)

// version 是当前版本号，构建时可通过 -ldflags 覆盖。
var version = "1.0.0"

// 标准流被抽成变量，便于在测试中替换。
var (
	stdin  io.Reader = os.Stdin
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

// 交互过程中的最大重新生成次数，防止无限循环。
const maxRegenerate = 5

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printUsage(stdout)
		return 0
	}

	cmd, rest := args[0], args[1:]

	// 未识别的子命令一律当作自然语言需求处理，这样用户可以直接
	// 执行 `command-ai "列出文件"`，也允许不带引号的多词输入。
	switch cmd {
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "command-ai %s\n", version)
		return 0
	case "base-url":
		return cmdSetConfig("base-url", rest, func(c *config.Config, v string) error {
			if !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
				return fmt.Errorf("Base URL 需以 http:// 或 https:// 开头")
			}
			c.BaseURL = strings.TrimRight(v, "/")
			return nil
		})
	case "api-key":
		return cmdSetConfig("api-key", rest, func(c *config.Config, v string) error {
			if strings.TrimSpace(v) == "" {
				return errors.New("API Key 不能为空")
			}
			c.APIKey = strings.TrimSpace(v)
			return nil
		})
	case "model":
		return cmdSetConfig("model", rest, func(c *config.Config, v string) error {
			if strings.TrimSpace(v) == "" {
				return errors.New("模型名不能为空")
			}
			c.Model = strings.TrimSpace(v)
			return nil
		})
	case "verbose":
		return cmdVerbose()
	case "usage":
		return cmdUsage(rest)
	case "config":
		return cmdShowConfig()
	}

	// 执行类：把全部参数拼成一条自然语言需求。
	request := strings.Join(args, " ")
	return cmdAsk(request)
}

// ---------- 配置类 ----------

func loadConfig() (*config.Config, string, error) {
	path := config.DefaultPath()
	if v := os.Getenv("COMMAND_AI_HOME"); v != "" {
		// 便于测试与自定义数据目录。
		path = v + "/config.yaml"
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, path, err
	}
	return cfg, path, nil
}

// cmdSetConfig 是 base-url / api-key / model 三个设置命令的通用实现。
func cmdSetConfig(name string, args []string, apply func(*config.Config, string) error) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "用法: command-ai %s <值>\n", name)
		return 2
	}
	value := strings.Join(args, " ")

	cfg, path, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 1
	}
	if err := apply(cfg, value); err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 2
	}
	if err := cfg.Save(path); err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 1
	}

	// 不打印 API Key 明文。
	shown := value
	if name == "api-key" {
		shown = config.MaskKey(value)
	}
	fmt.Fprintf(stdout, "%s = %s\n", name, shown)
	fmt.Fprintf(stdout, "已保存到 %s\n", path)
	return 0
}

func cmdVerbose() int {
	cfg, path, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 1
	}
	cfg.Verbose = !cfg.Verbose
	if err := cfg.Save(path); err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "verbose = %t\n", cfg.Verbose)
	return 0
}

func cmdShowConfig() int {
	cfg, path, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "配置文件: %s\n", path)
	fmt.Fprintf(stdout, "base_url: %s\n", cfg.BaseURL)
	fmt.Fprintf(stdout, "api_key:  %s\n", cfg.Masked())
	fmt.Fprintf(stdout, "model:    %s\n", cfg.Model)
	fmt.Fprintf(stdout, "verbose:  %t\n", cfg.Verbose)
	return 0
}

// ---------- 统计类 ----------

func cmdUsage(args []string) int {
	arg := ""
	if len(args) > 0 {
		arg = args[0]
	}
	period, err := usage.ParsePeriod(arg)
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 2
	}

	store, err := history.New(history.DefaultDir())
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 1
	}
	st, err := usage.Collect(store, period, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 1
	}
	st.Write(stdout)
	return 0
}

// ---------- 执行类 ----------

// session 保存一次 command-ai 调用过程中的交互状态。
type session struct {
	cfg      *config.Config
	client   *llm.Client
	store    *history.Store
	prompter *ui.Prompter

	request  string
	command  string
	feedback string

	// total* 用于终端展示的会话累计值。
	totalIn  int
	totalOut int
	// pending* 是自上一条历史记录以来消耗的用量，写入记录后清零，
	// 保证 history/usage 相加得到的总量不重复计数。
	pendingIn    int
	pendingOut   int
	pendingCalls int
}

// cmdAsk 执行完整交互流程。
//
// 状态机（与项目书一致）：
//
//	输入 → Thinking → 生成命令 → Allow?
//	                                ├─ y → 执行 → 输出 → Token
//	                                ├─ n → 取消 → Token
//	                                ├─ e → 解释 → Allow?（同一命令）
//	                                └─ r → 重新生成 → Allow?
func cmdAsk(request string) int {
	request = strings.TrimSpace(request)
	if request == "" {
		fmt.Fprintln(stderr, "错误: 需求描述为空")
		printUsage(stderr)
		return 2
	}

	cfg, _, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 1
	}

	store, err := history.New(history.DefaultDir())
	if err != nil {
		fmt.Fprintf(stderr, "错误: %v\n", err)
		return 1
	}

	s := &session{
		cfg:      cfg,
		client:   llm.New(cfg.BaseURL, cfg.APIKey, cfg.Model, nil),
		store:    store,
		prompter: ui.NewPrompter(stdin, stdout),
		request:  request,
	}
	return s.run()
}

// run 驱动交互状态机。
func (s *session) run() int {
	haveCommand := false // 是否需要（重新）生成命令
	showCommand := false // 是否需要打印 Command: 行
	regens := 0

	for {
		// 1) 需要新命令时调用 LLM。
		if !haveCommand {
			cmdStr, err := s.generate()
			if err != nil {
				fmt.Fprintf(stderr, "错误: %v\n", err)
				s.done()
				return 1
			}
			s.command = cmdStr
			haveCommand = true
			showCommand = true
		}

		// 2) 展示命令并询问用户。解释过同一命令时不重复打印 Command:。
		if showCommand {
			fmt.Fprintf(stdout, "Command: %s\n", s.command)
		}
		action, err := s.prompter.Ask()
		if err != nil {
			fmt.Fprintf(stderr, "错误: 读取输入失败: %v\n", err)
			return 1
		}

		switch action {
		case ui.ActionYes:
			return s.execute()

		case ui.ActionNo:
			s.append(s.buildRecord(history.ChoiceNo, nil, ""))
			s.done()
			return 0

		case ui.ActionExplain:
			// 解释之后回到 Allow 提示，命令保持不变、不重复展示。
			if err := s.explain(); err != nil {
				fmt.Fprintf(stderr, "错误: %v\n", err)
				s.done()
				return 1
			}
			showCommand = false
			continue

		case ui.ActionRegen:
			regens++
			if regens > maxRegenerate {
				fmt.Fprintf(stderr, "已达到最大重新生成次数 (%d)，退出\n", maxRegenerate)
				s.done()
				return 1
			}
			fb, err := s.prompter.AskFeedback()
			if err != nil {
				fmt.Fprintf(stderr, "错误: 读取输入失败: %v\n", err)
				return 1
			}
			s.feedback = fb
			s.append(s.buildRecord(history.ChoiceRegen, nil, ""))
			haveCommand = false // 回到 Thinking 重新生成
			continue
		}
	}
}

// generate 调用 LLM 生成（或重新生成）一条命令，并累计 Token。
func (s *session) generate() (string, error) {
	s.verbosef("→ POST %s/chat/completions model=%s", s.cfg.BaseURL, s.cfg.Model)
	s.verbosef("→ request: %s", s.request)
	if s.command != "" || s.feedback != "" {
		s.verbosef("→ previous: %q feedback: %q", s.command, s.feedback)
	}

	sp := ui.NewSpinner(stdout, "Thinking")
	sp.Start()
	cmdStr, u, err := s.client.GenerateCommand(context.Background(), s.request, s.command, s.feedback)
	sp.Stop()

	s.verbosef("← tokens: input=%d output=%d total=%d", u.PromptTokens, u.CompletionTokens, u.TotalTokens)
	s.countUsage(u)

	if err != nil {
		// 网络失败、鉴权失败等：给出友好提示，且不写入历史。
		return "", err
	}
	if cmdStr == "" {
		return "", errors.New("模型没有返回可执行的命令")
	}
	return cmdStr, nil
}

// explain 调用 LLM 解释当前命令并打印。
func (s *session) explain() error {
	sp := ui.NewSpinner(stdout, "Thinking")
	sp.Start()
	text, u, err := s.client.ExplainCommand(context.Background(), s.request, s.command)
	sp.Stop()

	s.verbosef("← explain tokens: input=%d output=%d", u.PromptTokens, u.CompletionTokens)
	s.countUsage(u)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, text)
	return nil
}

// countUsage 累计一次 LLM 调用的 Token 与请求次数。
func (s *session) countUsage(u llm.Usage) {
	s.totalIn += u.PromptTokens
	s.totalOut += u.CompletionTokens
	s.pendingIn += u.PromptTokens
	s.pendingOut += u.CompletionTokens
	s.pendingCalls++
	s.verbosef("· 累计: LLM 调用 %d 次, input=%d output=%d", s.pendingCalls, s.totalIn, s.totalOut)
}

// verbosef 仅在 verbose 模式下输出诊断信息（不包含任何密钥）。
func (s *session) verbosef(format string, args ...interface{}) {
	if s.cfg.Verbose {
		fmt.Fprintf(stdout, format+"\n", args...)
	}
}

// execute 执行当前命令，写入历史，并返回进程退出码。
func (s *session) execute() int {
	// 执行器的输出同时写到终端（实时展示）和内部缓冲（写入历史）。
	ex := executor.New()
	ex.Stdout = stdout
	ex.Stderr = stderr
	res, execErr := ex.Run(s.command)

	rec := s.buildRecord(history.ChoiceYes, res, "")
	if execErr != nil {
		rec.Error = execErr.Error()
		s.append(rec)
		fmt.Fprintf(stderr, "错误: %v\n", execErr)
		s.done()
		return 1
	}
	s.append(rec)
	if res.ExitCode != 0 {
		fmt.Fprintf(stderr, "(退出码 %d)\n", res.ExitCode)
	}
	s.verbosef("· 耗时 %s, 退出码 %d", res.Duration.Round(time.Millisecond), res.ExitCode)
	s.verbosef("· 历史目录: %s", s.store.Dir)
	s.done()
	return res.ExitCode
}

// buildRecord 构造一条历史记录。
func (s *session) buildRecord(choice history.Choice, res *executor.Result, errMsg string) history.Record {
	rec := history.Record{
		Timestamp:    time.Now(),
		Input:        s.request,
		Command:      s.command,
		Choice:       choice,
		InputTokens:  s.pendingIn,
		OutputTokens: s.pendingOut,
		LLMCalls:     s.pendingCalls,
		Model:        s.cfg.Model,
		Error:        errMsg,
	}
	// 用量已归入本条记录，清零以免在后续记录中重复计数。
	s.pendingIn, s.pendingOut, s.pendingCalls = 0, 0, 0
	if res != nil {
		rec.Output = res.Combined()
		code := res.ExitCode
		rec.ExitCode = &code
	}
	return rec
}

// append 写入一条历史记录。失败只提示、不中断主流程。
func (s *session) append(rec history.Record) {
	if err := s.store.Append(rec); err != nil {
		fmt.Fprintf(stderr, "警告: %v\n", err)
		return
	}
	s.verbosef("· 已记录 history/%s.jsonl (choice=%s)", rec.Timestamp.Format("2006-01-02"), rec.Choice)
}

// done 打印本次会话的 Token 汇总。
func (s *session) done() {
	printTokenLine(s.totalIn, s.totalOut)
}

func printTokenLine(in, out int) {
	fmt.Fprintf(stdout, "Token: %d/%d\n", in, out)
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `command-ai - 用自然语言生成并执行 shell 命令

用法:
  command-ai "需求描述"                  生成命令，确认后执行
  command-ai base-url <url>              设置 LLM Base URL
  command-ai api-key <key>               设置 API Key
  command-ai model <name>                设置模型名称
  command-ai verbose                     切换详细输出模式
  command-ai usage [周期]                查看 Token 用量
  command-ai config                      查看当前配置
  command-ai help                        显示帮助
  command-ai version                     显示版本

统计周期:
  today | this-week | this-month | this-year | all   （默认 today）

交互说明:
  生成命令后会提示 Allow[y/N/e/r]
    y  执行该命令
    n  取消（直接回车等同于 n）
    e  用与需求相同的语言解释该命令，然后再次询问
    r  重新生成命令（可附加一段反馈）

示例:
  command-ai "帮我列出当前目录下的文件"
  command-ai "帮我查看 ~ 目录下的文件"
  command-ai usage this-month

环境变量:
  COMMAND_AI_HOME    覆盖配置与历史数据的存放根目录
  COMMAND_AI_CONFIG  仅覆盖配置文件路径
`)
}
