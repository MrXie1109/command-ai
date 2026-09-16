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
		return "", usage, errors.New("未配置 API Key，请先执行: command-ai api-key <key>")
	}
	if c.Model == "" {
		return "", usage, errors.New("未配置模型，请先执行: command-ai model <name>")
	}

	body, err := json.Marshal(chatRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: 0,
		Stream:      false,
	})
	if err != nil {
		return "", usage, fmt.Errorf("构造请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", usage, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", usage, fmt.Errorf("请求 LLM 失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", usage, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", usage, fmt.Errorf("LLM 返回错误 (%d): %s", resp.StatusCode, extractError(data))
	}

	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", usage, fmt.Errorf("解析响应失败: %w", err)
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return "", cr.Usage, fmt.Errorf("LLM 返回错误: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", cr.Usage, errors.New("LLM 未返回任何结果")
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
		return "(空响应)"
	}
	return s
}

// systemPrompt 描述“生成命令”的约束。
func systemPrompt() string {
	return fmt.Sprintf(`You are a shell command generator for a CLI tool named command-ai.

Environment: %s/%s, user shell: %s

Rules:
1. You are NOT running inside a shell, and you must NOT invoke one yourself.
   Never emit `+"`sh -c ...`"+`, `+"`bash -c ...`"+`, `+"`cmd /C ...`"+` or any other nested shell
   wrapper. command-ai already hands your command to the platform shell, so a
   nested shell is redundant and will be rejected.
2. Never use shell-only syntax sugar or aliases. In particular write `+"`$HOME`"+` instead of
   `+"`~`"+`, and use `+"`%%USERPROFILE%%`"+` for the home directory on Windows.
   Other banned forms: history expansion (`+"`!!`"+`), interactive built-ins, and
   aliases that only exist in an interactive shell.
3. Output ONLY the command itself, on a single line, with no explanation, no markdown
   code fences, no leading "$" or ">", and no trailing commentary.
4. The command must be directly executable by the OS as-is.
5. Prefer POSIX/common tools that exist on the target platform.
6. Refuse briefly (in the same language as the user) if the request is not something
   that can be done with a single command.

Examples:
  "list files in my home directory" -> ls $HOME
  "show disk usage"                 -> df -h

If the user's request is impossible or unsafe to express as one command, reply with a
single short sentence explaining that instead of a command.`, runtime.GOOS, runtime.GOARCH, shellName())
}

func shellName() string {
	if runtime.GOOS == "windows" {
		return "cmd.exe"
	}
	return "sh"
}

// GenerateCommand 让 LLM 根据自然语言需求生成一条命令。
//
// previous 和 feedback 仅在“重新生成”时非空，用于让模型避开上一次的结果。
func (c *Client) GenerateCommand(ctx context.Context, request, previous, feedback string) (string, Usage, error) {
	msgs := []Message{{Role: "system", Content: systemPrompt()}}

	var user strings.Builder
	fmt.Fprintf(&user, "Request: %s\n", request)
	if previous != "" {
		fmt.Fprintf(&user, "\nThe previous suggestion was rejected:\n%s\n", previous)
	}
	if feedback != "" {
		fmt.Fprintf(&user, "\nUser feedback: %s\n", feedback)
	}
	if previous != "" || feedback != "" {
		user.WriteString("\nGenerate a different, better command that addresses the feedback.\n")
	}
	user.WriteString("\nReturn ONLY the command.")

	msgs = append(msgs, Message{Role: "user", Content: user.String()})

	out, usage, err := c.Chat(ctx, msgs)
	if err != nil {
		return "", usage, err
	}
	return CleanCommand(out), usage, nil
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

// CleanCommand 清理模型输出：去掉 markdown 代码块、提示符与多余空行。
func CleanCommand(s string) string {
	s = strings.TrimSpace(s)

	lines := strings.Split(s, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		// 跳过空行与代码块围栏（含 ```bash 这类带语言标记的围栏）。
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
