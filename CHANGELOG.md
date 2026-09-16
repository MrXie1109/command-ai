# Changelog

[English](CHANGELOG.md) | [简体中文](CHANGELOG_zh.md)

This project follows [Semantic Versioning](https://semver.org/) and the
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions.

## [1.4.0] - 2026-09-16

### Fixed

- **Module path now matches the repository.** `go.mod` declared
  `github.com/command-ai/command-ai` while the repository lives at
  `github.com/MrXie1109/command-ai`, so installing from the remote failed with
  `module declares its path as ... but was required as ...`. The module path and all
  import paths were updated, which makes `go install` work:

  ```bash
  go install github.com/MrXie1109/command-ai/cmd/command-ai@latest
  ```

### Changed

- **The default template is more compact**: 32 lines down to 20, roughly 2200 characters
  down to 990, with every constraint kept. Same behaviour at a lower token cost per
  request.
- The examples section is now bilingual for the `Error:` case. A Chinese-only refusal
  example biased the model into answering Chinese refusals even for English requests;
  with an English example alongside it, the refusal language correctly follows the
  request again.

## [1.3.0] - 2026-09-16

### Added

- **Editable prompt template.** The system prompt used for command generation now lives in
  `template.txt`, next to `config.yaml`. It is created with the built-in default on first
  run, so what the model receives is visible and editable without recompiling. The file
  replaces the built-in system prompt; deleting it (or emptying it) restores the default.
- **Placeholders** substituted before the prompt is sent: `{{os}}`, `{{arch}}`,
  `{{shell}}`, `{{request}}`, `{{previous}}`, `{{feedback}}`. Unknown placeholders are
  left untouched so typos stay visible.
- **`command-ai template [show|path|reset]`** to inspect the path and source, print the
  effective template, or restore the built-in default.
- New `internal/prompt` package owning the default template, placeholder rendering, and
  file handling (mode `0600`, `0700` directory).

### Changed

- The default system prompt now has a single definition shared by the built-in fallback and
  the generated `template.txt`, so the two can no longer drift apart.
- `template.txt` is excluded by `.gitignore` alongside `config.yaml`.

### Fixed

- Changelog links pointed at a placeholder repository path; they now point at the real one.

### Notes

- The template applies to command generation only; the `e` (explain) prompts stay built in
  because they carry their own contract about answering in the request's language.
- A template that omits `{{os}}` / `{{arch}}` / `{{shell}}` no longer tells the model the
  target platform, which is what prevents `~` on Unix and wrong paths on Windows.

## [1.2.0] - 2026-09-16

### Added

- **Explicit `Command:` / `Error:` reply protocol.** The model must now label every reply:
  `Command: <command>` when the request can be done with one command, `Error: <reason>`
  when it cannot. Only a `Command:` reply is executable; an unlabelled reply is treated as
  an error. The system prompt tells the model to use `Error:` for chit-chat, greetings,
  insults, interactive sessions, and anything unsafe.
- **`Allow[N/e/r]` prompt.** On an `Error:` reply there is nothing to run, so the prompt
  omits `y`; pressing `y` is rejected with an explanation and the prompt repeats. `e`
  explains why the request could not be turned into a command and how to rephrase it,
  while `n` and `r` keep their usual meaning.
- **Coloured console output** — bold/cyan `Command:`, bold red `Error:`, yellow prompt,
  dim token summary. Colour is enabled only when stdout is a terminal and honours
  `NO_COLOR` (wins), `FORCE_COLOR`, and `CLICOLOR_FORCE=1`.
- `model_error` field in history records, distinguishing a model refusal from an execution
  failure (`error`).

### Fixed

- **A refusal was executed as a command.** Replying `Error:`-style text such as
  "I can't help with that." was previously treated as a command and handed to the shell,
  producing `unexpected EOF while looking for matching \`\'\'` and exit code 2.

## [1.1.1] - 2026-09-16

### Added

- `Makefile` covering the whole workflow: `build`, `dist` (six cross-compiled targets),
  `test`, `cover`, `vet`, `fmt`, `fmt-check`, `check`, `tidy`, `install`, `uninstall`,
  `clean`, `run`, and `version`. Running `make` with no arguments lists them.
  `make check` runs formatting, vet, and the tests in one go.
  The version is read from `cmd/command-ai/main.go`, so there is a single source of
  truth; override it with `make dist VERSION=x.y.z`.

### Fixed

- Five messages were still hardcoded in Chinese and ignored the selected language, so
  `LANG=C command-ai ""` printed `错误: 需求描述为空` instead of English. Fixed:
  - an empty request (`cli.empty_request`)
  - base URL validation (`cli.base_url_scheme`)
  - empty API key (`cli.api_key_empty`)
  - empty model name (`cli.model_empty`)
  - an error path in the execution branch that printed a bare literal
- Added a source-scanning test that fails the build if any string literal outside the
  `i18n` package contains Chinese characters, so this class of omission cannot recur.
  This is the guard that the earlier key-completeness test could not provide: that test
  only checked strings already routed through `i18n.T`, not strings that never were.

### Fixed

- `internal/history` was missing from the repository: the `.gitignore` entry `history/`
  also matched `internal/history/`, so a fresh clone could not build. The pattern is now
  anchored to the repository root (`/history/`, `/config.yaml`).
- `go.sum` was incomplete and a fresh clone failed with
  `missing go.sum entry for go.mod file`; regenerated with `go mod tidy`.

### Changed

- Half-width parentheses `()` are now used consistently, including in Chinese text where
  the full-width form is the usual convention. The maintainer prefers the half-width
  style, so it is applied uniformly across source, help text, and documentation.
- Packaging details for the public repository: `LICENSE` now names the copyright holder,
  and the changelog dates reflect the actual release date.

## [1.1.0] - 2026-09-16

### Added

- **Bilingual interface (English / Chinese)**
  - New `internal/i18n` package: every user-visible string is centralised with an English
    and a Chinese variant
  - Locale is detected automatically in POSIX priority order,
    `LC_ALL` > `LC_MESSAGES` > `LANG`; values starting with `zh` select Chinese, while
    `C`, `POSIX`, unset, and any other language fall back to English
  - New `language` config key and `command-ai lang [zh|en|auto]` subcommand; an explicit
    config value takes precedence over the environment, which keeps Chinese in cron jobs
    and containers that report `LANG=C`
  - `command-ai config` now shows the current language and where it came from
  - Help, usage, error messages, statistics labels, and verbose diagnostics all follow the
    selected language
  - A test scans the source for every `i18n.T(...)` key and asserts it is defined in both
    languages

### Changed

- Non-Chinese locales (for example `LANG=C`) now get an English interface; previously the
  output was Chinese regardless of locale
- The rule for explanations is unchanged: they always use the language of the user's
  request, independently of the interface language

### Notes

- `Command:`, `Token:`, `Thinking`, and `Allow[y/N/e/r]` are fixed output formats required
  by the project specification and never change with the language
- Error messages from the internal packages (config, history, llm, executor) are localised
  as well

## [1.0.0] - 2026-09-16

First stable release, covering every milestone (M1–M6) of the project specification.

### Added

- **Configuration**
  - `base-url`, `api-key`, `model`, and `verbose` subcommands
  - `config` subcommand to inspect the current configuration (API key masked)
  - `config.yaml` written with mode `0600` inside a `0700` directory, using a temporary
    file plus an atomic rename
- **Command generation**
  - LLM client compatible with the OpenAI Chat Completions protocol (`/chat/completions`)
  - System prompt states explicitly that it is not inside a shell, forbids `~`, aliases,
    and nested shells, and requires a single executable line
  - Output cleaning strips markdown code fences, language tags, and `$` / `>` prompts
- **Interaction**
  - `Thinking -` spinner (silent when stdout is not a terminal)
  - `Allow[y/N/e/r]` state machine: run / cancel / explain / regenerate
  - Empty input means cancel; invalid input re-prompts; EOF exits safely
  - Explanations use the user's language and return to the confirmation prompt for the same
    command without reprinting it
  - Regeneration accepts feedback and passes the previous command plus that feedback to the
    model; capped at 5 rounds to prevent infinite loops
- **Execution**
  - Runs through the platform shell (Unix `sh -c`, Windows `cmd /C`)
  - Output is streamed to the terminal and captured for history at the same time
  - Child processes never inherit stdin, so interactive commands cannot hang the tool
  - The command's exit code becomes the process exit code
- **History and statistics**
  - `history/YYYY-MM-DD.jsonl` appended per day, file mode `0600`, directory mode `0700`
  - Records timestamp, input, command, choice (y/n/e/r), output, exit code, tokens, model
  - Tokens and call counts are stored as per-step deltas, so summing records yields the true
    total without double-counting after `e` or `r`
  - Corrupt JSON lines are skipped and do not break the statistics
  - `usage [today|this-week|this-month|this-year|all]` reports request counts and
    INPUT/OUTPUT tokens
  - Network and authentication failures are never written to history
- **Informational**
  - `help` and `version` subcommands
- **Engineering**
  - Single-binary distribution with no runtime dependencies
  - `.gitignore` excludes `config.yaml` and `history/` to keep secrets and private data out
    of the repository
  - Unit tests for every internal package plus end-to-end CLI interaction tests

### Security

- The API key never appears in plaintext on stdout or in logs
- Config and history files use mode `0600` on Unix
- The tool provides no sandbox, allowlist, or dangerous-command filtering; the `Allow`
  confirmation before execution is the only safeguard

[1.4.0]: https://github.com/MrXie1109/command-ai/releases/tag/v1.4.0
[1.3.0]: https://github.com/MrXie1109/command-ai/releases/tag/v1.3.0
[1.2.0]: https://github.com/MrXie1109/command-ai/releases/tag/v1.2.0
[1.1.1]: https://github.com/MrXie1109/command-ai/releases/tag/v1.1.1
[1.1.0]: https://github.com/MrXie1109/command-ai/releases/tag/v1.1.0
[1.0.0]: https://github.com/MrXie1109/command-ai/releases/tag/v1.0.0
