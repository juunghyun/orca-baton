<div align="center">

<img src="docs/assets/logo.svg" alt="" width="96" />

# orca-baton

**Start Claude Code on the right account, and move a conversation between your accounts, full transcript included.**

[![CI](https://github.com/juunghyun/orca-baton/actions/workflows/ci.yml/badge.svg)](https://github.com/juunghyun/orca-baton/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-8250DF?style=flat-square)](LICENSE)
[![macOS](https://img.shields.io/badge/platform-macOS-57606A?style=flat-square&logo=apple)](#install)

English | [한국어](README.ko.md)

</div>

Your team seat and your personal plan are separate Claude accounts. `baton` starts Claude Code on the account that owns the folder you're in, keeps each account's conversations and memory apart, and when a task moves, `/swap personal` continues the same conversation on your other account in the same tab. A statusline HUD shows which account you're on and how much is left on each.

It's built for [Orca](https://github.com/stablyai/orca) (point Orca's Claude command at `baton claude`) and works the same in a plain terminal.

> **Successor to [cc-baton](https://github.com/juunghyun/cc-baton)** (Python, built on claude-swap). orca-baton is a single Go binary, each account folder keeps its own login, and it never stores or reads tokens. See [Moving from cc-baton](#moving-from-cc-baton).

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/juunghyun/orca-baton/main/scripts/install.sh | sh
```

This puts `baton` (and a `baton-claude` link) in `~/.local/bin`, checks the download's checksum, and runs `baton install`, which adds `/swap`, its hook and the HUD to `~/.claude/settings.json`. It never asks anything, is safe to re-run, backs up `settings.json` first, stops if that file isn't valid JSON, and leaves a statusline you already have alone.

Then tell baton about your accounts and folders:

```sh
baton add work --dir ~/.claude --group work   # the login you already have
baton add personal --group personal           # new folder; Claude Code opens there, run /login once
baton rule add ~/personal personal            # folders under ~/personal use personal
baton rule default work                       # everything else uses work
```

**Orca:** in Orca's settings, set the Claude command to `baton claude` (or `baton-claude` if the field won't take a space). If you passed account-specific flags to Claude in Orca (such as `--settings` or `--mcp-config`), move them to that account's `args` in baton's config and clear them in Orca. baton doesn't change Orca's settings.

**Plain terminal:** run `baton` instead of `claude`. Arguments go to Claude Code as they are (`baton --resume`, `baton -c`, …).

## Which account starts

1. `--resume <id>`: the account that has that conversation (the most recently used copy if several do).
2. A git worktree (including Orca's worktree workspaces): the rule for its original repository's path.
3. Otherwise the longest matching folder rule.
4. No rule matches: the default account.

`baton <account> [args…]` overrides all of that. If you resume a conversation that only exists on another account, it's copied over first.

## Moving a conversation

Type `/swap personal` in Claude Code. The hook handles it before any model call (so it works even when the account has hit its limit), Claude Code exits, the transcript and its tool outputs are copied to the other account, and the same conversation resumes there in the same tab. The original copy stays where it was; your other conversations and prompt history stay with their accounts.

Moving to an account in another group (work ↔ personal) asks first; `/swap personal --yes` goes ahead. `/swap` alone lists your accounts.

## HUD

Three lines under the prompt:

1. The account and its group, with this account's 5-hour and weekly usage and time to reset.
2. Your other accounts' usage. Each session writes its own account's numbers to a small file that the others read, so no network calls and no tokens. Old numbers show their age; a window past its reset time shows as reset.
3. Model, effort, context use and the switches you turned on.

## Commands

| Command | What it does |
|---|---|
| `baton [args…]` / `baton claude [args…]` | Start Claude Code on the account the rules pick |
| `baton <account> [args…]` | Start on that account |
| `/swap <account> [--yes]` | Move this conversation to another account, in the same tab |
| `baton add <name> [--dir D] [--group G] [--color C]` | Add an account (a new folder under `~/.claude-accounts` unless `--dir`) |
| `baton list` · `rm <name>` · `alias <name> <new>` | List, remove (the folder stays), rename |
| `baton rule [add <path> <account> \| rm <path> \| default <account>]` | Folder rules |
| `baton set bypass\|update\|chip on\|off` | Skip permission prompts · keep npm-installed Claude Code updated · effort level chip |
| `baton lang en\|ko` | Display language |
| `baton install` · `uninstall` | Add or remove `/swap`, its hook and the HUD |

Settings live in `~/.config/orca-baton/config.json`. Per-account `args` and `env` are added only when that account starts:

```json
{
  "accounts": [
    {"name": "work", "dir": "~/.claude", "group": "work", "args": ["--mcp-config=/Users/me/.claude/work-mcp.json"]},
    {"name": "personal", "dir": "~/.claude-accounts/personal", "group": "personal", "color": "39"}
  ],
  "rules": [{"path": "~/personal", "account": "personal"}],
  "default": "work"
}
```

## Uninstall

```sh
baton uninstall && rm ~/.local/bin/baton ~/.local/bin/baton-claude
```

Removes only what baton added to `settings.json`. Accounts, rules and account folders stay. Point Orca's Claude command back to `claude`.

## Moving from cc-baton

1. `cc-baton uninstall`: stops claude-swap from touching your logins. `~/.claude-swap-backup` stays.
2. Install orca-baton as above, then `baton import`: registers your claude-swap accounts with their folders, groups and colors, and your language and switch settings. Nothing is copied or converted.
3. Add folder rules (`baton rule add …`).
4. If an account asks you to log in, run `/login` once in that account's Claude Code.

Not carried over from cc-baton: the account picker (folder rules pick the account), automatic swap on rate limits (swap by hand with `/swap`), idle-session sleep, MCP server copying, and the usage API.

## Before you use it

- **Your own accounts only.** Sharing an account or its login with anyone else is not allowed under Anthropic's [Consumer Terms](https://www.anthropic.com/legal/consumer-terms).
- **Each account keeps its own limits.** baton only changes which of your accounts Claude Code uses; it doesn't raise or reset any limit.
- **Work conversations belong to your organization.** Team and Enterprise seats are covered by your organization's agreement ([Commercial Terms](https://www.anthropic.com/legal/commercial-terms)), under which the organization owns its inputs and outputs. After a move to a personal account, the conversation is handled under the [Consumer Terms](https://www.anthropic.com/legal/consumer-terms), including use for model training unless you opt out. The group check asks before such a move.
- **Permission prompts stay on unless you turn them off.** `baton set bypass on` starts Claude Code with `--dangerously-skip-permissions`, which lets Claude run commands without asking. It's off by default.
- baton reads parts of Claude Code that aren't official APIs (the session registry and the statusline input). Built against Claude Code 2.1.329 on macOS.

## Development

```sh
go vet ./... && go test ./...
```

Tests run the real binary against a temporary home folder and a fake `claude`, so they never touch your accounts.

## Author's note

I work across a team seat and a personal plan every day. Every time a task moved from one account to the other, the conversation stayed behind and I had to explain everything again. baton is the smallest thing I could build that keeps the thread.

orca-baton is an unofficial add-on, not affiliated with or endorsed by Anthropic or Orca. Claude and Claude Code are trademarks of Anthropic, PBC. MIT licensed; provided as is, without warranty.
