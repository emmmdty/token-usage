English | [简体中文](README.zh-CN.md)

# token-usage

Multi-provider AI coding tool usage monitor — query quota usage and available models across OpenCode Go, Claude, Codex, Volcano Engine, and user-defined custom coding-plan providers.

## Features

- **Multi-provider support** — OpenCode Go, Claude, Codex, Volcano Engine (Coding/Agent Plan), plus custom providers (Z.ai GLM, Kimi, MiniMax, DeepSeek, openai-compatible)
- **Provider → account hierarchy** — every provider supports multiple accounts
- **Local login detection** — Claude/Codex/Opencode logins are auto-detected and offered for reuse without touching those files
- **Custom providers with live validation** — a custom provider is only saved if its quota query actually works
- **Quota monitoring** — 5-hour rolling, weekly, and monthly windows in one view
- **Model listing** — see available models for your plan
- **Diagnostics** — `doctor` checks config, keyring, arkcli, network, and connectivity
- **Shell aliases** — install/uninstall the `tu` shortcut
- **JSON output** — machine-readable output with `--json`
- **Secure storage** — API keys stored in system keyring, with fallback to encrypted config
- **Concurrent queries** — parallel quota fetching with configurable concurrency

## Supported Providers

| Provider | Auth Method | Quota Windows |
|----------|-------------|---------------|
| **OpenCode Go** | API Key | 5h / Weekly / Monthly |
| **Claude** | OAuth (auto-detected from Claude Code) | 5h / 7d |
| **Codex** | OAuth (auto-detected from Codex) | 5h / 7d |
| **Volcano Engine (Coding Plan)** | API key probe or official `arkcli` | session / Weekly / Monthly |
| **Z.ai GLM (Coding Plan)** | API Key | token/credit limits |
| **Kimi (Coding Plan)** | API Key | 5h / Weekly |
| **MiniMax (Coding Plan)** | API Key | per-model remaining quota |
| **DeepSeek** | API Key | balance (pay-as-you-go) |
| **openai-compatible** | API Key | billing endpoints when available |

### Volcano Engine quota

Volcano (Ark) does not expose quota windows through plain API keys. `token-usage`
therefore works in two modes:

1. **arkcli mode (recommended)** — if the official [ark-cli](https://github.com/volcengine/ark-cli)
   is installed and logged in (`npm i -g @volcengine/ark-cli && arkcli auth login`),
   full 5h/weekly/monthly windows are fetched via `arkcli usage plan`. The CLI is
   invoked with `ARKCLI_NO_UPDATE_NOTIFIER=1` and agent caller metadata, so no
   silent updates or interactive prompts are triggered.
2. **Probe mode** — otherwise the API key (auto-read from `~/.config/opencode/opencode.json`
   or entered manually) is validated with a 1-token completion; usage shows `n/a`
   with a hint to install arkcli.

> Note: the ark-cli installer injects skills into local AI agents by default.
> Set `ARKCLI_SKIP_POSTINSTALL=1` during installation to skip that.

#### Multiple Volcano accounts

`opencode.json` can hold one provider entry per Volcano account (e.g. one
per phone number). `token-usage` reads them all and registers one account
per entry, each pinned to its own key:

```bash
token-usage account add volcengine --use-local
# Detected 2 Volcano accounts in opencode.json. Add all of them? [Y/n]
```

Quota windows come from arkcli when the account's key matches a logged-in
profile (matched by key suffix); otherwise the key is probed and windows
show `n/a`. You can also bind an arkcli profile explicitly with `--profile`,
or pick a single opencode.json entry with `--opencode-provider <id>`.

Because arkcli keeps a **single login per HOME**, tracking two Volcano
accounts with full windows at the same time needs one arkcli HOME each:

```bash
mkdir -p ~/.config/token-usage/arkcli-homes/coding-plan{,-2}
HOME=~/.config/token-usage/arkcli-homes/coding-plan   arkcli auth login volc-sso  # phone 1
HOME=~/.config/token-usage/arkcli-homes/coding-plan-2 arkcli auth login volc-sso  # phone 2

token-usage account add volcengine coding-plan \
  --plan coding --use-local \
  --opencode-provider Volcano-Engine-coding-plan \
  --arkcli-home ~/.config/token-usage/arkcli-homes/coding-plan
```

Each account then queries through its own login, independently.

## Installation

### Go install

```bash
go install github.com/emmmdty/token-usage/cmd/token-usage@latest
```

Requires Go 1.26.6+. If `~/go/bin` is not in your PATH, use:

```bash
GOBIN=~/.local/bin go install github.com/emmmdty/token-usage/cmd/token-usage@latest
```

### Download binary

Download the latest release from [GitHub Releases](https://github.com/emmmdty/token-usage/releases).

Available for Linux, macOS, and Windows (amd64/arm64).

### Build from source

```bash
git clone https://github.com/emmmdty/token-usage.git
cd token-usage
go build -o token-usage ./cmd/token-usage/
```

## Quick start

```bash
# Just run it — shows usage across all configured providers
token-usage

# Or use the alias
tu
```

## Usage

### Providers

```bash
# List providers and their accounts
token-usage provider list

# Add a provider (interactive menu: presets or custom)
token-usage provider add

# Add the Volcano Engine coding plan detected from opencode.json
token-usage provider add volcengine --plan coding --use-local

# Add a custom provider non-interactively (saved only if quota query works)
token-usage provider add custom --name my-glm --query-type zai-glm \
  --base-url https://api.z.ai --key <api-key>

# Disable/remove
token-usage provider disable claude
token-usage provider remove my-glm
```

### Accounts (per provider)

```bash
# Add an account to a provider (presets detect local logins first)
token-usage account add claude
token-usage account add opencode work

# Register one account per Volcano entry found in opencode.json
token-usage account add volcengine --use-local

# List accounts grouped by provider (-> marks the current one)
token-usage account list

# Mark the current account for a provider
token-usage account switch volcengine coding

# For opencode this also updates opencode's own auth.json
token-usage account switch opencode work

# Validate that quota querying works for an account
token-usage account test opencode/work

# Export/import metadata (no secrets)
token-usage account export
token-usage account import accounts.json
```

### Quota

```bash
# View quota for every account of every provider
token-usage quota

# Filter: provider, account, or provider/account
token-usage quota volcengine
token-usage quota -n work
token-usage quota -n opencode/work

# JSON output
token-usage quota --json
```

### Models

```bash
# List available models (opencode provider)
token-usage models
```

### Diagnostics

```bash
token-usage doctor
```

`doctor` exits non-zero when any check fails (warnings do not affect the
exit code), so it can be used in scripts and CI.

### Shell alias

```bash
token-usage alias install     # install the 'tu' alias
token-usage alias uninstall
```

### Version & updates

```bash
token-usage version
token-usage update
```

## Internationalization (i18n)

token-usage supports Chinese (zh) and English (en). English is the default.

### Switching language

```bash
# View current language
tu lang

# Switch to Chinese
tu lang zh

# Switch to English
tu lang en
```

### Per-run language override

```bash
# Use Chinese for this run only (does not persist)
token-usage --lang zh quota

# Environment variable (higher priority than config)
TOKEN_USAGE_LANG=zh token-usage quota
```

### Language priority

1. `--lang` flag (highest)
2. `TOKEN_USAGE_LANG` environment variable
3. `config.yaml` `language` field (set by `tu lang`)
4. System `LANG`/`LC_ALL` (zh* → zh, otherwise en)
5. Default: en

## Short aliases

| Command | Alias |
|---------|-------|
| `providers` | `p` |
| `provider` | `pr` |
| `account` | `a` |
| `account add` | `aa` |
| `account list` | `al` |
| `account remove` | `ar` |
| `account switch` | `sw` |
| `account test` | `t` |
| `account export` | `ae` |
| `account import` | `ai` |
| `quota` | `q` |
| `models` | `m` |
| `current` | `cc` |
| `token-usage` | `tu` (shell alias) |

## Global flags

| Flag | Description |
|------|-------------|
| `-j, --json` | JSON output |
| `-n, --account` | Specify account |
| `-o, --output` | Output to file |
| `--no-color` | Disable color output |

## Configuration

Config file: `~/.config/token-usage/config.yaml`

```yaml
version: "3"
providers:
  opencode:
    enabled: true
    default_account: work
    accounts:
      work:
        source: manual        # key stored in the credential store
        key_id: "abc123"      # last 6 chars only
      personal:
        source: manual
  claude:
    enabled: true
    creds_path: ~/.claude/.credentials.json
    accounts:
      local:
        source: local         # read from the Claude Code login
  codex:
    enabled: true
    auth_path: ~/.codex/auth.json
    accounts:
      local:
        source: local
  volcengine:
    enabled: true
    accounts:
      coding-plan:
        source: local         # API key auto-read from opencode.json
        plan: coding          # coding | agent
custom:
  my-glm:
    query_type: zai-glm       # zai-glm | kimi | minimax | deepseek | openai-compatible
    base_url: https://api.z.ai
    enabled: true
    accounts:
      main:
        source: manual
color_thresholds:
  warning: 50
  danger: 80
max_concurrent_requests: 5
use_master_password: false
```

### Configuration options

| Option | Default | Description |
|--------|---------|-------------|
| `providers.*.enabled` | true | Enable/disable provider |
| `providers.*.endpoint` | - | Custom API endpoint |
| `providers.*.default_account` | - | Current-account marker |
| `custom.*.query_type` | required | Built-in query implementation |
| `color_thresholds.warning` | 50 | Quota percentage to trigger warning color |
| `color_thresholds.danger` | 80 | Quota percentage to trigger danger color |
| `max_concurrent_requests` | 5 | Max parallel API requests |
| `use_master_password` | false | Use custom master password for encrypted storage |
| `language` | - | Display language: `zh` (Chinese) or `en` (English) |

## Environment variables

| Variable | Description |
|----------|-------------|
| `NO_COLOR` | Disable color output (see [no-color.org](https://no-color.org)) |
| `TOKEN_USAGE_MASTER_PASSWORD` | Master password for encrypted storage |
| `TOKEN_USAGE_KEYRING_PASSWORD` | Password for keyring file backend |
| `TOKEN_USAGE_LANG` | Display language override (`zh` or `en`) |
| `ARKCLI_SKIP_POSTINSTALL` | Skip ark-cli installer side effects (agent skill injection) |
| `TOKEN_USAGE_KEYRING_DISABLED` | Disable system keyring probing and use the encrypted file backend (headless/CI environments) |

## Security

- API keys are stored in the system keyring (Linux: Secret Service, macOS: Keychain, Windows: Credential Manager)
- Config file only stores the last 6 characters of each key ID (`sk-...XXXXXX`)
- Falls back to AES-256-GCM encrypted config file if keyring is unavailable
- Master password option for encrypted config
- Config and secrets files use restrictive permissions (0600)

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error (usage, auth, network, config, or keyring) |

## License

MIT
