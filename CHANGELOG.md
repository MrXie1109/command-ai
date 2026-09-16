# Changelog

[English](CHANGELOG.md) | [简体中文](CHANGELOG_zh.md)

This project follows [Semantic Versioning](https://semver.org/) and the
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions.

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

### Changed

- Project now uses half-width parentheses `()` consistently, including in Chinese text
  where full-width `（）` is the usual convention. The maintainer prefers the half-width
  form, so it is applied uniformly across the source, help text, and documentation.
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

[1.1.1]: https://github.com/command-ai/command-ai/releases/tag/v1.1.1
[1.1.0]: https://github.com/command-ai/command-ai/releases/tag/v1.1.0
[1.0.0]: https://github.com/command-ai/command-ai/releases/tag/v1.0.0
