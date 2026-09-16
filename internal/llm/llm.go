// Package llm 实现兼容 OpenAI Chat Completions 协议的客户端，
// 并负责构造“生成命令 / 解释命令”两类提示词。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/command-ai/command-ai/internal/i18n"
	"github.com/command-ai/command-ai/internal/prompt"
)

const (
	// requestTimeout 是单次请求的超时时间。
	requestTimeout = 60 * time.Second
	// maxErrBody 是错误响应体的最大读取长度。
	maxErrBody = 4 << 10
)

// Message 是一条对话消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Usage 是一次请求的 Token 消耗。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Client 是 LLM 客户端。
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client

	// SystemTemplate 是「生成命令」使用的 system 提示词模板。
	// 为空时使用 prompt.Default()。
	SystemTemplate string
}

// New 创建一个客户端。httpClient 为 nil 时使用默认客户端。
func New(baseURL, apiKey, model string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    httpClient,
	}
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	Stream      bool      `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Chat 发送一次对话请求，返回回复内容与 Token 消耗。
func (c *Client) Chat(ctx context.Context, messages []Message) (string, Usage, error) {
	var usage Usage
	if c.APIKey == "" {
		return "", usage, errors.New(i18n.T("llm.no_api_key"))
	}
	if c.Model == "" {
		return "", usage, errors.New(i18n.T("llm.no_model"))
	}

	body, err := json.Marshal(chatRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: 0,
		Stream:      false,
	})
	if err != nil {
		return "", usage, fmt.Errorf(i18n.T("llm.build_failed"), err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", usage, fmt.Errorf(i18n.T("llm.build_failed"), err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", usage, fmt.Errorf(i18n.T("llm.request_failed"), err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", usage, fmt.Errorf(i18n.T("llm.read_failed"), err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", usage, fmt.Errorf(i18n.T("llm.http_error"), resp.StatusCode, extractError(data))
	}

	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", usage, fmt.Errorf(i18n.T("llm.parse_failed"), err)
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return "", cr.Usage, fmt.Errorf(i18n.T("llm.api_error"), cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", cr.Usage, errors.New(i18n.T("llm.no_choices"))
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), cr.Usage, nil
}

// extractError 从错误响应体中尽量提取可读信息。
func extractError(data []byte) string {
	if len(data) > maxErrBody {
		data = data[:maxErrBody]
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err == nil && cr.Error != nil && cr.Error.Message != "" {
		return cr.Error.Message
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return i18n.T("llm.empty_response")
	}
	return s
}

// ReplyKind 区分模型的两种回复。
type ReplyKind int

const (
	// KindCommand 表示模型给出了一条可执行命令。
	KindCommand ReplyKind = iota
	// KindError 表示模型无法给出命令，附带了原因说明。
	KindError
)

// Reply 是模型对一次需求的结构化回复。
type Reply struct {
	Kind ReplyKind
	Text string // KindCommand 时为命令本身；KindError 时为原因说明
}

// IsCommand 报告本次回复是否带有可执行命令。
func (r Reply) IsCommand() bool { return r.Kind == KindCommand }

// commandLabels / errorLabels 是模型可能使用的标签。
//
// 提示词要求用英文标签，但模型有时会跟着用户语言走，因此一并接受中文写法。
var (
	commandLabels = []string{"command:", "command：", "命令:", "命令："}
	errorLabels   = []string{"error:", "error：", "错误:", "错误："}
)

// systemPrompt 渲染本次「生成命令」使用的 system 提示词。
//
// 内容来自 SystemTemplate(用户的 template.txt)，为空时回退到内置默认模板。
func (c *Client) systemPrompt(d prompt.Data) string {
	tpl := c.SystemTemplate
	if strings.TrimSpace(tpl) == "" {
		tpl = prompt.Default()
	}
	return prompt.Render(tpl, d)
}

// envData 填充与运行环境相关的占位符。
func envData(request, previous, feedback string) prompt.Data {
	return prompt.Data{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Shell:    shellName(),
		Request:  request,
		Previous: previous,
		Feedback: feedback,
	}
}

func shellName() string {
	if runtime.GOOS == "windows" {
		return "cmd.exe"
	}
	return "sh"
}

// cutLabel 判断 line 是否以某个标签开头，返回标签之后的内容。
func cutLabel(line string, labels []string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	for _, lb := range labels {
		if strings.HasPrefix(lower, lb) {
			return strings.TrimSpace(trimmed[len(lb):]), true
		}
	}
	return "", false
}

// splitLines 返回去掉空行与 markdown 围栏后的各行。
func splitLines(s string) []string {
	out := make([]string, 0, 4)
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "```") {
			continue
		}
		out = append(out, t)
	}
	return out
}

// ParseReply 把模型的原始输出解析为 Reply。
//
// 只有当回复被显式标注为 "Command:" 时才视为可执行命令。无法识别标签时
// 一律按 Error 处理(fail-safe)：宁可让用户按 r 重试，也不执行一段
// 未经标注的文本——这正是 "fuck you" 被当命令执行的根因。
func ParseReply(raw string) Reply {
	lines := splitLines(raw)
	if len(lines) == 0 {
		return Reply{Kind: KindError, Text: ""}
	}

	first := lines[0]
	if rest, ok := cutLabel(first, commandLabels); ok {
		// 标签之后可能换行写命令，此时取下一行。
		if rest == "" && len(lines) > 1 {
			rest = lines[1]
		}
		return Reply{Kind: KindCommand, Text: CleanCommand(rest)}
	}
	if rest, ok := cutLabel(first, errorLabels); ok {
		// Error 的说明可能跨行，合并为一段。
		if len(lines) > 1 {
			rest = strings.TrimSpace(rest + " " + strings.Join(lines[1:], " "))
		}
		return Reply{Kind: KindError, Text: rest}
	}

	// 没有可识别的标签：按 Error 处理，不执行。
	return Reply{Kind: KindError, Text: strings.Join(lines, " ")}
}

// GenerateCommand 让 LLM 根据自然语言需求生成命令或给出拒答原因。
//
// reply 是上一次的回复、feedback 是用户反馈，二者仅在“重新生成”时非空。
func (c *Client) GenerateCommand(ctx context.Context, request string, reply Reply, feedback string) (Reply, Usage, error) {
	msgs := []Message{{Role: "system", Content: c.systemPrompt(envData(request, reply.Text, feedback))}}

	var user strings.Builder
	fmt.Fprintf(&user, "Request: %s\n", request)
	if reply.Text != "" {
		if reply.IsCommand() {
			fmt.Fprintf(&user, "\nYour previous command was rejected:\n%s\n", reply.Text)
		} else {
			fmt.Fprintf(&user, "\nYou previously replied that no command can be given:\n%s\n", reply.Text)
		}
	}
	if feedback != "" {
		fmt.Fprintf(&user, "\nUser feedback: %s\n", feedback)
	}
	if reply.Text != "" || feedback != "" {
		user.WriteString("\nTry again and address the feedback.\n")
	}
	user.WriteString("\nReply with \"Command: ...\" or \"Error: ...\".")

	msgs = append(msgs, Message{Role: "user", Content: user.String()})

	raw, usage, err := c.Chat(ctx, msgs)
	if err != nil {
		return Reply{}, usage, err
	}
	return ParseReply(raw), usage, nil
}

// ExplainCommand 让 LLM 用与用户相同的语言解释一条命令。
func (c *Client) ExplainCommand(ctx context.Context, request, command string) (string, Usage, error) {
	msgs := []Message{
		{Role: "system", Content: `You explain shell commands to the user.

Rules:
1. Answer in the SAME LANGUAGE as the user's request.
2. Explain what the command does, its important options, and any side effects
   (file writes, deletions, network access, privilege requirements).
3. Be concise: at most 3 short sentences, no markdown headings.`},
		{Role: "user", Content: fmt.Sprintf("User request: %s\nCommand: %s\n\nExplain this command in the user's language.", request, command)},
	}
	out, usage, err := c.Chat(ctx, msgs)
	if err != nil {
		return "", usage, err
	}
	return strings.TrimSpace(out), usage, nil
}

// ExplainError 让 LLM 说明为什么该需求无法用单条命令完成，并尽量给出改写建议。
func (c *Client) ExplainError(ctx context.Context, request, reason string) (string, Usage, error) {
	msgs := []Message{
		{Role: "system", Content: `You explain to a user why their request could not be
turned into a single shell command.

Rules:
1. Answer in the SAME LANGUAGE as the user's request.
2. Explain the obstacle briefly and, when possible, say how to rephrase the request
   so that it can be done with one command.
3. Be concise: at most 3 short sentences, no markdown headings.`},
		{Role: "user", Content: fmt.Sprintf("User request: %s\nYour reason for refusing: %s\n\nExplain this to the user in their language.", request, reason)},
	}
	out, usage, err := c.Chat(ctx, msgs)
	if err != nil {
		return "", usage, err
	}
	return strings.TrimSpace(out), usage, nil
}

// CleanCommand 清理模型输出：去掉 markdown 代码块、提示符与多余空行。
func CleanCommand(s string) string {
	s = strings.TrimSpace(s)

	lines := strings.Split(s, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		// 跳过空行与代码块围栏(含 ```bash 这类带语言标记的围栏)。
		if t == "" || strings.HasPrefix(t, "```") {
			continue
		}
		t = strings.TrimPrefix(t, "$ ")
		t = strings.TrimPrefix(t, "> ")
		t = strings.TrimSpace(t)
		// 处理被一对反引号包裹的单个命令，例如 `ls -la`。
		if len(t) > 1 && strings.HasPrefix(t, "`") && strings.HasSuffix(t, "`") {
			t = strings.TrimSpace(strings.Trim(t, "`"))
		}
		if t == "" {
			continue
		}
		cleaned = append(cleaned, t)
	}
	if len(cleaned) == 0 {
		return ""
	}
	// 约定只输出单行命令；若模型仍返回多行，取第一行有效内容。
	return cleaned[0]
}
