package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/command-ai/command-ai/internal/history"
)

// fakeLLM 是一个可编程的假 LLM 服务。
type fakeLLM struct {
	mu       sync.Mutex
	replies  []string // 依次返回的命令
	explain  string   // 解释文本
	calls    int
	lastBody map[string]any
	usage    map[string]int
}

func newFakeLLM(replies ...string) *fakeLLM {
	return &fakeLLM{
		replies: replies,
		explain: "该命令会列出文件。",
		usage:   map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
	}
}

func (f *fakeLLM) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(body, &req)

		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls++
		f.lastBody = req

		msgs, _ := req["messages"].([]any)
		isExplain := false
		if len(msgs) > 0 {
			if sys, ok := msgs[0].(map[string]any); ok {
				if s, _ := sys["content"].(string); strings.Contains(s, "You explain shell commands") {
					isExplain = true
				}
			}
		}

		content := f.explain
		if !isExplain {
			idx := f.calls - 1
			if idx < 0 {
				idx = 0
			}
			if idx >= len(f.replies) {
				idx = len(f.replies) - 1
			}
			if len(f.replies) == 0 {
				content = "ls"
			} else {
				content = f.replies[idx]
			}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": content}}},
			"usage":   f.usage,
		})
	}
}

// testEnv 搭建一个隔离的运行环境。
type testEnv struct {
	home    string
	stdout  *bytes.Buffer
	stderr  *bytes.Buffer
	restore func()
}

// setLocale 固定语言环境，使断言不依赖运行机器的 LANG。
func setLocale(t *testing.T, loc string) {
	t.Helper()
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", loc)
}

// setup 把配置目录、历史目录与标准流全部重定向到临时空间。
func setup(t *testing.T, llmURL, input string) *testEnv {
	t.Helper()

	setLocale(t, "zh_CN.UTF-8")
	home := t.TempDir()
	t.Setenv("COMMAND_AI_HOME", home)

	cfg := "base_url: " + llmURL + "\napi_key: sk-test\nmodel: test-model\nlanguage: zh\nverbose: false\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	env := &testEnv{home: home, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	oldIn, oldOut, oldErr := stdin, stdout, stderr
	stdin, stdout, stderr = strings.NewReader(input), env.stdout, env.stderr
	env.restore = func() { stdin, stdout, stderr = oldIn, oldOut, oldErr }
	t.Cleanup(env.restore)
	return env
}

// records 读取本次运行写入的全部历史记录。
func (e *testEnv) records(t *testing.T) []history.Record {
	t.Helper()
	store, err := history.New(filepath.Join(e.home, "history"))
	if err != nil {
		t.Fatalf("history.New: %v", err)
	}
	from := time.Date(1970, 1, 1, 0, 0, 0, 0, time.Local)
	recs, err := store.LoadRange(from, time.Now())
	if err != nil {
		t.Fatalf("LoadRange: %v", err)
	}
	return recs
}

func TestRunVersionAndHelp(t *testing.T) {
	setLocale(t, "zh_CN.UTF-8")
	var out bytes.Buffer
	oldOut := stdout
	stdout = &out
	defer func() { stdout = oldOut }()

	if code := run([]string{"version"}); code != 0 {
		t.Errorf("version 退出码 = %d", code)
	}
	if !strings.Contains(out.String(), "command-ai") {
		t.Errorf("version 输出 = %q", out.String())
	}

	out.Reset()
	if code := run([]string{"help"}); code != 0 {
		t.Errorf("help 退出码 = %d", code)
	}
	for _, want := range []string{"Allow[y/N/e/r]", "usage", "base-url", "api-key"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help 缺少 %q", want)
		}
	}
}

func TestRunNoArgsPrintsUsage(t *testing.T) {
	setLocale(t, "zh_CN.UTF-8")
	var out bytes.Buffer
	oldOut := stdout
	stdout = &out
	defer func() { stdout = oldOut }()

	if code := run(nil); code != 0 {
		t.Errorf("退出码 = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "用法") {
		t.Errorf("应输出用法, got %q", out.String())
	}
}

func TestRunSetConfigCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("COMMAND_AI_HOME", home)

	var out bytes.Buffer
	oldOut, oldErr := stdout, stderr
	stdout, stderr = &out, &bytes.Buffer{}
	defer func() { stdout, stderr = oldOut, oldErr }()

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"base-url", "https://example.com/v1"}, "base-url = https://example.com/v1"},
		{[]string{"model", "gpt-4o-mini"}, "model = gpt-4o-mini"},
		{[]string{"api-key", "sk-abcdefghijk"}, "api-key = sk-a****"},
	}
	for _, c := range cases {
		out.Reset()
		if code := run(c.args); code != 0 {
			t.Fatalf("%v 退出码 = %d", c.args, code)
		}
		if !strings.Contains(out.String(), c.want) {
			t.Errorf("%v 输出 %q 应包含 %q", c.args, out.String(), c.want)
		}
	}

	// 写入的配置必须能被读回，且不含明文密钥。
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "gpt-4o-mini") || !strings.Contains(text, "sk-abcdefghijk") {
		t.Errorf("配置未正确保存:\n%s", text)
	}
	// 输出里不能出现完整密钥。
	if strings.Contains(out.String(), "sk-abcdefghijk") {
		t.Error("API Key 不应明文输出")
	}
}

func TestRunSetConfigRejectsBadBaseURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("COMMAND_AI_HOME", home)

	oldErr := stderr
	stderr = &bytes.Buffer{}
	defer func() { stderr = oldErr }()

	if code := run([]string{"base-url", "not-a-url"}); code != 2 {
		t.Errorf("非法 URL 退出码 = %d, want 2", code)
	}
}

func TestRunVerboseToggles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("COMMAND_AI_HOME", home)

	var out bytes.Buffer
	oldOut := stdout
	stdout = &out
	defer func() { stdout = oldOut }()

	run([]string{"verbose"})
	if !strings.Contains(out.String(), "verbose = true") {
		t.Errorf("首次应打开 verbose, got %q", out.String())
	}
	out.Reset()
	run([]string{"verbose"})
	if !strings.Contains(out.String(), "verbose = false") {
		t.Errorf("再次应关闭 verbose, got %q", out.String())
	}
}

func TestRunConfigShowsMaskedKey(t *testing.T) {
	llm := newFakeLLM("ls")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, "")

	if code := run([]string{"config"}); code != 0 {
		t.Fatalf("退出码 = %d", code)
	}
	out := env.stdout.String()
	if !strings.Contains(out, "sk-t****") {
		t.Errorf("应显示脱敏后的 Key, got %q", out)
	}
	if strings.Contains(out, "sk-test") {
		t.Error("不应显示明文 Key")
	}
}

// TestExecuteYes 覆盖 y 分支：生成 → 执行 → 输出 → Token。
func TestExecuteYes(t *testing.T) {
	llm := newFakeLLM("echo hello-from-llm")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, "y\n")

	code := run([]string{"帮我输出一段文字"})
	if code != 0 {
		t.Fatalf("退出码 = %d, stderr = %s", code, env.stderr)
	}
	out := env.stdout.String()
	if !strings.Contains(out, "Command: echo hello-from-llm") {
		t.Errorf("应展示生成的命令, got:\n%s", out)
	}
	if !strings.Contains(out, "hello-from-llm") {
		t.Errorf("应执行命令并输出结果, got:\n%s", out)
	}
	if !strings.Contains(out, "Token: 10/5") {
		t.Errorf("应显示 Token, got:\n%s", out)
	}

	recs := env.records(t)
	if len(recs) != 1 {
		t.Fatalf("应写入 1 条历史, got %d", len(recs))
	}
	r := recs[0]
	if r.Choice != history.ChoiceYes || r.Command != "echo hello-from-llm" {
		t.Errorf("历史记录不正确: %+v", r)
	}
	if !strings.Contains(r.Output, "hello-from-llm") {
		t.Errorf("历史应包含命令输出, got %q", r.Output)
	}
	if r.InputTokens != 10 || r.OutputTokens != 5 {
		t.Errorf("历史 Token 不正确: %+v", r)
	}
}

// TestExecuteNo 覆盖 n 分支：不执行命令，但仍记录 Token。
func TestExecuteNo(t *testing.T) {
	llm := newFakeLLM("touch /tmp/should-not-exist-command-ai")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, "n\n")

	code := run([]string{"危险操作"})
	if code != 0 {
		t.Fatalf("退出码 = %d", code)
	}
	if !strings.Contains(env.stdout.String(), "Token: 10/5") {
		t.Errorf("取消后仍应显示 Token, got:\n%s", env.stdout.String())
	}

	recs := env.records(t)
	if len(recs) != 1 || recs[0].Choice != history.ChoiceNo {
		t.Fatalf("应记录一次取消: %+v", recs)
	}
	if recs[0].Output != "" {
		t.Errorf("取消时不应有输出, got %q", recs[0].Output)
	}
}

// TestEmptyInputDefaultsToNo 覆盖“空输入默认为 n”。
func TestEmptyInputDefaultsToNo(t *testing.T) {
	llm := newFakeLLM("echo nope")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, "\n")

	if code := run([]string{"需求"}); code != 0 {
		t.Fatalf("退出码 = %d", code)
	}
	recs := env.records(t)
	if len(recs) != 1 || recs[0].Choice != history.ChoiceNo {
		t.Fatalf("空输入应视为取消: %+v", recs)
	}
	if recs[0].Output != "" || recs[0].ExitCode != nil {
		t.Errorf("空输入不应执行命令: %+v", recs[0])
	}
}

// TestExplainThenExecute 覆盖 e 分支：解释后回到 Allow 提示，同一命令。
func TestExplainThenExecute(t *testing.T) {
	llm := newFakeLLM("echo explained")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, "e\ny\n")

	code := run([]string{"解释一下"})
	if code != 0 {
		t.Fatalf("退出码 = %d, stderr=%s", code, env.stderr)
	}
	out := env.stdout.String()
	if !strings.Contains(out, "该命令会列出文件。") {
		t.Errorf("应输出解释, got:\n%s", out)
	}
	// 解释后应再次提示，且命令不变。
	if n := strings.Count(out, "Allow[y/N/e/r]"); n != 2 {
		t.Errorf("解释后应重新提示一次, 共 2 次, got %d:\n%s", n, out)
	}
	if n := strings.Count(out, "Command: echo explained"); n != 1 {
		t.Errorf("解释后不应重复打印命令, got %d 次 Command:\n%s", n, out)
	}
	if !strings.Contains(out, "explained") {
		t.Errorf("确认后应执行命令, got:\n%s", out)
	}
	// 生成 1 次 + 解释 1 次 = 2 次调用；Token 累加。
	if llm.calls != 2 {
		t.Errorf("应调用 LLM 2 次, got %d", llm.calls)
	}
	if !strings.Contains(out, "Token: 20/10") {
		t.Errorf("解释消耗的 Token 应累加, got:\n%s", out)
	}
}

// TestExplainThenCancel 覆盖 e 之后取消。
func TestExplainThenCancel(t *testing.T) {
	llm := newFakeLLM("echo x")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, "e\nn\n")

	if code := run([]string{"解释后取消"}); code != 0 {
		t.Fatalf("退出码 = %d", code)
	}
	if strings.Contains(env.stdout.String(), "echo x\nx") {
		t.Error("取消后不应执行")
	}
	recs := env.records(t)
	if len(recs) != 1 || recs[0].Choice != history.ChoiceNo {
		t.Fatalf("应记录取消: %+v", recs)
	}
}

// TestRegenerate 覆盖 r 分支：重新生成新命令后执行。
func TestRegenerate(t *testing.T) {
	llm := newFakeLLM("ls ~", "ls $HOME")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	// r → 反馈 → y
	env := setup(t, srv.URL, "r\n不要用波浪号\ny\n")

	code := run([]string{"列出家目录"})
	if code != 0 {
		t.Fatalf("退出码 = %d, stderr=%s", code, env.stderr)
	}
	out := env.stdout.String()
	if !strings.Contains(out, "Command: ls ~") {
		t.Errorf("首次应展示原始命令, got:\n%s", out)
	}
	if !strings.Contains(out, "Command: ls $HOME") {
		t.Errorf("重新生成后应展示新命令, got:\n%s", out)
	}
	// 第二次请求必须把上一次的命令和用户反馈带给模型。
	msgs, _ := llm.lastBody["messages"].([]any)
	if len(msgs) < 2 {
		t.Fatalf("消息不足: %+v", llm.lastBody)
	}
	userMsg, _ := msgs[1].(map[string]any)
	content, _ := userMsg["content"].(string)
	if !strings.Contains(content, "ls ~") || !strings.Contains(content, "不要用波浪号") {
		t.Errorf("重新生成请求应携带上一命令与反馈, got:\n%s", content)
	}

	// 历史里应有一条 r 和一条 y。
	recs := env.records(t)
	var sawRegen, sawYes bool
	for _, r := range recs {
		if r.Choice == history.ChoiceRegen {
			sawRegen = true
		}
		if r.Choice == history.ChoiceYes && r.Command == "ls $HOME" {
			sawYes = true
		}
	}
	if !sawRegen || !sawYes {
		t.Errorf("历史应同时包含 r 与 y: %+v", recs)
	}
}

// TestRegenerateLoopIsBounded 防止无限重新生成。
func TestRegenerateLoopIsBounded(t *testing.T) {
	llm := newFakeLLM("ls")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, strings.Repeat("r\n\n", maxRegenerate+3))

	code := run([]string{"需求"})
	if code == 0 {
		t.Errorf("持续的 r 应以非 0 退出而不是死循环, stderr=%s", env.stderr)
	}
	if !strings.Contains(env.stderr.String(), "最大重新生成次数") {
		t.Errorf("应提示达到上限, stderr=%s", env.stderr)
	}
}

// TestLLMFailureIsNotRecorded 网络失败不写历史。
func TestLLMFailureIsNotRecorded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"Authentication Fails"}}`)
	}))
	defer srv.Close()
	env := setup(t, srv.URL, "y\n")

	code := run([]string{"需求"})
	if code == 0 {
		t.Error("鉴权失败应返回非 0")
	}
	if !strings.Contains(env.stderr.String(), "Authentication Fails") {
		t.Errorf("应显示友好错误, stderr=%s", env.stderr.String())
	}
	if recs := env.records(t); len(recs) != 0 {
		t.Errorf("失败请求不应写入历史, got %+v", recs)
	}
}

// TestMissingAPIKeyGivesActionableError 未配置 Key 时给出可操作提示。
func TestMissingAPIKeyGivesActionableError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("COMMAND_AI_HOME", home)
	var errBuf bytes.Buffer
	oldErr, oldIn := stderr, stdin
	stderr, stdin = &errBuf, strings.NewReader("y\n")
	defer func() { stderr, stdin = oldErr, oldIn }()

	code := run([]string{"需求"})
	if code == 0 {
		t.Error("缺少 API Key 应返回非 0")
	}
	if !strings.Contains(errBuf.String(), "api-key") {
		t.Errorf("错误应提示如何配置, got %q", errBuf.String())
	}
}

// TestNonZeroExitCodeIsPropagated 命令失败时透传退出码。
func TestNonZeroExitCodeIsPropagated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("依赖 Unix shell")
	}
	llm := newFakeLLM("exit 7")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, "y\n")

	code := run([]string{"返回非零"})
	if code != 7 {
		t.Errorf("退出码 = %d, want 7", code)
	}
	recs := env.records(t)
	if len(recs) != 1 || recs[0].ExitCode == nil || *recs[0].ExitCode != 7 {
		t.Errorf("历史应记录退出码 7: %+v", recs)
	}
}

// TestUsageCommand 覆盖 usage 子命令。
func TestUsageCommand(t *testing.T) {
	llm := newFakeLLM("echo counted")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, "y\n")

	if code := run([]string{"统计一下", "用量"}); code != 0 {
		t.Fatalf("生成退出码 = %d", code)
	}
	if code := run([]string{"统计一下", "用量"}); code != 0 {
		t.Fatalf("生成退出码 = %d", code)
	}

	env.stdout.Reset()
	if code := run([]string{"usage"}); code != 0 {
		t.Fatalf("usage 退出码 = %d", code)
	}
	out := env.stdout.String()
	if !strings.Contains(out, "今日用量") {
		t.Errorf("默认应统计今日, got:\n%s", out)
	}
	if !strings.Contains(out, "请求次数:     2") {
		t.Errorf("应统计 2 次请求, got:\n%s", out)
	}
	if !strings.Contains(out, "INPUT Token:  20") || !strings.Contains(out, "OUTPUT Token: 10") {
		t.Errorf("Token 汇总不正确, got:\n%s", out)
	}

	env.stdout.Reset()
	if code := run([]string{"usage", "this-year"}); code != 0 {
		t.Fatalf("usage this-year 退出码 = %d", code)
	}
	if !strings.Contains(env.stdout.String(), "本年用量") {
		t.Errorf("应统计本年, got:\n%s", env.stdout.String())
	}

	env.stdout.Reset()
	if code := run([]string{"usage", "all"}); code != 0 {
		t.Fatalf("usage all 退出码 = %d", code)
	}
	if !strings.Contains(env.stdout.String(), "全部用量") {
		t.Errorf("应统计全部, got:\n%s", env.stdout.String())
	}
}

func TestUsageRejectsUnknownPeriod(t *testing.T) {
	env := setup(t, "http://127.0.0.1:1", "")
	if code := run([]string{"usage", "yesterday"}); code != 2 {
		t.Errorf("未知周期退出码 = %d, want 2", code)
	}
	if !strings.Contains(env.stderr.String(), "未知的统计周期") {
		t.Errorf("应提示可用周期, got %q", env.stderr.String())
	}
}

// TestUnquotedNaturalLanguage 未加引号的多词需求应被完整拼接。
func TestUnquotedNaturalLanguage(t *testing.T) {
	llm := newFakeLLM("echo joined")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	setup(t, srv.URL, "y\n")

	run([]string{"帮我", "列出", "当前目录"})

	msgs, _ := llm.lastBody["messages"].([]any)
	userMsg, _ := msgs[1].(map[string]any)
	content, _ := userMsg["content"].(string)
	if !strings.Contains(content, "帮我 列出 当前目录") {
		t.Errorf("多词需求应完整传入, got:\n%s", content)
	}
}

// TestUsageIsNotDoubleCounted 保证记录中的用量是「每步增量」，
// 使用 e / r 时把记录相加不会重复计数。
func TestUsageIsNotDoubleCounted(t *testing.T) {
	llm := newFakeLLM("ls ~", "ls $HOME")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	// 两次生成（每次 10/5）+ 一次执行。
	env := setup(t, srv.URL, "r\n\nn\n")

	if code := run([]string{"列出家目录"}); code != 0 {
		t.Fatalf("退出码 = %d", code)
	}

	recs := env.records(t)
	if len(recs) != 2 {
		t.Fatalf("应有 r 与 n 两条记录, got %d: %+v", len(recs), recs)
	}
	sumIn, sumOut, sumCalls := 0, 0, 0
	for _, r := range recs {
		sumIn += r.InputTokens
		sumOut += r.OutputTokens
		sumCalls += r.LLMCalls
	}
	// 会话累计为 20/10、2 次调用，记录之和必须与之一致。
	if sumIn != 20 || sumOut != 10 {
		t.Errorf("记录 Token 之和 = %d/%d, want 20/10（不应重复计数）", sumIn, sumOut)
	}
	if sumCalls != 2 {
		t.Errorf("记录调用次数之和 = %d, want 2", sumCalls)
	}
	if !strings.Contains(env.stdout.String(), "Token: 20/10") {
		t.Errorf("会话累计 Token 应为 20/10, got:\n%s", env.stdout.String())
	}
}

// TestExplainTokensFoldIntoNextRecord “解释”不单独成记录，
// 其消耗应并入随后的那条记录。
func TestExplainTokensFoldIntoNextRecord(t *testing.T) {
	llm := newFakeLLM("echo x")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, "e\ny\n")

	if code := run([]string{"解释并执行"}); code != 0 {
		t.Fatalf("退出码 = %d", code)
	}

	recs := env.records(t)
	if len(recs) != 1 {
		t.Fatalf("应只有 1 条记录, got %d: %+v", len(recs), recs)
	}
	r := recs[0]
	if r.Choice != history.ChoiceYes {
		t.Errorf("choice = %q, want y", r.Choice)
	}
	// 生成 10/5 + 解释 10/5 = 20/10，共 2 次调用。
	if r.InputTokens != 20 || r.OutputTokens != 10 {
		t.Errorf("Token = %d/%d, want 20/10", r.InputTokens, r.OutputTokens)
	}
	if r.LLMCalls != 2 {
		t.Errorf("LLMCalls = %d, want 2", r.LLMCalls)
	}
}

// TestVerboseModePrintsDiagnostics verbose 打开时应输出诊断信息，且不泄露密钥。
func TestVerboseModePrintsDiagnostics(t *testing.T) {
	llm := newFakeLLM("echo hi")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, "y\n")

	// 打开 verbose。
	if code := run([]string{"verbose"}); code != 0 {
		t.Fatalf("verbose 退出码 = %d", code)
	}
	env.stdout.Reset()

	if code := run([]string{"打个招呼"}); code != 0 {
		t.Fatalf("退出码 = %d", code)
	}
	out := env.stdout.String()
	for _, want := range []string{"chat/completions", "Token:", "耗时", "历史目录"} {
		if !strings.Contains(out, want) {
			t.Errorf("verbose 输出缺少 %q:\n%s", want, out)
		}
	}
	// 诊断信息里绝不能出现 API Key。
	if strings.Contains(out, "sk-test") {
		t.Error("verbose 输出不应包含 API Key")
	}
}

// TestNonVerboseIsQuiet 默认不应输出诊断信息。
func TestNonVerboseIsQuiet(t *testing.T) {
	llm := newFakeLLM("echo hi")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, "y\n")

	run([]string{"打个招呼"})
	if strings.Contains(env.stdout.String(), "chat/completions") {
		t.Errorf("非 verbose 模式不应输出诊断信息:\n%s", env.stdout.String())
	}
}

// ---------- 语言（i18n） ----------

// runCaptured 在指定语言环境下执行一次命令并返回输出。
func runCaptured(t *testing.T, loc string, args ...string) (string, string) {
	t.Helper()
	setLocale(t, loc)
	var out, errBuf bytes.Buffer
	oldOut, oldErr := stdout, stderr
	stdout, stderr = &out, &errBuf
	defer func() { stdout, stderr = oldOut, oldErr }()
	run(args)
	return out.String(), errBuf.String()
}

// TestHelpFollowsLocale help 应随 LANG 切换语言。
func TestHelpFollowsLocale(t *testing.T) {
	home := t.TempDir()
	t.Setenv("COMMAND_AI_HOME", home)

	en, _ := runCaptured(t, "en_US.UTF-8", "help")
	if !strings.Contains(en, "generate and run shell commands") {
		t.Errorf("LANG=en 时 help 应为英文:\n%s", en)
	}
	if strings.Contains(en, "用法:") {
		t.Errorf("LANG=en 时不应出现中文:\n%s", en)
	}

	zh, _ := runCaptured(t, "zh_CN.UTF-8", "help")
	if !strings.Contains(zh, "用自然语言生成并执行 shell 命令") {
		t.Errorf("LANG=zh 时 help 应为中文:\n%s", zh)
	}
	if strings.Contains(zh, "generate and run shell commands") {
		t.Errorf("LANG=zh 时不应出现英文正文:\n%s", zh)
	}
}

// TestLocaleDefaultsToEnglishWhenUnset 未设置 LANG（C locale）时使用英文。
func TestLocaleDefaultsToEnglishWhenUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("COMMAND_AI_HOME", home)

	for _, loc := range []string{"", "C", "POSIX", "fr_FR.UTF-8"} {
		out, _ := runCaptured(t, loc, "help")
		if !strings.Contains(out, "generate and run shell commands") {
			t.Errorf("LANG=%q 时应回退英文:\n%s", loc, out)
		}
	}
}

// TestConfigLanguageOverridesLocale 配置中的显式语言优先于环境变量。
func TestConfigLanguageOverridesLocale(t *testing.T) {
	llm := newFakeLLM("echo hi")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, "y\n")

	// setup 默认写入 language: zh；这里改成 en，同时把环境设成中文。
	cfgPath := filepath.Join(env.home, "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "language: zh") {
		t.Fatalf("setup 应写入 language: zh:\n%s", data)
	}
	if err := os.WriteFile(cfgPath, []byte(strings.Replace(string(data), "language: zh", "language: en", 1)), 0o600); err != nil {
		t.Fatal(err)
	}

	setLocale(t, "zh_CN.UTF-8")
	env.stdout.Reset()
	if code := run([]string{"usage"}); code != 0 {
		t.Fatalf("退出码 = %d", code)
	}
	out := env.stdout.String()
	if !strings.Contains(out, "Requests") {
		t.Errorf("配置 language=en 应压过 LANG=zh:\n%s", out)
	}
	if strings.Contains(out, "请求次数") {
		t.Errorf("配置 language=en 时不应输出中文:\n%s", out)
	}
}

// TestLangCommand 覆盖 lang 子命令。
func TestLangCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("COMMAND_AI_HOME", home)

	// 默认 auto：显示当前语言、配置来源与环境来源。
	out, _ := runCaptured(t, "zh_CN.UTF-8", "lang")
	if !strings.Contains(out, "中文") || !strings.Contains(out, "auto") {
		t.Errorf("lang 应显示状态, got:\n%s", out)
	}

	// 设置为 en。
	out, _ = runCaptured(t, "zh_CN.UTF-8", "lang", "en")
	if !strings.Contains(out, "language = en") {
		t.Errorf("应回显设置结果, got:\n%s", out)
	}
	// 已落盘。
	data, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "language: en") {
		t.Errorf("language 未写入配置:\n%s", data)
	}

	// 显式 en 立即生效，即使 LANG=zh。
	out, _ = runCaptured(t, "zh_CN.UTF-8", "usage")
	if !strings.Contains(out, "Requests") {
		t.Errorf("language=en 应生效:\n%s", out)
	}

	// 非法取值应被拒绝。
	_, errOut := runCaptured(t, "en_US.UTF-8", "lang", "klingon")
	if !strings.Contains(errOut, "zh, en or auto") {
		t.Errorf("非法语言应给出提示, got:\n%s", errOut)
	}
}

// TestUsageLabelsFollowLocale usage 统计标签应随语言切换。
func TestUsageLabelsFollowLocale(t *testing.T) {
	llm := newFakeLLM("echo counted")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()
	env := setup(t, srv.URL, "y\n")

	if code := run([]string{"count"}); code != 0 {
		t.Fatalf("退出码 = %d", code)
	}

	// 切到英文后查看统计。
	cfgPath := filepath.Join(env.home, "config.yaml")
	data, _ := os.ReadFile(cfgPath)
	os.WriteFile(cfgPath, []byte(strings.Replace(string(data), "language: zh", "language: en", 1)), 0o600)

	env.stdout.Reset()
	run([]string{"usage", "all"})
	out := env.stdout.String()
	for _, want := range []string{"all-time usage", "Requests:", "INPUT tokens:", "OUTPUT tokens:", "Total tokens:", "Executed:"} {
		if !strings.Contains(out, want) {
			t.Errorf("英文统计缺少 %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "请求次数") {
		t.Errorf("英文模式下不应出现中文标签:\n%s", out)
	}
}

// TestErrorMessagesAreLocalized 错误提示也应本地化，并保持可操作。
func TestErrorMessagesAreLocalized(t *testing.T) {
	home := t.TempDir()
	t.Setenv("COMMAND_AI_HOME", home)
	// 不写配置 => 缺少 API Key。
	oldIn := stdin
	stdin = strings.NewReader("y\n")
	defer func() { stdin = oldIn }()

	_, en := runCaptured(t, "en_US.UTF-8", "do something")
	if !strings.Contains(en, "error:") || !strings.Contains(en, "api-key") {
		t.Errorf("英文错误提示不正确:\n%s", en)
	}
	if strings.Contains(en, "错误") {
		t.Errorf("英文模式下不应出现中文错误:\n%s", en)
	}

	_, zh := runCaptured(t, "zh_CN.UTF-8", "做点什么")
	if !strings.Contains(zh, "错误:") || !strings.Contains(zh, "api-key") {
		t.Errorf("中文错误提示不正确:\n%s", zh)
	}
}

// TestAllowPromptIsStable 项目书规定的交互标记不随语言变化。
func TestAllowPromptIsStable(t *testing.T) {
	llm := newFakeLLM("echo hi")
	srv := httptest.NewServer(llm.handler())
	defer srv.Close()

	for _, loc := range []string{"en_US.UTF-8", "zh_CN.UTF-8"} {
		env := setup(t, srv.URL, "y\n")
		setLocale(t, loc)
		env.stdout.Reset()
		run([]string{"hi"})
		out := env.stdout.String()
		for _, want := range []string{"Command: echo hi", "Allow[y/N/e/r]", "Token: 10/5"} {
			if !strings.Contains(out, want) {
				t.Errorf("LANG=%s 时缺少固定格式 %q:\n%s", loc, want, out)
			}
		}
	}
}
