# 更新日志

[English](CHANGELOG.md) | [简体中文](CHANGELOG_zh.md)

本项目遵循 [语义化版本](https://semver.org/lang/zh-CN/) 与
[Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 约定。

## [1.1.1] - 2026-09-16

### Added

- 新增 `Makefile`，覆盖完整工作流：`build`、`dist`(六平台交叉编译)、`test`、`cover`、
  `vet`、`fmt`、`fmt-check`、`check`、`tidy`、`install`、`uninstall`、`clean`、
  `run`、`version`。直接执行 `make` 会列出全部目标，`make check` 一次完成
  格式检查、vet 与测试。
  版本号从 `cmd/command-ai/main.go` 读取，保持单一来源；可用
  `make dist VERSION=x.y.z` 覆盖。

### Fixed

- 有 5 处提示仍然硬编码中文、未跟随所选语言，导致 `LANG=C command-ai ""` 输出
  `错误: 需求描述为空` 而不是英文。已修复：
  - 需求为空(`cli.empty_request`)
  - Base URL 校验(`cli.base_url_scheme`)
  - API Key 为空(`cli.api_key_empty`)
  - 模型名为空(`cli.model_empty`)
  - 执行分支中直接打印字面量的一处错误输出
- 新增源码扫描测试：只要 `i18n` 包之外的字符串字面量含有中文就判为失败，
  防止同类遗漏再次发生。此前的「文案 key 完整性」测试无法覆盖这类问题——
  它只检查已经走 `i18n.T` 的字符串，发现不了压根没接进去的字符串。

### Fixed

- 仓库缺少 `internal/history`：`.gitignore` 中的 `history/` 同时匹配了
  `internal/history/`，导致全新克隆无法构建。现已锚定到仓库根
  (`/history/`、`/config.yaml`)。
- `go.sum` 不完整，全新克隆会报 `missing go.sum entry for go.mod file`；
  已用 `go mod tidy` 重新生成。

### Changed

- 全项目统一使用半角括号 `()`，中文文案也不例外：维护者偏好半角写法，
  因此源码、帮助文本与文档一律采用该形式，不再使用全角写法。
- 公开仓库的打包细节：`LICENSE` 补上版权人，CHANGELOG 日期改为真实发布日期。

## [1.1.0] - 2026-09-16

### Added

- **界面双语(中文 / 英文)**
  - 新增 `internal/i18n`：全部用户可见文案集中管理，中英各一份
  - 自动按 POSIX 优先级读取 `LC_ALL` > `LC_MESSAGES` > `LANG` 判定语言；
    `zh*` 为中文，`C`/`POSIX`/未设置/其他语言一律回退英文
  - 新增 `language` 配置项与 `command-ai lang [zh|en|auto]` 子命令；
    配置中的显式设置优先于环境变量，便于在 `LANG=C` 的 cron/容器中固定语言
  - `command-ai config` 显示当前语言及其来源
  - 帮助、用法、报错、统计标签、verbose 诊断信息全部随语言切换
  - 测试新增源码扫描，确保所有 `i18n.T(...)` 用到的文案 key 都已在中英两边定义

### Changed

- 非中文语言环境(如 `LANG=C`)下界面由中文改为英文；此前无论 locale 一律输出中文
- 「解释」的语言规则不变：始终与用户需求的自然语言一致，与界面语言无关

### Notes

- `Command:`、`Token:`、`Thinking`、`Allow[y/N/e/r]` 属项目书规定的固定输出格式，不随语言变化
- 内部错误信息(配置、历史、LLM、执行器)也一并纳入双语

## [1.0.0] - 2026-09-16

首个正式版本，实现项目书的全部里程碑(M1–M6)。

### Added

- **配置管理**
  - `base-url` / `api-key` / `model` / `verbose` 四个配置子命令
  - `config` 子命令查看当前配置(API Key 脱敏显示)
  - `config.yaml` 以 `0600` 权限保存，目录 `0700`；写入采用「临时文件 + 原子改名」
- **命令生成**
  - 兼容 OpenAI Chat Completions 协议的 LLM 客户端(`/chat/completions`)
  - 系统提示词明确声明「不在 shell 中」，禁止 `~`、别名等语法糖与嵌套 shell，
    只输出单行可执行命令
  - 模型输出清洗：剥离 markdown 代码块、语言标记、`$` / `>` 提示符
- **交互流程**
  - `Thinking -` 旋转动画(非终端环境自动静默)
  - `Allow[y/N/e/r]` 状态机：执行 / 取消 / 解释 / 重新生成
  - 空输入默认取消；非法输入重新提示；EOF 安全退出
  - 解释使用与用户相同的语言；解释后回到同一命令的确认提示且不重复打印命令
  - 重新生成支持附加反馈，并把上一次的命令与反馈一并告知模型；
    最多 5 轮防止无限循环
- **命令执行**
  - 通过平台解释器执行(Unix `sh -c` / Windows `cmd /C`)
  - 输出既实时回显终端，又捕获写入历史
  - 子进程不继承 stdin，避免交互式命令挂起
  - 命令退出码透传为进程退出码
- **历史与统计**
  - `history/YYYY-MM-DD.jsonl` 按天追加，文件 `0600`、目录 `0700`
  - 记录时间戳、输入、命令、选择(y/n/e/r)、输出、退出码、Token、模型
  - Token 与调用次数按「每步增量」记录，记录相加即真实总量，
    使用 `e`/`r` 不会重复计数
  - 损坏的 JSON 行会被跳过，不影响统计
  - `usage [today|this-week|this-month|this-year|all]` 统计请求次数与 INPUT/OUTPUT Token
  - 网络或鉴权失败不写入历史
- **信息类**
  - `help` / `version` 子命令
- **工程**
  - 单二进制分发，无运行时依赖
  - `.gitignore` 排除 `config.yaml` 与 `history/`，避免密钥与隐私泄露
  - 测试覆盖全部 internal 包，并包含 CLI 端到端交互测试

### Security

- API Key 从不以明文出现在标准输出或日志中
- 配置与历史文件权限在 Unix 下为 `0600`
- 本工具不提供沙箱、白名单或危险命令拦截；执行前的 `Allow` 确认是唯一兜底

[1.1.1]: https://github.com/command-ai/command-ai/releases/tag/v1.1.1
[1.1.0]: https://github.com/command-ai/command-ai/releases/tag/v1.1.0
[1.0.0]: https://github.com/command-ai/command-ai/releases/tag/v1.0.0
