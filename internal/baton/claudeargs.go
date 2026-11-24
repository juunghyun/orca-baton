package baton

import (
	"slices"
	"strings"
)

// claude 인자 중 값을 하나 받는 것 (Claude Code 2.1.329 실행 파일의 옵션 정의에서 확인).
// 여러 값을 받는 것(--add-dir 등)도 하나로 본다. Orca 는 --mcp-config=<파일> 처럼 = 형태로 넘긴다.
var valueFlags = map[string]bool{
	"--add-dir": true, "--agent": true, "--agents": true, "--allowedTools": true, "--allowed-tools": true,
	"--disallowedTools": true, "--disallowed-tools": true, "--tools": true, "--betas": true,
	"--fallback-model": true, "--file": true, "--mcp-config": true, "--model": true, "--name": true, "-n": true,
	"--plugin-dir": true, "--setting-sources": true, "--settings": true, "--effort": true,
	"--permission-mode": true, "--append-system-prompt": true, "--append-system-prompt-file": true,
	"--system-prompt": true, "--system-prompt-file": true, "--output-format": true, "--input-format": true,
	"--max-turns": true, "--max-budget-usd": true, "--json-schema": true, "--permission-prompt-tool": true,
	"--session-id": true,
}

// 값이 있을 수도 없을 수도 있는 것. 다음 토큰이 - 로 시작하지 않으면 값으로 먹는다(commander 와 같다).
var optionalValueFlags = map[string]bool{
	"-r": true, "--resume": true, "-d": true, "--debug": true, "--from-pr": true,
	"-w": true, "--worktree": true, "--remote-control": true, "--rc": true,
}

// 다른 계정으로 이어 띄울 때 버리는 것: 어느 대화를 열지 정하는 인자와 한 번만 쓰는 인자.
var sessionFlags = map[string]bool{
	"-r": true, "--resume": true, "-c": true, "--continue": true, "--session-id": true,
	"--fork-session": true, "--from-pr": true, "-w": true, "--worktree": true, "-p": true, "--print": true,
}

type token struct {
	flag  string // "" 면 위치 인자(첫 프롬프트 등)
	value []string
}

func tokenize(args []string) []token {
	var out []token
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			for _, rest := range args[i+1:] {
				out = append(out, token{value: []string{rest}})
			}
			return out
		case !strings.HasPrefix(a, "-") || a == "-":
			out = append(out, token{value: []string{a}})
		case strings.Contains(a, "="):
			f, _, _ := strings.Cut(a, "=")
			out = append(out, token{flag: f, value: []string{a}})
		case valueFlags[a] && i+1 < len(args):
			out = append(out, token{flag: a, value: []string{a, args[i+1]}})
			i++
		case optionalValueFlags[a] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-"):
			out = append(out, token{flag: a, value: []string{a, args[i+1]}})
			i++
		default:
			out = append(out, token{flag: a, value: []string{a}})
		}
	}
	return out
}

// oneShot 은 대화 화면 없이 한 번 돌고 끝나는 호출인가(-p/--print, -v/--version).
// shortcut: TTY 유무는 보지 않는다(시험이 TTY 없이 돈다). S0 에서 본 Orca 일회성 호출은 모두 이 인자를 달았다.
func oneShot(args []string) bool {
	return slices.ContainsFunc(tokenize(args), func(t token) bool {
		return t.flag == "-p" || t.flag == "--print" || t.flag == "-v" || t.flag == "--version"
	})
}

// resumeSID 는 --resume <id> / -r <id> / --resume=<id> 의 세션 ID. 없거나 형식이 아니면 "".
func resumeSID(args []string) string {
	for _, t := range tokenize(args) {
		if t.flag != "-r" && t.flag != "--resume" {
			continue
		}
		v := t.value[len(t.value)-1]
		if _, after, ok := strings.Cut(v, "="); ok && len(t.value) == 1 {
			v = after
		}
		if v != t.flag && validSID(v) {
			return v
		}
	}
	return ""
}

func hasPermissionFlag(args []string) bool {
	for _, t := range tokenize(args) {
		switch t.flag {
		case "--permission-mode", "--dangerously-skip-permissions", "--allow-dangerously-skip-permissions":
			return true
		}
	}
	return false
}

// relaunchArgs 는 같은 탭에서 다른 계정으로 이어 띄울 때 남길 인자. 대화를 고르는 인자와 첫 프롬프트는 버린다
// (프롬프트를 남기면 이어 띄운 대화에 같은 요청이 한 번 더 들어간다). Orca 의 --plugin-dir 같은 건 남긴다.
func relaunchArgs(args []string) []string {
	var out []string
	for _, t := range tokenize(args) {
		if t.flag == "" || sessionFlags[t.flag] {
			continue
		}
		out = append(out, t.value...)
	}
	return out
}
