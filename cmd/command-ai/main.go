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
	"github.com/command-ai/command-ai/internal/i18n"
	"github.com/command-ai/command-ai/internal/llm"
	"github.com/command-ai/command-ai/internal/prompt"
	"github.com/command-ai/command-ai/internal/ui"
	"github.com/command-ai/command-ai/internal/usage"
)

// version 是当前版本号，构建时可通过 -ldflags 覆盖。
var version = "1.3.0"

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
	// 先按环境变量(LC_ALL / LC_MESSAGES / LANG)选择语言，保证即使配置
	// 读取失败，错误提示也是可读的；随后用配置中的显式设置覆盖。
	i18n.SetLang(i18n.Detect())
	if cfg, _, err := loadConfig(); err == nil {
		i18n.SetLang(i18n.Resolve(cfg.Language))
	}

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
				return errors.New(i18n.T("cli.base_url_scheme"))
			}
			c.BaseURL = strings.TrimRight(v, "/")
			return nil
		})
	case "api-key":
		return cmdSetConfig("api-key", rest, func(c *config.Config, v string) error {
			if strings.TrimSpace(v) == "" {
				return errors.New(i18n.T("cli.api_key_empty"))
			}
			c.APIKey = strings.TrimSpace(v)
			return nil
		})
	case "model":
		return cmdSetConfig("model", rest, func(c *config.Config, v string) error {
			if strings.TrimSpace(v) == "" {
				return errors.New(i18n.T("cli.model_empty"))
			}
			c.Model = strings.TrimSpace(v)
			return nil
		})
	case "verbose":
		return cmdVerbose()
	case "lang", "language":
		return cmdLang(rest)
	case "template", "tmpl":
		return cmdTemplate(rest)
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
	path := config.EffectivePath()
	cfg, err := config.Load(path)
	if err != nil {
		return nil, path, err
	}
	return cfg, path, nil
}

// loadSystemTemplate 读取与 config.yaml 同级的对话模板。
//
// 首次运行时自动生成一份内置默认模板，便于用户发现并直接编辑；
// 生成失败(例如目录只读)不算致命错误，此时使用内置模板。
func loadSystemTemplate() string {
	path := prompt.PathFor(config.EffectivePath())
	if created, err := prompt.Ensure(path); err != nil {
		// 只提示，不影响主流程。
		fmt.Fprintf(stderr, i18n.T("cli.warn")+"\n", err)
	} else if created {
		fmt.Fprintf(stderr, i18n.T("cli.tmpl_created")+"\n", path)
	}
	content, _, err := prompt.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, i18n.T("cli.warn")+"\n", err)
		return prompt.Default()
	}
	return content
}

// cmdSetConfig 是 base-url / api-key / model 三个设置命令的通用实现。
func cmdSetConfig(name string, args []string, apply func(*config.Config, string) error) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, i18n.T("cli.usage_set")+"\n", name)
		return 2
	}
	value := strings.Join(args, " ")

	cfg, path, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
		return 1
	}
	if err := apply(cfg, value); err != nil {
		fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
		return 2
	}
	if err := cfg.Save(path); err != nil {
		fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
		return 1
	}

	// 不打印 API Key 明文。
	shown := value
	if name == "api-key" {
		shown = config.MaskKey(value)
	}
	fmt.Fprintf(stdout, "%s = %s\n", name, shown)
	fmt.Fprintf(stdout, i18n.T("cli.saved_to")+"\n", path)
	return 0
}

func cmdVerbose() int {
	cfg, path, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
		return 1
	}
	cfg.Verbose = !cfg.Verbose
	if err := cfg.Save(path); err != nil {
		fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
		return 1
	}
	fmt.Fprintf(stdout, i18n.T("cli.verbose_status")+"\n", cfg.Verbose)
	return 0
}

// cmdLang 查看或设置界面语言。
//
//	command-ai lang            显示当前语言及来源
//	command-ai lang zh|en|auto 设置语言(auto 表示跟随 LANG 等环境变量)
func cmdLang(args []string) int {
	cfg, path, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
		return 1
	}

	if len(args) == 0 {
		effective := i18n.Resolve(cfg.Language)
		fmt.Fprintf(stdout, i18n.T("cli.lang_status")+"\n",
			i18n.LangName(effective), cfg.Language, i18n.LangName(i18n.Detect()))
		return 0
	}

	want := strings.ToLower(strings.TrimSpace(args[0]))
	switch want {
	case "zh", "en", "auto":
	default:
		fmt.Fprintln(stderr, i18n.T("cli.lang_invalid"))
		fmt.Fprintln(stderr, i18n.T("cli.lang_usage"))
		return 2
	}

	cfg.Language = want
	if err := cfg.Save(path); err != nil {
		fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
		return 1
	}

	// 立即切换，使本次输出使用新语言。
	i18n.SetLang(i18n.Resolve(want))
	fmt.Fprintf(stdout, i18n.T("cli.lang_set")+"\n", want)
	if want == i18n.LangAuto {
		fmt.Fprintf(stdout, i18n.T("cli.lang_status")+"\n",
			i18n.LangName(i18n.Current()), want, i18n.LangName(i18n.Detect()))
	}
	fmt.Fprintf(stdout, i18n.T("cli.saved_to")+"\n", path)
	return 0
}

// cmdTemplate 查看或重置对话模板。
//
//	command-ai template          显示模板路径、来源与可用占位符
//	command-ai template show     打印当前生效的模板内容
//	command-ai template path     仅打印路径，便于 $EDITOR $(command-ai template path)
//	command-ai template reset    恢复内置默认模板
func cmdTemplate(args []string) int {
	cfgPath := config.EffectivePath()
	path := prompt.PathFor(cfgPath)

	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(strings.TrimSpace(args[0]))
	}

	switch sub {
	case "", "status":
		// 首次运行时同样生成默认模板，保持与执行流程一致。
		if created, err := prompt.Ensure(path); err != nil {
			fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
		} else if created {
			fmt.Fprintf(stdout, i18n.T("cli.tmpl_created")+"\n", path)
		}
		content, custom, err := prompt.Load(path)
		if err != nil {
			fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
			return 1
		}
		fmt.Fprintf(stdout, i18n.T("cli.tmpl_path")+"\n", path)
		if custom {
			fmt.Fprintf(stdout, i18n.T("cli.tmpl_custom")+"\n", len(content))
		} else {
			fmt.Fprintln(stdout, i18n.T("cli.tmpl_default"))
		}
		fmt.Fprintf(stdout, i18n.T("cli.tmpl_placeholders")+"\n", strings.Join(prompt.Placeholders(), " "))
		fmt.Fprintln(stdout, i18n.T("cli.tmpl_hint"))
		return 0

	case "path":
		fmt.Fprintln(stdout, path)
		return 0

	case "show":
		content, _, err := prompt.Load(path)
		if err != nil {
			fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
			return 1
		}
		fmt.Fprintln(stdout, i18n.T("cli.tmpl_show_banner"))
		fmt.Fprint(stdout, content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Fprintln(stdout)
		}
		return 0

	case "reset":
		fmt.Fprintf(stdout, i18n.T("cli.tmpl_reset_warn")+"\n", path)
		if err := prompt.Save(path, prompt.Default()); err != nil {
			fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
			return 1
		}
		fmt.Fprintf(stdout, i18n.T("cli.tmpl_reset")+"\n", path)
		return 0

	default:
		fmt.Fprintln(stderr, i18n.T("cli.tmpl_usage"))
		return 2
	}
}

func cmdShowConfig() int {
	cfg, path, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
		return 1
	}
	effective := i18n.Resolve(cfg.Language)
	fmt.Fprintf(stdout, i18n.T("cli.config_path")+"\n", path)
	fmt.Fprintf(stdout, "base_url: %s\n", cfg.BaseURL)
	fmt.Fprintf(stdout, "api_key:  %s\n", cfg.Masked())
	fmt.Fprintf(stdout, "model:    %s\n", cfg.Model)
	fmt.Fprintf(stdout, i18n.T("cli.config_language")+"\n", cfg.Language, i18n.LangName(effective))
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
		fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
		return 2
	}

	store, err := history.New(history.DefaultDir())
	if err != nil {
		fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
		return 1
	}
	st, err := usage.Collect(store, period, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
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
	style    *ui.Style

	request  string
	reply    llm.Reply // 模型本次的结构化回复(命令或拒答原因)
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
// 状态机(与项目书一致)：
//
//	输入 → Thinking → 生成命令 → Allow?
//	                                ├─ y → 执行 → 输出 → Token
//	                                ├─ n → 取消 → Token
//	                                ├─ e → 解释 → Allow?(同一命令)
//	                                └─ r → 重新生成 → Allow?
func cmdAsk(request string) int {
	request = strings.TrimSpace(request)
	if request == "" {
		fmt.Fprintln(stderr, i18n.T("cli.empty_request"))
		printUsage(stderr)
		return 2
	}

	cfg, _, err := loadConfig()
	if err != nil {
		fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
		return 1
	}

	store, err := history.New(history.DefaultDir())
	if err != nil {
		fmt.Fprintf(stderr, i18n.T("cli.err")+"\n", err)
		return 1
	}

	client := llm.New(cfg.BaseURL, cfg.APIKey, cfg.Model, nil)
	client.SystemTemplate = loadSystemTemplate()

	s := &session{
		cfg:      cfg,
		client:   client,
		store:    store,
		prompter: ui.NewPrompter(stdin, stdout),
		style:    ui.NewStyle(stdout),
		request:  request,
	}
	return s.run()
}

// run 驱动交互状态机。
//
//	输入 → Thinking → 模型回复
//	                   ├─ Command: <命令> → Allow[y/N/e/r]
//	                   │     ├─ y → 执行 → 输出 → Token
//	                   │     ├─ n → 取消 → Token
//	                   │     ├─ e → 解释命令 → Allow?(同一命令)
//	                   │     └─ r → 重新生成 → Allow?
//	                   └─ Error: <原因>   → Allow[N/e/r](没有命令可执行)
//	                         ├─ n → 取消 → Token
//	                         ├─ e → 解释原因 → Allow?
//	                         └─ r → 重新生成 → Allow?
func (s *session) run() int {
	haveReply := false // 是否需要(重新)请求模型
	showReply := false // 是否需要打印 Command:/Error: 行
	regens := 0

	for {
		// 1) 需要新回复时调用 LLM。
		if !haveReply {
			reply, err := s.generate()
			if err != nil {
				fmt.Fprintf(stderr, s.style.Red(i18n.T("cli.err"))+"\n", err)
				s.done()
				return 1
			}
			s.reply = reply
			haveReply = true
			showReply = true
		}

		// 2) 展示回复并询问用户。解释过之后不重复展示。
		if showReply {
			s.showReply()
		}

		// 模型拒答时没有可执行内容，提示中不提供 y。
		action, err := s.prompter.Ask(s.reply.IsCommand())
		if err != nil {
			fmt.Fprintf(stderr, s.style.Red(i18n.T("cli.read_input_failed"))+"\n", err)
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
			// 解释之后回到提示，内容保持不变、不重复展示。
			if err := s.explain(); err != nil {
				fmt.Fprintf(stderr, s.style.Red(i18n.T("cli.err"))+"\n", err)
				s.done()
				return 1
			}
			showReply = false
			continue

		case ui.ActionRegen:
			regens++
			if regens > maxRegenerate {
				fmt.Fprintf(stderr, s.style.Red(i18n.T("cli.max_regen"))+"\n", maxRegenerate)
				s.done()
				return 1
			}
			fb, err := s.prompter.AskFeedback()
			if err != nil {
				fmt.Fprintf(stderr, s.style.Red(i18n.T("cli.read_input_failed"))+"\n", err)
				return 1
			}
			s.feedback = fb
			s.append(s.buildRecord(history.ChoiceRegen, nil, ""))
			haveReply = false // 回到 Thinking 重新生成
			continue
		}
	}
}

// showReply 按回复类型打印 "Command:" 或 "Error:" 行。
func (s *session) showReply() {
	if s.reply.IsCommand() {
		fmt.Fprintln(stdout, s.style.Label(i18n.T("cli.command_label"), s.style.Cyan(s.reply.Text)))
		return
	}
	text := s.reply.Text
	if text == "" {
		text = i18n.T("cli.empty_reply")
	}
	fmt.Fprintln(stdout, s.style.ErrorLine(i18n.T("cli.error_label"), text))
}

// generate 请求模型生成命令或说明拒答原因，并累计 Token。
func (s *session) generate() (llm.Reply, error) {
	s.verbosef(i18n.T("cli.vb_endpoint"), s.cfg.BaseURL, s.cfg.Model)
	s.verbosef(i18n.T("cli.vb_request"), s.request)
	if s.reply.Text != "" || s.feedback != "" {
		s.verbosef(i18n.T("cli.vb_previous"), s.reply.Text, s.feedback)
	}

	sp := ui.NewSpinner(stdout, "Thinking")
	sp.Start()
	reply, u, err := s.client.GenerateCommand(context.Background(), s.request, s.reply, s.feedback)
	sp.Stop()

	s.verbosef(i18n.T("cli.vb_tokens"), u.PromptTokens, u.CompletionTokens, u.TotalTokens)
	s.countUsage(u)

	if err != nil {
		// 网络失败、鉴权失败等：给出友好提示，且不写入历史。
		return llm.Reply{}, err
	}
	// 未标注为 Command 的回复一律当作拒答，绝不执行。
	if reply.IsCommand() && reply.Text == "" {
		return llm.Reply{Kind: llm.KindError, Text: i18n.T("cli.unlabeled_reply")}, nil
	}
	return reply, nil
}

// explain 解释当前回复：命令走命令解释，拒答走原因说明。
func (s *session) explain() error {
	sp := ui.NewSpinner(stdout, "Thinking")
	sp.Start()

	var (
		text string
		u    llm.Usage
		err  error
	)
	if s.reply.IsCommand() {
		text, u, err = s.client.ExplainCommand(context.Background(), s.request, s.reply.Text)
	} else {
		text, u, err = s.client.ExplainError(context.Background(), s.request, s.reply.Text)
	}
	sp.Stop()

	s.verbosef(i18n.T("cli.vb_explain"), u.PromptTokens, u.CompletionTokens)
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
	s.verbosef(i18n.T("cli.vb_cumulative"), s.pendingCalls, s.totalIn, s.totalOut)
}

// verbosef 仅在 verbose 模式下输出诊断信息(不包含任何密钥)。
func (s *session) verbosef(format string, args ...interface{}) {
	if s.cfg.Verbose {
		fmt.Fprintf(stdout, format+"\n", args...)
	}
}

// execute 执行当前命令，写入历史，并返回进程退出码。
func (s *session) execute() int {
	// 执行器的输出同时写到终端(实时展示)和内部缓冲(写入历史)。
	ex := executor.New()
	ex.Stdout = stdout
	ex.Stderr = stderr
	res, execErr := ex.Run(s.reply.Text)

	rec := s.buildRecord(history.ChoiceYes, res, "")
	if execErr != nil {
		rec.Error = execErr.Error()
		s.append(rec)
		fmt.Fprintf(stderr, s.style.Red(i18n.T("cli.err"))+"\n", execErr)
		s.done()
		return 1
	}
	s.append(rec)
	if res.ExitCode != 0 {
		fmt.Fprintf(stderr, s.style.Dim(i18n.T("cli.exit_code"))+"\n", res.ExitCode)
	}
	s.verbosef(i18n.T("cli.vb_duration"), res.Duration.Round(time.Millisecond), res.ExitCode)
	s.verbosef(i18n.T("cli.vb_history"), s.store.Dir)
	s.done()
	return res.ExitCode
}

// buildRecord 构造一条历史记录。
func (s *session) buildRecord(choice history.Choice, res *executor.Result, errMsg string) history.Record {
	rec := history.Record{
		Timestamp:    time.Now(),
		Input:        s.request,
		Choice:       choice,
		InputTokens:  s.pendingIn,
		OutputTokens: s.pendingOut,
		LLMCalls:     s.pendingCalls,
		Model:        s.cfg.Model,
		Error:        errMsg,
	}
	if s.reply.IsCommand() {
		rec.Command = s.reply.Text
	} else {
		// 拒答不是执行失败，单独记录原因，便于事后区分。
		rec.ModelError = s.reply.Text
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
		fmt.Fprintf(stderr, i18n.T("cli.warn")+"\n", err)
		return
	}
	s.verbosef(i18n.T("cli.vb_recorded"), rec.Timestamp.Format("2006-01-02"), rec.Choice)
}

// done 打印本次会话的 Token 汇总。
func (s *session) done() {
	printTokenLine(s.totalIn, s.totalOut)
}

func printTokenLine(in, out int) {
	style := ui.NewStyle(stdout)
	fmt.Fprint(stdout, style.Dim(fmt.Sprintf(i18n.T("cli.token"), in, out)))
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, i18n.T("cli.help"))
}
