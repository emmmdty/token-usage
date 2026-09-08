# Contributing

Thanks for considering a contribution! This project keeps things lean:
a single Go binary, few dependencies, and tests that run on every push.

English below · 中文说明见文末

## Getting Started

```bash
git clone https://github.com/emmmdty/token-usage.git
cd token-usage
go build ./cmd/token-usage
go test ./...
```

Go 1.26+ is required (see `go.mod`).

## Before You Push

The pre-commit hook runs gofmt and go vet; CI runs the same plus race tests
on Linux/macOS/Windows:

```bash
gofmt -w .
go vet ./...
go test -race ./...
```

Enable the hook once per clone:

```bash
git config core.hooksPath .githooks
```

## Ground Rules

- **Keep dependencies minimal.** A new direct dependency needs a strong
  justification — this tool is meant to stay a small, auditable binary.
- **User-facing strings go through i18n.** Add keys to *both*
  `internal/i18n/en.json` and `internal/i18n/zh.json`.
- **Tests accompany changes.** Bug fixes get a regression test; new
  providers/features get unit tests (see `internal/provider/*_test.go` for
  the fake-CLI / httptest patterns).
- **Commits follow Conventional Commits** (`feat:`, `fix:`, `docs:`,
  `test:`, `chore:`, `ci:`) — release notes are generated from them.
- **Never commit credentials or real provider output containing account
  identifiers.** Tests use fakes and fixtures only.

## Pull Requests

1. Fork / branch from `main`.
2. Keep the diff focused; one logical change per PR.
3. Fill in the PR template. CI must be green before review.
4. For behavior changes, update both READMEs (`README.md`,
   `README.zh-CN.md`).

---

# 贡献指南（中文）

- 环境要求：Go 1.26+；`go build ./cmd/token-usage && go test ./...` 应直接通过
- 提交前：`gofmt -w . && go vet ./... && go test -race ./...`
- 提交信息使用 Conventional Commits（`feat:` / `fix:` / `docs:` …），发布说明据此自动生成
- 面向用户的文案必须中英两份 i18n 文件同步添加
- 保持依赖克制；新增直接依赖需要充分理由
- 严禁提交真实凭证或含账号信息的真实输出；测试一律使用 fake/fixture
- PR 保持单一逻辑变更，行为变化需同步更新两份 README
