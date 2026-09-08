# Security Policy

token-usage reads and stores provider credentials (API keys, OAuth tokens),
so security reports are taken seriously.

## Supported Versions

| Version | Supported |
| ------- | --------- |
| latest release | ✅ |
| older releases | ❌ — please upgrade |

## Reporting a Vulnerability

Please do **not** open a public issue for security problems.

Use GitHub's private vulnerability reporting:
**https://github.com/emmmdty/token-usage/security/advisories/new**

Include what you can of: affected version/commit, reproduction steps, and
impact. You will get a response within a few days, and a fix or mitigation
will be coordinated with you before any public disclosure.

## What Is In Scope

- Credential leakage through logs, output, config files, or IPC
- Insecure storage in the keyring / encrypted-fallback paths
- Command injection or path traversal in any subcommand
- The self-update mechanism (download integrity, replacement of the binary)

## What Is Out of Scope

- The security of the upstream AI providers themselves
- A compromised host (if the machine is owned, every stored secret is lost —
  token-usage only offers the same guarantees as the system keyring)
