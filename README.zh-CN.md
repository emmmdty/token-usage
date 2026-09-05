[English](README.md) | 简体中文

# token-usage

多供应商 AI 编程工具用量监控——统一查询 OpenCode Go、Claude、Codex、火山引擎以及用户自定义编码计划供应商的配额用量与可用模型。

## 功能特性

- **多供应商支持** —— OpenCode Go、Claude、Codex、火山引擎（Coding/Agent Plan），以及自定义供应商（Z.ai GLM、Kimi、MiniMax、DeepSeek、openai 兼容）
- **供应商 → 账户层级** —— 每个供应商支持多个账户
- **本地登录检测** —— 自动检测 Claude/Codex/Opencode 的本地登录并提议复用，不改动这些文件本身
- **自定义供应商实时校验** —— 只有配额查询真正可用的自定义供应商才会被保存
- **配额监控** —— 5 小时滚动、每周、每月窗口一屏尽览
- **模型列表** —— 查看你的套餐内可用模型
- **诊断** —— `doctor` 检查配置、keyring、arkcli、网络与连通性
- **Shell 别名** —— 一键安装/卸载 `tu` 快捷方式
- **JSON 输出** —— `--json` 获得机器可读输出
- **安全存储** —— API 密钥存入系统 keyring，不可用时回退到加密配置文件
- **并发查询** —— 并行拉取配额，并发数可配置

## 支持的供应商

| 供应商 | 认证方式 | 配额窗口 |
|----------|-------------|---------------|
| **OpenCode Go** | API Key | 5h / 每周 / 每月 |
| **Claude** | OAuth（自动检测 Claude Code 登录） | 5h / 7d |
| **Codex** | OAuth（自动检测 Codex 登录） | 5h / 7d |
| **火山引擎（Coding Plan）** | API key 探测或官方 `arkcli` | 会话 / 每周 / 每月 |
| **Z.ai GLM（Coding Plan）** | API Key | token/credit 限额 |
| **Kimi（Coding Plan）** | API Key | 5h / 每周 |
| **MiniMax（Coding Plan）** | API Key | 按模型剩余配额 |
| **DeepSeek** | API Key | 余额（按量付费） |
| **openai 兼容** | API Key | 计费端点（如可用） |

### 火山引擎配额

火山方舟不提供基于普通 API key 的配额窗口查询。`token-usage` 因此工作在两种模式下：

1. **arkcli 模式（推荐）** —— 如果安装并登录了官方 [ark-cli](https://github.com/volcengine/ark-cli)
   （`npm i -g @volcengine/ark-cli && arkcli auth login`），则通过 `arkcli usage plan`
   获取完整的 5h/每周/每月窗口。调用时会注入 `ARKCLI_NO_UPDATE_NOTIFIER=1` 与
   agent 调用方元数据，因此不会触发静默更新或交互式提示。
2. **探测模式** —— 否则使用 API key（自动读取 `~/.config/opencode/opencode.json`
   或手动输入）通过 1-token 补全校验密钥；用量显示 `n/a` 并提示安装 arkcli。

> 注意：ark-cli 安装器默认会向本地 AI agent 注入 skills。安装时设置
> `ARKCLI_SKIP_POSTINSTALL=1` 可跳过该行为。

## 安装

### Go install

```bash
go install github.com/emmmdty/token-usage/cmd/token-usage@latest
```

需要 Go 1.26.6+。如果 `~/go/bin` 不在 PATH 中：

```bash
GOBIN=~/.local/bin go install github.com/emmmdty/token-usage/cmd/token-usage@latest
```

### 下载二进制

从 [GitHub Releases](https://github.com/emmmdty/token-usage/releases) 下载最新版本。

支持 Linux、macOS、Windows（amd64/arm64）。

### 从源码构建

```bash
git clone https://github.com/emmmdty/token-usage.git
cd token-usage
go build -o token-usage ./cmd/token-usage/
```

## 快速开始

```bash
# 直接运行 —— 显示所有已配置供应商的用量
token-usage

# 或使用别名
tu
```

## 使用方法

### 供应商

```bash
# 列出供应商及其账户
token-usage provider list

# 添加供应商（交互式菜单：预设或自定义）
token-usage provider add

# 添加从 opencode.json 检测到的火山引擎编码计划
token-usage provider add volcengine --plan coding --use-local

# 非交互式添加自定义供应商（配额查询可用时才会保存）
token-usage provider add custom --name my-glm --query-type zai-glm \
  --base-url https://api.z.ai --key <api-key>

# 停用/删除
token-usage provider disable claude
token-usage provider remove my-glm
```

### 账户（按供应商）

```bash
# 给供应商添加账户（预设会先检测本地登录）
token-usage account add claude
token-usage account add opencode work

# 按供应商分组列出账户（-> 标记当前账户）
token-usage account list

# 标记供应商的当前账户
token-usage account switch volcengine coding

# 对 opencode 同时会更新 opencode 自己的 auth.json
token-usage account switch opencode work

# 校验账户的配额查询是否可用
token-usage account test opencode/work

# 导出/导入元数据（不含密钥）
token-usage account export
token-usage account import accounts.json
```

### 配额

```bash
# 查看每个供应商所有账户的配额
token-usage quota

# 过滤：供应商、账户，或 供应商/账户
token-usage quota volcengine
token-usage quota -n work
token-usage quota -n opencode/work

# JSON 输出
token-usage quota --json
```

### 模型

```bash
# 列出可用模型（opencode 供应商）
token-usage models
```

### 诊断

```bash
token-usage doctor
```

`doctor` 在任何检查项失败时以非零码退出（WARN 警告不影响退出码），可用于脚本与 CI。

### Shell 别名

```bash
token-usage alias install     # 安装 'tu' 别名
token-usage alias uninstall
```

### 版本与更新

```bash
token-usage version
token-usage update
```

## 国际化（i18n）

token-usage 支持中文（zh）和英文（en）。默认英文。

### 切换语言

```bash
# 查看当前语言
tu lang

# 切换到中文
tu lang zh

# 切换到英文
tu lang en
```

### 单次运行覆盖语言

```bash
# 仅本次运行使用中文（不持久化）
token-usage --lang zh quota

# 环境变量（优先级高于配置）
TOKEN_USAGE_LANG=zh token-usage quota
```

### 语言优先级

1. `--lang` 标志（最高）
2. `TOKEN_USAGE_LANG` 环境变量
3. `config.yaml` 的 `language` 字段（由 `tu lang` 设置）
4. 系统 `LANG`/`LC_ALL`（zh* → zh，否则 en）
5. 默认：en

## 短别名

| 命令 | 别名 |
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
| `token-usage` | `tu`（shell 别名） |

## 全局标志

| 标志 | 说明 |
|------|-------------|
| `-j, --json` | JSON 输出 |
| `-n, --account` | 指定账户 |
| `-o, --output` | 输出到文件 |
| `--no-color` | 关闭彩色输出 |

## 配置

配置文件：`~/.config/token-usage/config.yaml`

```yaml
version: "3"
providers:
  opencode:
    enabled: true
    default_account: work
    accounts:
      work:
        source: manual        # 密钥存于凭据存储
        key_id: "abc123"      # 仅存最后 6 位
      personal:
        source: manual
  claude:
    enabled: true
    creds_path: ~/.claude/.credentials.json
    accounts:
      local:
        source: local         # 读取 Claude Code 登录
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
        source: local         # API key 自动读取自 opencode.json
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

### 配置项

| 选项 | 默认值 | 说明 |
|--------|---------|-------------|
| `providers.*.enabled` | true | 启用/停用供应商 |
| `providers.*.endpoint` | - | 自定义 API 端点 |
| `providers.*.default_account` | - | "当前账户"标记 |
| `custom.*.query_type` | 必填 | 内置查询实现 |
| `color_thresholds.warning` | 50 | 触发警告颜色的配额百分比 |
| `color_thresholds.danger` | 80 | 触发危险颜色的配额百分比 |
| `max_concurrent_requests` | 5 | 最大并行 API 请求数 |
| `use_master_password` | false | 加密存储使用自定义主密码 |
| `language` | - | 显示语言：`zh`（中文）或 `en`（英文） |

## 环境变量

| 变量 | 说明 |
|----------|-------------|
| `NO_COLOR` | 关闭彩色输出（见 [no-color.org](https://no-color.org)） |
| `TOKEN_USAGE_MASTER_PASSWORD` | 加密存储的主密码 |
| `TOKEN_USAGE_KEYRING_PASSWORD` | keyring 文件后端的密码 |
| `TOKEN_USAGE_LANG` | 显示语言覆盖（`zh` 或 `en`） |
| `ARKCLI_SKIP_POSTINSTALL` | 跳过 ark-cli 安装器的副作用（agent skill 注入） |
| `TOKEN_USAGE_KEYRING_DISABLED` | 禁用系统 keyring 探测，改用加密文件后端（适用于无 keyring 的环境） |

## 安全

- API 密钥存储在系统 keyring（Linux: Secret Service，macOS: Keychain，Windows: Credential Manager）
- 配置文件只保存密钥 ID 的最后 6 位（`sk-...XXXXXX`）
- keyring 不可用时回退到 AES-256-GCM 加密配置文件
- 加密配置可选用主密码
- 配置与密钥文件使用受限权限（0600）

## 退出码

| 退出码 | 含义 |
|------|---------|
| 0 | 成功 |
| 1 | 错误（用法、认证、网络、配置、keyring，或 doctor 检查失败） |

## 许可证

MIT
