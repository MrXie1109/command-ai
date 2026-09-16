package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCleanCommand(t *testing.T) {
	cases := []struct{ in, want string }{
		{"ls", "ls"},
		{"  ls  ", "ls"},
		{"```\nls\n```", "ls"},
		{"```bash\nls -la\n```", "ls -la"},
		{"$ ls", "ls"},
		{"> ls", "ls"},
		{"ls\n", "ls"},
		{"\n\nls\n", "ls"},
		{"", ""},
		{"```", ""},
	}
	for _, c := range cases {
		if got := CleanCommand(c.in); got != c.want {
			t.Errorf("CleanCommand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanCommandTakesFirstLine(t *testing.T) {
	got := CleanCommand("ls -la\necho hello")
	if got != "ls -la" {
		t.Errorf("应只取第一行, got %q", got)
	}
}

func TestChatSendsExpectedRequest(t *testing.T) {
	var gotBody chatRequest
	var gotAuth, gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)

		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": "ls -la\n"}}},
			"usage":   map[string]int{"prompt_tokens": 11, "completion_tokens": 4, "total_tokens": 15},
		})
	}))
	defer srv.Close()

	c := New(srv.URL+"/", "sk-secret", "test-model", nil)
	out, u, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if out != "ls -la" {
		t.Errorf("content = %q, want %q", out, "ls -la")
	}
	if u.PromptTokens != 11 || u.CompletionTokens != 4 {
		t.Errorf("usage = %+v", u)
	}
	if gotAuth != "Bearer sk-secret" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, 结尾斜杠应被规整", gotPath)
	}
	if gotBody.Model != "test-model" {
		t.Errorf("model = %q", gotBody.Model)
	}
}

func TestChatHandlesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"message":"Authentication Fails","type":"authentication_error"}}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "bad", "m", nil)
	_, _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !strings.Contains(err.Error(), "Authentication Fails") {
		t.Errorf("错误信息应包含服务端原因, got: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("错误信息应包含状态码, got: %v", err)
	}
}

func TestChatHandlesEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[]}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "k", "m", nil)
	if _, _, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}); err == nil {
		t.Error("空 choices 应报错")
	}
}

func TestChatWithoutAPIKey(t *testing.T) {
	c := New("http://127.0.0.1:1", "", "m", nil)
	_, _, err := c.Chat(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "api-key") {
		t.Errorf("缺少 API Key 应给出可操作提示, got %v", err)
	}
}

func TestChatWithoutModel(t *testing.T) {
	c := New("http://127.0.0.1:1", "k", "", nil)
	_, _, err := c.Chat(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "model") {
		t.Errorf("缺少模型应给出可操作提示, got %v", err)
	}
}

func TestGenerateCommandPromptRules(t *testing.T) {
	var gotBody chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ls $HOME"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "k", "m", nil)
	cmd, _, err := c.GenerateCommand(context.Background(), "列出家目录", "", "")
	if err != nil {
		t.Fatalf("GenerateCommand: %v", err)
	}
	if cmd != "ls $HOME" {
		t.Errorf("command = %q", cmd)
	}
	if len(gotBody.Messages) != 2 || gotBody.Messages[0].Role != "system" {
		t.Fatalf("应包含 system + user 两条消息, got %+v", gotBody.Messages)
	}

	sys := gotBody.Messages[0].Content
	// 提示词必须明确“不在 shell 中”并禁止 ~ 这类语法糖。
	if !strings.Contains(sys, "NOT running inside a shell") {
		t.Error("system 提示词应声明不在 shell 中")
	}
	if !strings.Contains(sys, "$HOME") {
		t.Error("system 提示词应给出 $HOME 的可执行写法")
	}
	if !strings.Contains(sys, "ONLY the command") {
		t.Error("system 提示词应要求只输出命令")
	}
	if !strings.Contains(gotBody.Messages[1].Content, "列出家目录") {
		t.Error("user 消息应包含原始需求")
	}
}

func TestGenerateCommandIncludesFeedbackOnRegenerate(t *testing.T) {
	var gotBody chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		io.WriteString(w, `{"choices":[{"message":{"content":"ls -a $HOME"}}],"usage":{}}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "k", "m", nil)
	if _, _, err := c.GenerateCommand(context.Background(), "列出家目录", "ls ~", "不要用 ~"); err != nil {
		t.Fatalf("GenerateCommand: %v", err)
	}

	user := gotBody.Messages[1].Content
	for _, want := range []string{"ls ~", "不要用 ~", "rejected"} {
		if !strings.Contains(user, want) {
			t.Errorf("重新生成时 user 消息应包含 %q, got:\n%s", want, user)
		}
	}
}

func TestExplainCommandSameLanguageRule(t *testing.T) {
	var gotBody chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		io.WriteString(w, `{"choices":[{"message":{"content":"该命令会列出文件。"}}],"usage":{"prompt_tokens":5,"completion_tokens":6}}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "k", "m", nil)
	out, u, err := c.ExplainCommand(context.Background(), "列出文件", "ls")
	if err != nil {
		t.Fatalf("ExplainCommand: %v", err)
	}
	if out != "该命令会列出文件。" {
		t.Errorf("explain = %q", out)
	}
	if u.PromptTokens != 5 || u.CompletionTokens != 6 {
		t.Errorf("usage = %+v", u)
	}
	if !strings.Contains(gotBody.Messages[0].Content, "SAME LANGUAGE") {
		t.Error("解释用的 system 提示词要求使用与用户相同的语言")
	}
	if !strings.Contains(gotBody.Messages[1].Content, "ls") {
		t.Error("解释请求应包含待解释的命令")
	}
}

func TestCleanCommandFencedWithLanguageTag(t *testing.T) {
	// 回归：早期实现会把 ```bash 的语言标记误当成命令。
	got := CleanCommand("```bash\nls -la\n```")
	if got != "ls -la" {
		t.Errorf("got %q, want %q", got, "ls -la")
	}
	got = CleanCommand("```sh\npwd\n```")
	if got != "pwd" {
		t.Errorf("got %q, want %q", got, "pwd")
	}
}

func TestCleanCommandSingleBacktickWrapped(t *testing.T) {
	if got := CleanCommand("`ls -la`"); got != "ls -la" {
		t.Errorf("got %q, want %q", got, "ls -la")
	}
}
