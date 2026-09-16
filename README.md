# command-ai

[English](README.md) | [简体中文](README_zh.md)

Generate and run shell commands from natural language.

Describe what you want in plain language; `command-ai` asks an LLM to produce the
corresponding command, shows it to you for confirmation, and then runs it.

```bash
$ command-ai "list the files in the current directory"
Thinking -
Command: ls
Allow[y/N/e/r] y
Documents  Downloads  Music  Pictures  Public  Templates  Videos
Token: 128/12
```

The interface language follows `LANG` / `LC_ALL` / `LC_MESSAGES`: anything starting with
`zh` selects Chinese, everything else (including `C`, `POSIX`, and unset) selects English.
You can pin it with `command-ai lang zh|en|auto`. See [Interface language](#interface-language).

> [!WARNING]
> This tool provides **no safety net**: no sandbox, no allowlist, no dangerous-command
> filtering. It assumes you understand the risk of running commands and accept the
> consequences. The `Allow[y/N/e/r]` confirmation is the only safeguard — always read the
> command before pressing `y`.

---

## Features

- Natural-language driven: no need to memorise command syntax, flags, or platform quirks
- Ships as a single binary with no runtime dependencies
- Bring your own LLM: any provider speaking the OpenAI Chat Completions protocol
- Multi-turn interaction: confirm / cancel / explain / regenerate
- Explicit `Command:` / `Error:` protocol: a refusal is never executed by accident
- Coloured output on a terminal, with `NO_COLOR` and `FORCE_COLOR` support
- Editable prompt template in `template.txt`, next to `config.yaml`
- Bilingual interface (English and Chinese) that follows your system locale
- Lightweight config (YAML) and history (JSONL) with strict file permissions
- Token usage statistics by time period

## Installation

### Build from source

Requires Go 1.18 or newer.

```bash
git clone https://github.com/MrXie1109/command-ai
cd command-ai
make build          # -> dist/command-ai
```

Or without `make`:

```bash
go build -o dist/command-ai ./cmd/command-ai
```

Cross-compile all six targets at once:

```bash
make dist           # -> dist/command-ai-{linux,windows,darwin}-{amd64,arm64}[.exe]
```

Equivalently, by hand:

```bash
# Linux
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -o dist/command-ai-linux-amd64      ./cmd/command-ai
# Windows
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/command-ai-windows-amd64.exe ./cmd/command-ai
# macOS (Apple Silicon)
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o dist/command-ai-darwin-arm64     ./cmd/command-ai
```

`make` reads the version from `cmd/command-ai/main.go`, so `make build` and `make dist`
always stamp the binary with the value in the source. Override it when needed with
`make dist VERSION=2.0.0`.

### Install onto your PATH

```bash
make install        # -> ~/.local/bin/command-ai
```

`PREFIX` defaults to `~/.local`, so `make install PREFIX=/usr/local` installs elsewhere.
There is a matching `make uninstall`.

Or by hand:

```bash
install -m 0755 dist/command-ai ~/.local/bin/command-ai
```

## Quick start

```bash
# 1. Configure a provider (DeepSeek in this example)
command-ai base-url https://api.deepseek.com
command-ai api-key sk-xxxxxxxxxxxxxxxxxxxx
command-ai model deepseek-flash

# 2. Verify the configuration (the API key is masked)
command-ai config

# 3. Start using it
command-ai "show the 5 largest files in the current directory"
```

## Commands

### Execution

```bash
command-ai "your request"
```

The flow is:

1. Show the `Thinking -` spinner
2. Call the LLM to generate a command
3. Show `Command: <generated command>`
4. Prompt with `Allow[y/N/e/r]`
5. Run the command and print its output
6. Show `Token: INPUT/OUTPUT`

Quotes are optional; without them all arguments are joined into a single request:

```bash
command-ai list the files in my home directory
```

### Configuration

| Command | Description |
|---------|-------------|
| `command-ai base-url <url>` | Set the LLM base URL |
| `command-ai api-key <key>` | Set the API key |
| `command-ai model <name>` | Set the model name |
| `command-ai lang [zh\|en\|auto]` | Show or set the interface language |
| `command-ai template [show\|path\|reset]` | View or reset the prompt template |
| `command-ai verbose` | Toggle verbose output |
| `command-ai config` | Show the current configuration (key masked) |

### Informational

| Command | Description |
|---------|-------------|
| `command-ai help` | Show help |
| `command-ai version` | Show the version |

### Usage statistics

```bash
command-ai usage [today|this-week|this-month|this-year|all]
```

| Period | Description |
|--------|-------------|
| `today` | Today's usage (default) |
| `this-week` | This week's usage (weeks start on Monday) |
| `this-month` | This month's usage |
| `this-year` | This year's usage |
| `all` | All-time usage |

Example output:

```
$ command-ai usage this-month
this month usage (2024-05-01 to 2024-05-17)
  Requests:      42
  INPUT tokens:  15320
  OUTPUT tokens: 2871
  Total tokens:  18191
  Executed:      31
```

## Interaction

The model answers in one of two explicitly labelled forms: `Command: <command>` when the
request can be done with a single command, or `Error: <reason>` when it cannot (a
greeting, an insult, something interactive, or anything unsafe). Only a `Command:` reply
is ever executed — an unlabelled reply is treated as an error, so a refusal can never be
handed to the shell by mistake.

For a command the tool prompts with `Allow[y/N/e/r]`:

| Input | Behaviour |
|-------|-----------|
| `y` | Run the command |
| `n` | Cancel (**pressing Enter alone means `n`**) |
| `e` | Explain the command in the language of your request, then ask about the same command again |
| `r` | Regenerate the command (you may add a feedback message), then ask again |

When the model replies `Error:` there is nothing to run, so the prompt becomes
`Allow[N/e/r]`:

| Input | Behaviour |
|-------|-----------|
| `y` | Rejected — you are told there is no command, and the prompt is repeated |
| `n` | Cancel (**pressing Enter alone means `n`**) |
| `e` | Explain why the request cannot be done with one command, and how to rephrase it |
| `r` | Ask the model again (you may add a feedback message) |

Any other input is rejected and the prompt is repeated. The full flow:

```
[input] → Thinking → model reply
                      ├─ Command: <command> → Allow[y/N/e/r]
                      │     ├─ y → run → output → Token
                      │     ├─ n → cancel → Token
                      │     ├─ e → explain command → Allow?
                      │     └─ r → regenerate → Allow?
                      └─ Error: <reason>   → Allow[N/e/r]  (no y)
                            ├─ n → cancel → Token
                            ├─ e → explain the reason → Allow?
                            └─ r → regenerate → Allow?
```

An example using `e` and `r`:

```bash
$ command-ai "list the files in my home directory"
Thinking -
Command: ls $HOME
Allow[y/N/e/r] e
This command lists everything in the current user's home directory.
Allow[y/N/e/r] r
Feedback (optional, press Enter to skip): use the long format
Command: ls -l $HOME
Allow[y/N/e/r] y
...
Token: 312/48
```

> The system prompt tells the model explicitly that it is **not inside a shell** and cannot
> use shell sugar such as `~` or aliases, so it emits directly executable forms like
> `ls $HOME`.

## Prompt template

The prompt sent to the model lives in `template.txt`, in the same directory as
`config.yaml`. It is created with the built-in default on first run, so you can see and
edit exactly what the model receives — no recompilation needed.

```bash
command-ai template          # path, source, and available placeholders
command-ai template show     # print the effective template
command-ai template path     # print the path only: $EDITOR $(command-ai template path)
command-ai template reset    # restore the built-in default
```

The template replaces the built-in system prompt for **command generation**. If the file
is missing or contains only whitespace, the built-in default is used, so deleting the
file is a complete undo. `command-ai template reset` writes the default back.

Placeholders are substituted before the prompt is sent:

| Placeholder | Value |
|-------------|-------|
| `{{os}}` | Target operating system, e.g. `linux` |
| `{{arch}}` | Target architecture, e.g. `amd64` |
| `{{shell}}` | Interpreter the command is handed to, e.g. `sh`, `cmd.exe` |
| `{{request}}` | The user's natural-language request |
| `{{previous}}` | The previous command or refusal (empty on the first round) |
| `{{feedback}}` | The feedback typed after `r` (may be empty) |

Unknown placeholders are left untouched rather than blanked, so a typo is visible instead
of silently disappearing.

Two notes:

- The template affects command generation only. The `e` (explain) prompts stay built in,
  because they carry their own contract (answer in the language of the request).
- If your template omits `{{os}}` / `{{arch}}` / `{{shell}}`, the model will not know the
  target platform, which is what keeps it from emitting `~` on Unix or wrong paths on
  Windows. The default template includes them.

The file is written with mode `0600` like `config.yaml`, and is excluded by `.gitignore`.

## Colours

Output is coloured when stdout is a terminal:

| Element | Style |
|---------|-------|
| `Command:` label | bold, command in cyan |
| `Error:` label and text | bold red |
| `Allow[...]` prompt | yellow |
| `Token:` summary and diagnostics | dim |

Colour is disabled automatically when the output is not a terminal, so pipes and log files
stay clean. It also honours the usual environment variables:

| Variable | Effect |
|----------|--------|
| `NO_COLOR` (non-empty) | Always disable colour, per [no-color.org](https://no-color.org); wins over the others |
| `FORCE_COLOR` (non-empty) | Always enable colour, even when redirected to a file |
| `CLICOLOR_FORCE=1` | Same as `FORCE_COLOR` |

## Interface language

The interface is available in **English** and **Chinese** only.

### Automatic detection

At startup the locale environment variables are read in POSIX priority order:

```
LC_ALL  >  LC_MESSAGES  >  LANG
```

Values look like `zh_CN.UTF-8`, `en_US.UTF-8@euro`, `C`, or `POSIX`:

| Value | Interface language |
|-------|--------------------|
| Starts with `zh` (`zh`, `zh_CN`, `zh-TW`, `zh_Hans`, …) | Chinese |
| `C`, `POSIX` | English (the C locale convention) |
| Any other language (`en_US`, `fr_FR`, `ja_JP`, …) | English |
| Unset | English |

```bash
$ LANG=C command-ai usage
today usage (2024-05-17 to 2024-05-17)
  Requests:      6
  INPUT tokens:  2561
  OUTPUT tokens: 529
  Total tokens:  3090
  Executed:      3

$ LANG=zh_CN.UTF-8 command-ai usage
今日用量(2024-05-17 ~ 2024-05-17)
  请求次数:     6
  INPUT Token:  2561
  OUTPUT Token: 529
  合计 Token:   3090
  实际执行:     3
```

### Setting it manually

If `LANG` is unreliable (cron jobs and containers often report `C`), pin the language in
the configuration:

```bash
command-ai lang zh     # always Chinese
command-ai lang en     # always English
command-ai lang auto   # follow the environment again
command-ai lang        # show the current language and where it came from
```

An explicit setting in the config takes precedence over the environment, so Chinese is
preserved even when `LANG=C`. It is stored in the `language` field of `config.yaml`:

```yaml
language: auto   # auto | zh | en
```

### Relationship to explanation language

The interface language only affects prompts, help text, and error messages. The `e` branch
always explains a command in **the language of your request**, independently of the
interface language: an English request gets an English explanation, a Chinese request gets
a Chinese one.

> `Command:`, `Token:`, `Thinking`, and `Allow[y/N/e/r]` are fixed output formats required
> by the project specification and never change with the language.

## Configuration and data

### Config file

Default path: `$XDG_CONFIG_HOME/command-ai/config.yaml`, or
`~/.config/command-ai/config.yaml` when `XDG_CONFIG_HOME` is unset.

```yaml
base_url: https://api.deepseek.com
api_key: sk-xxxxxxxxxxxxxxxxxxxx
model: deepseek-flash
language: auto   # auto | zh | en
verbose: false
```

- File mode `0600`, directory mode `0700` (equivalent ACL protection on Windows)
- Written via a temporary file plus an atomic rename, so an interrupted write cannot
  corrupt the configuration
- Prefer changing it through the `command-ai` subcommands rather than editing by hand

### History

Default path: `~/.local/share/command-ai/history/YYYY-MM-DD.jsonl`.

One JSON record per line:

```json
{
  "timestamp": "2024-05-17T10:30:00+08:00",
  "input": "list the files in the current directory",
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

`command` is empty when the model refused; the reason is recorded in `model_error` instead,
so a refusal is distinguishable from an execution failure (`error`):

```json
{
  "timestamp": "2024-05-17T10:31:00+08:00",
  "input": "tell me a joke",
  "command": "",
  "choice": "n",
  "model_error": "讲笑话不是可以用单条命令完成的任务。",
  "input_tokens": 464,
  "output_tokens": 20,
  "llm_calls": 1,
  "model": "deepseek-flash"
}
```

`input_tokens`, `output_tokens`, and `llm_calls` describe the usage of **that step only**,
measured since the previous record, rather than a running session total. Summing all records
therefore yields the true total and nothing is double-counted when you use `e` (explain) or
`r` (regenerate). Explanations do not produce a record of their own; their cost is folded
into the record that follows.

- File mode `0600`, directory mode `0700`
- Network and authentication failures are never written to history
- Corrupt lines are skipped so they cannot break the statistics

### Environment variables

| Variable | Description |
|----------|-------------|
| `COMMAND_AI_HOME` | Override the root directory for config and history (useful for isolation and testing) |
| `COMMAND_AI_CONFIG` | Override only the config file path |
| `COMMAND_AI_HOME` | Also determines where `template.txt` is created |
| `LANG` / `LC_ALL` / `LC_MESSAGES` | Select the interface language; priority `LC_ALL` > `LC_MESSAGES` > `LANG` |
| `NO_COLOR` | Disable coloured output |
| `FORCE_COLOR` / `CLICOLOR_FORCE` | Force coloured output even when redirected |

## Project layout

```
command-ai/
├── cmd/
│   └── command-ai/
│       ├── main.go          # CLI entry point, subcommand dispatch, interaction state machine
│       └── main_test.go     # end-to-end interaction tests
├── internal/
│   ├── config/              # config read/write (YAML, 0600)
│   ├── prompt/              # editable prompt template and placeholders
│   ├── history/             # history records (JSONL, one file per day)
│   ├── i18n/                # English/Chinese strings and locale detection
│   ├── llm/                 # LLM client and prompts
│   ├── executor/            # command execution and output capture
│   ├── ui/                  # spinner and the Allow prompt
│   └── usage/               # usage statistics
├── go.mod
├── Makefile                 # build, test, cross-compile, install
├── README.md
├── README_zh.md
├── CHANGELOG.md
├── CHANGELOG_zh.md
├── LICENSE
└── .gitignore
```

## Development

Every task is available through `make`; run `make` with no arguments to list them.

```bash
make check      # gofmt check + go vet + tests (run this before committing)
make test       # tests only
make cover      # tests plus a total coverage figure
make fmt        # format all Go sources
make build      # host binary into dist/
make dist       # all six cross-compiled binaries
make clean      # remove dist/ and coverage.out
make release TAG=v1.2.0   # tag and push a release
```

## Releases

Tagging and publishing is a two-step process:

```bash
# 1. bump the version in cmd/command-ai/main.go, update the changelogs, commit
# 2. tag and push
make release TAG=v1.2.0

# 3. build the artifacts and attach them to a GitHub Release
make dist
gh release create v1.2.0 \
  --title "command-ai v1.2.0" --notes-file CHANGELOG.md \
  dist/command-ai-*
```

Released versions are listed on
[the releases page](https://github.com/MrXie1109/command-ai/releases); each one ships
binaries for Linux, Windows, and macOS on both amd64 and arm64, plus a `SHA256SUMS` file.
```

The underlying commands, if you prefer them directly:

```bash
go test ./...       # tests
gofmt -l -w .       # format
go vet ./...        # static analysis
```

> With gccgo rather than the official Go toolchain, `go vet` and cross-compilation may be
> unavailable; in that case run the tests with `go test -vet=off ./...`, or
> `make test GO_TEST_FLAGS=-vet=off`.

## Security notes

- The API key is never printed in plaintext to stdout or written to logs
  (`command-ai config` masks it)
- `config.yaml` and `history/*.jsonl` use file mode `0600`
- `.gitignore` excludes `config.yaml` and `history/` so secrets and private data never
  reach the repository
- The tool provides **no** sandbox, allowlist, or dangerous-command filtering

## Non-goals

- ❌ No command safety net, sandbox, or allowlist
- ❌ No bundled LLM service and no API key
- ❌ Not a shell replacement; does not implement pipes or redirection itself
- ❌ No cross-session context memory (interaction is scoped to a single request)

## License

[MIT](LICENSE)
