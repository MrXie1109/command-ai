# command-ai

[English](README.md) | [简体中文](README_zh.md)

用自然语言驱动 AI 生成并执行 shell 命令的 CLI 工具。

你只需要用日常语言描述需求，`command-ai` 会调用 LLM 生成对应的命令，展示给你确认，然后执行。

```bash
$ command-ai "帮我列出当前目录下的文件"
Thinking -
Command: ls
Allow[y/N/e/r] y
公共  模板  视频  图片  文档  下载  音乐  桌面
Token: 128/12
```

界面语言会自动跟随 `LANG` / `LC_ALL` / `LC_MESSAGES`：`zh*` 显示中文，其余（含 `C`、`POSIX`、
未设置）显示英文。也可以用 `command-ai lang zh|en|auto` 固定语言。详见[界面语言](#界面语言)。

> [!WARNING]
> 本项目**不提供任何安全防护**：没有沙箱、没有白名单、没有危险命令拦截。
> 它假设你清楚自己在做什么，并自行承担执行命令的后果。
> `Allow[y/N/e/r]` 确认环节是唯一的兜底，请务必看清命令再按 `y`。

---

## 特性

- 自然语言驱动，减少记忆命令语法、参数与平台差异的负担
- 单二进制分发，无运行时依赖
- 用户自备 LLM：兼容 OpenAI Chat Completions 协议的服务商均可
- 多轮交互：确认 / 取消 / 解释 / 重新生成
- 界面中英双语，自动跟随系统语言环境
- 轻量配置（YAML）与历史（JSONL），文件权限严格
- 按时间维度统计 Token 消耗

## 安装

### 从源码构建

需要 Go 1.18 或更高版本。

```bash
git clone <repo-url> command-ai
cd command-ai
go build -o command-ai ./cmd/command-ai
```

交叉编译单二进制文件：

```bash
# Linux
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -o dist/command-ai-linux-amd64      ./cmd/command-ai
# Windows
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/command-ai-windows-amd64.exe ./cmd/command-ai
# macOS (Apple Silicon)
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o dist/command-ai-darwin-arm64     ./cmd/command-ai
```

### 安装到 PATH

```bash
install -m 0755 command-ai ~/.local/bin/command-ai
```

## 快速开始

```bash
# 1. 配置服务商（以 DeepSeek 为例）
command-ai base-url https://api.deepseek.com
command-ai api-key sk-xxxxxxxxxxxxxxxxxxxx
command-ai model deepseek-flash

# 2. 确认配置（API Key 会脱敏显示）
command-ai config

# 3. 开始使用
command-ai "查看当前目录下最大的 5 个文件"
```

## 命令接口

### 执行类

```bash
command-ai "需求描述"
```

流程：

1. 显示 `Thinking -` 旋转动画
2. 调用 LLM 生成命令
3. 显示 `Command: <生成的命令>`
4. 提示 `Allow[y/N/e/r]`
5. 执行命令并输出结果
6. 显示 `Token: INPUT/OUTPUT`

需求描述可以不加引号，此时全部参数会被拼成一条需求：

```bash
command-ai 帮我列出家目录下的文件
```

### 配置类

| 命令 | 说明 |
|------|------|
| `command-ai base-url <url>` | 设置 LLM 服务商 Base URL |
| `command-ai api-key <key>` | 设置 API Key |
| `command-ai model <name>` | 设置模型名称 |
| `command-ai lang [zh\|en\|auto]` | 查看或设置界面语言 |
| `command-ai verbose` | 切换详细输出模式 |
| `command-ai config` | 查看当前配置（Key 脱敏） |

### 信息类

| 命令 | 说明 |
|------|------|
| `command-ai help` | 显示帮助信息 |
| `command-ai version` | 显示版本号 |

### 统计类

```bash
command-ai usage [today|this-week|this-month|this-year|all]
```

| 参数 | 说明 |
|------|------|
| `today` | 今日用量（默认） |
| `this-week` | 本周用量（周一为一周开始） |
| `this-month` | 本月用量 |
| `this-year` | 本年用量 |
| `all` | 全部用量 |

输出示例：

```
$ command-ai usage this-month
本月用量（2024-05-01 ~ 2024-05-17）
  请求次数:     42
  INPUT Token:  15320
  OUTPUT Token: 2871
  合计 Token:   18191
  实际执行:     31
```

## 交互说明

生成命令后，工具会提示 `Allow[y/N/e/r]`：

| 输入 | 行为 |
|------|------|
| `y` | 执行该命令 |
| `n` | 取消（**直接回车等同于 `n`**） |
| `e` | 用与需求相同的语言解释该命令，然后再次询问同一个命令 |
| `r` | 重新生成命令（可附加一段反馈），然后再次询问 |

其他输入会被拒绝并重新提示。完整流程：

```
[输入] → Thinking → 生成命令 → Allow?
                                  ├─ y → 执行 → 输出 → Token
                                  ├─ n → 取消 → Token
                                  ├─ e → 解释 → Allow?
                                  └─ r → 重新生成 → Allow?
```

一个使用 `e` 与 `r` 的例子：

```bash
$ command-ai "帮我列出~目录下的文件"
Thinking -
Command: ls $HOME
Allow[y/N/e/r] e
这个命令会列出当前用户的家目录下的所有文件。
Allow[y/N/e/r] r
Feedback (可选，回车跳过): 用长格式
Command: ls -l $HOME
Allow[y/N/e/r] y
...
Token: 312/48
```

> 提示词中已明确告知模型它**不在 shell 中**，无法使用 `~`、别名等 shell 语法糖，
> 因此它会输出 `ls $HOME` 这类可直接执行的形式。

## 界面语言

界面提供**中文**与**英文**两种语言，不会出现其他语言。

### 自动判定

启动时按 POSIX 优先级读取语言环境变量：

```
LC_ALL  >  LC_MESSAGES  >  LANG
```

取值形如 `zh_CN.UTF-8`、`en_US.UTF-8@euro`、`C`、`POSIX`：

| 取值 | 界面语言 |
|------|----------|
| 以 `zh` 开头（`zh`、`zh_CN`、`zh-TW`、`zh_Hans`…） | 中文 |
| `C`、`POSIX` | 英文（C locale 惯例） |
| 其他任何语言（`en_US`、`fr_FR`、`ja_JP`…） | 英文 |
| 未设置 | 英文 |

```bash
$ LANG=C command-ai usage
today usage (2024-05-17 to 2024-05-17)
  Requests:      6
  INPUT tokens:  2561
  OUTPUT tokens: 529
  Total tokens:  3090
  Executed:      3

$ LANG=zh_CN.UTF-8 command-ai usage
今日用量（2024-05-17 ~ 2024-05-17）
  请求次数:     6
  INPUT Token:  2561
  OUTPUT Token: 529
  合计 Token:   3090
  实际执行:     3
```

### 手动指定

如果 `LANG` 不可靠（例如 cron、容器里往往是 `C`），可以在配置中固定语言：

```bash
command-ai lang zh     # 固定中文
command-ai lang en     # 固定英文
command-ai lang auto   # 恢复跟随环境变量
command-ai lang        # 查看当前语言及来源
```

配置里的显式设置**优先于**环境变量，这样即使 `LANG=C` 也能保持中文。
写入 `config.yaml` 的 `language` 字段：

```yaml
language: auto   # auto | zh | en
```

### 与「解释」语言的区别

界面语言只影响提示、帮助与报错文案。`e` 分支的命令解释始终使用**与你的需求相同的语言**，
与界面语言无关：用英文提问就得到英文解释，用中文提问就得到中文解释。

> `Command:`、`Token:`、`Thinking`、`Allow[y/N/e/r]` 是项目书规定的固定输出格式，
> 不随语言变化。

## 配置与数据

### 配置文件

默认路径：`$XDG_CONFIG_HOME/command-ai/config.yaml`（未设置时为 `~/.config/command-ai/config.yaml`）。

```yaml
base_url: https://api.deepseek.com
api_key: sk-xxxxxxxxxxxxxxxxxxxx
model: deepseek-flash
language: auto   # auto | zh | en
verbose: false
```

- 文件权限 `0600`，目录权限 `0700`（Windows 下由系统 ACL 等效保护）
- 写入采用「临时文件 + 原子改名」，避免中断导致配置损坏
- 建议始终通过 `command-ai` 子命令修改，而不是手工编辑

### 历史记录

默认路径：`~/.local/share/command-ai/history/YYYY-MM-DD.jsonl`。

每行一条 JSON 记录：

```json
{
  "timestamp": "2024-05-17T10:30:00+08:00",
  "input": "帮我列出当前目录下的文件",
  "command": "ls",
  "choice": "y",
  "output": "a.txt\nb.txt\n",
  "exit_code": 0,
  "input_tokens": 128,
  "output_tokens": 12,
  "llm_calls": 1,
  "model": "deepseek-flash"
}
```

`input_tokens` / `output_tokens` / `llm_calls` 记录的是**自上一条记录以来**这一步的用量，
而不是整个会话的累计值。这样把所有记录相加就等于真实总量，使用 `e`（解释）或
`r`（重新生成）时也不会重复计数。“解释”不单独成一条记录，其消耗会并入随后的那条记录。

- 文件权限 `0600`，目录权限 `0700`
- 网络或鉴权失败不写入历史
- 损坏的行会被自动跳过，不影响统计

### 环境变量

| 变量 | 说明 |
|------|------|
| `COMMAND_AI_HOME` | 覆盖配置与历史数据的存放根目录（便于测试与隔离） |
| `COMMAND_AI_CONFIG` | 仅覆盖配置文件路径 |
| `LANG` / `LC_ALL` / `LC_MESSAGES` | 选择界面语言，优先级 `LC_ALL` > `LC_MESSAGES` > `LANG` |

## 项目结构

```
command-ai/
├── cmd/
│   └── command-ai/
│       ├── main.go          # CLI 入口、子命令分发、交互状态机
│       └── main_test.go     # 端到端交互测试
├── internal/
│   ├── config/              # 配置读写（YAML，0600）
│   ├── history/             # 历史记录（JSONL，按天分文件）
│   ├── i18n/                # 中英文文案与语言环境判定
│   ├── llm/                 # LLM 客户端与提示词
│   ├── executor/            # 命令执行与输出捕获
│   ├── ui/                  # spin 动画与 Allow 提示
│   └── usage/               # 用量统计
├── go.mod
├── README.md
├── README_zh.md
├── CHANGELOG.md
├── CHANGELOG_zh.md
├── LICENSE
└── .gitignore
```

## 开发

```bash
# 运行全部测试
go test ./...

# 格式化
gofmt -l -w .

# 静态检查
go vet ./...
```

> 若使用 gccgo 而非官方 Go 工具链，`go vet` 与跨平台编译可能不可用，
> 此时请用 `go test -vet=off ./...` 运行测试。

## 安全说明

- API Key 从不明文输出到标准输出或日志（`command-ai config` 会脱敏）
- `config.yaml` 与 `history/*.jsonl` 权限为 `0600`
- `.gitignore` 已排除 `config.yaml` 与 `history/`，避免密钥与隐私入库
- 本工具**不提供**沙箱、白名单或危险命令拦截

## 非目标

- ❌ 不提供命令安全防护、沙箱、白名单
- ❌ 不内置 LLM 服务，不提供 API Key
- ❌ 不替代 shell，不实现管道/重定向等 shell 特性
- ❌ 不做跨会话的上下文记忆（仅单次请求内交互）

## 许可证

[MIT](LICENSE)
