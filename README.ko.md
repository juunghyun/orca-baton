<div align="center">

<img src="docs/assets/logo.svg" alt="" width="96" />

# orca-baton

**Claude Code 를 맞는 계정으로 띄우고, 내 계정끼리 대화를 옮깁니다. 트랜스크립트 전체 그대로.**

[![CI](https://github.com/juunghyun/orca-baton/actions/workflows/ci.yml/badge.svg)](https://github.com/juunghyun/orca-baton/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-8250DF?style=flat-square)](LICENSE)
[![macOS](https://img.shields.io/badge/platform-macOS-57606A?style=flat-square&logo=apple)](#설치)

[English](README.md) | 한국어

</div>

팀 시트와 개인 요금제는 서로 다른 Claude 계정입니다. `baton` 은 지금 폴더가 속한 계정으로 Claude Code 를 띄우고, 계정마다 대화와 메모리를 따로 둡니다. 작업이 다른 계정으로 넘어가야 하면 `/swap personal` 한 번으로 같은 탭에서 그 대화가 다른 계정으로 이어집니다. statusline HUD 에는 지금 계정과 계정마다 남은 사용량이 보입니다.

[Orca](https://github.com/stablyai/orca) 에 맞춰 만들었고(Orca 의 Claude 실행 명령을 `baton claude` 로), 일반 터미널에서도 똑같이 동작합니다.

> **[cc-baton](https://github.com/juunghyun/cc-baton)(Python, claude-swap 기반)의 후속입니다.** Go 바이너리 하나이고, 로그인은 계정 폴더가 직접 갖고, 토큰을 저장하거나 읽지 않습니다. [cc-baton 에서 옮기기](#cc-baton-에서-옮기기)를 보세요.

## 설치

```sh
curl -fsSL https://raw.githubusercontent.com/juunghyun/orca-baton/main/scripts/install.sh | sh
```

`~/.local/bin` 에 `baton`(과 `baton-claude` 링크)을 넣고, 받은 파일의 체크섬을 확인한 뒤 `baton install` 을 돌립니다. `baton install` 은 `~/.claude/settings.json` 에 `/swap`·그 훅·HUD 를 넣습니다. 아무것도 묻지 않고, 여러 번 돌려도 결과가 같습니다. `settings.json` 은 먼저 백업하고, 올바른 JSON 이 아니면 멈추고, 이미 쓰는 statusline 은 바꾸지 않습니다.

그다음 계정과 폴더를 알려 주세요:

```sh
baton add work --dir ~/.claude --group work   # 지금 로그인돼 있는 계정
baton add personal --group personal           # 새 폴더. Claude Code 가 그 폴더로 뜨면 /login 한 번
baton rule add ~/personal personal            # ~/personal 아래는 personal
baton rule default work                       # 나머지는 work
```

**Orca:** Orca 설정에서 Claude 실행 명령을 `baton claude` 로 바꾸세요(칸이 공백을 못 받으면 `baton-claude`). Orca 에서 Claude 에 계정별 인자(`--settings`, `--mcp-config` 등)를 넘기고 있었다면 baton 설정의 그 계정 `args` 로 옮기고 Orca 쪽은 비우세요. baton 은 Orca 설정을 바꾸지 않습니다.

**일반 터미널:** `claude` 대신 `baton` 을 실행하세요. 인자는 그대로 Claude Code 로 갑니다(`baton --resume`, `baton -c` …).

## 어느 계정으로 뜨나

1. `--resume <id>`: 그 대화를 가진 계정(사본이 여럿이면 가장 최근에 쓴 쪽)
2. git 워크트리(Orca 워크트리 작업 공간 포함): 원본 저장소 경로의 규칙
3. 그 밖엔 가장 길게 맞는 폴더 규칙
4. 맞는 규칙이 없으면 기본 계정

`baton <계정> [인자…]` 는 위 규칙보다 우선합니다. 다른 계정에만 있는 대화를 이어 띄우면 먼저 복사합니다.

## 대화 옮기기

Claude Code 에서 `/swap personal` 을 치세요. 훅이 모델 호출 전에 처리하므로 한도에 걸린 계정에서도 동작합니다. Claude Code 가 끝나고, 트랜스크립트와 떼어 저장한 도구 출력이 다른 계정으로 복사되고, 같은 탭에서 같은 대화가 이어집니다. 원본은 그대로 남고, 다른 대화와 프롬프트 기록은 각 계정에 남습니다.

그룹이 다른 계정(업무 ↔ 개인)으로 옮길 때는 먼저 묻습니다. `/swap personal --yes` 로 진행합니다. `/swap` 만 치면 계정 목록이 나옵니다.

## HUD

입력창 아래 세 줄:

1. 지금 계정과 그룹, 이 계정의 5시간·주간 사용량과 리셋까지 남은 시간
2. 다른 계정의 사용량. 세션마다 자기 계정 수치를 작은 파일로 남기고 다른 세션이 읽습니다. 네트워크 호출도 토큰도 쓰지 않습니다. 오래된 수치에는 나이가 붙고, 리셋 시각이 지난 창은 초기화로 표시합니다.
3. 모델, effort, 컨텍스트 사용률, 켠 기능

## 명령

| 명령 | 하는 일 |
|---|---|
| `baton [인자…]` / `baton claude [인자…]` | 규칙이 고른 계정으로 Claude Code 실행 |
| `baton <계정> [인자…]` | 그 계정으로 실행 |
| `/swap <계정> [--yes]` | 이 대화를 다른 계정으로, 같은 탭에서 |
| `baton add <이름> [--dir 폴더] [--group 그룹] [--color 색]` | 계정 추가(`--dir` 이 없으면 `~/.claude-accounts` 아래 새 폴더) |
| `baton list` · `rm <이름>` · `alias <이름> <새 이름>` | 목록, 제거(폴더는 남음), 이름 바꾸기 |
| `baton rule [add <경로> <계정> \| rm <경로> \| default <계정>]` | 폴더 규칙 |
| `baton set bypass\|update\|chip on\|off` | 권한 확인 건너뛰기 · npm 으로 깐 Claude Code 자동 최신화 · effort 레벨 칩 |
| `baton lang en\|ko` | 화면 언어 |
| `baton install` · `uninstall` | `/swap`·훅·HUD 넣기/빼기 |

설정 파일은 `~/.config/orca-baton/config.json` 입니다. 계정별 `args`·`env` 는 그 계정으로 띄울 때만 붙습니다:

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

## 제거

```sh
baton uninstall && rm ~/.local/bin/baton ~/.local/bin/baton-claude
```

`settings.json` 에서 baton 이 넣은 것만 뺍니다. 계정·규칙·계정 폴더는 남습니다. Orca 의 Claude 실행 명령은 `claude` 로 되돌리세요.

## cc-baton 에서 옮기기

1. `cc-baton uninstall`: claude-swap 이 로그인을 더 건드리지 않게 합니다. `~/.claude-swap-backup` 은 남습니다.
2. 위처럼 orca-baton 을 설치하고 `baton import`: claude-swap 계정을 폴더·그룹·색과 함께, 언어와 스위치 설정까지 등록합니다. 아무것도 복사하거나 변환하지 않습니다.
3. 폴더 규칙을 넣으세요(`baton rule add …`).
4. 어느 계정이 로그인을 요구하면 그 계정의 Claude Code 에서 `/login` 을 한 번 하세요.

cc-baton 에서 가져오지 않은 것: 계정 피커(폴더 규칙이 고릅니다), 한도 도달 시 자동 스왑(`/swap` 으로 직접), 유휴 세션 절전, MCP 서버 복사, 사용량 API.

## 쓰기 전에

- **본인 계정만.** 계정이나 로그인을 다른 사람과 나눠 쓰는 것은 Anthropic [소비자 약관](https://www.anthropic.com/legal/consumer-terms)상 허용되지 않습니다.
- **한도는 계정마다 그대로입니다.** baton 은 Claude Code 가 내 계정 중 어느 것을 쓸지만 바꿉니다. 한도를 늘리거나 초기화하지 않습니다.
- **업무 대화는 조직의 것입니다.** Team·Enterprise 시트는 조직의 계약([상업 약관](https://www.anthropic.com/legal/commercial-terms))을 따르고, 그 입력과 출력은 조직 소유입니다. 개인 계정으로 옮긴 대화는 [소비자 약관](https://www.anthropic.com/legal/consumer-terms)을 따르며, 거부하지 않으면 모델 학습에 쓰일 수 있습니다. 그래서 그룹이 다르면 옮기기 전에 묻습니다.
- **권한 확인은 끄지 않는 한 그대로입니다.** `baton set bypass on` 은 Claude Code 를 `--dangerously-skip-permissions` 로 띄워, Claude 가 묻지 않고 명령을 실행하게 합니다. 기본은 꺼짐입니다.
- baton 은 Claude Code 의 공식 API 가 아닌 부분(세션 등록 파일, statusline 입력)을 읽습니다. macOS 의 Claude Code 2.1.329 기준으로 만들었습니다.

## 개발

```sh
go vet ./... && go test ./...
```

테스트는 진짜 바이너리를 임시 홈 폴더와 가짜 `claude` 로 돌리므로 실제 계정에 닿지 않습니다.

## 만든 이유

저는 매일 팀 시트와 개인 요금제를 오가며 일합니다. 작업이 한 계정에서 다른 계정으로 넘어갈 때마다 대화는 남겨지고, 처음부터 다시 설명해야 했습니다. baton 은 그 흐름을 잇기 위해 만들 수 있는 가장 작은 도구입니다.

orca-baton 은 비공식 부가 도구이며 Anthropic 이나 Orca 와 제휴하거나 보증받지 않았습니다. Claude 와 Claude Code 는 Anthropic, PBC 의 상표입니다. MIT 라이선스이며, 보증 없이 있는 그대로 제공됩니다.
