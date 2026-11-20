package baton

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	cmdMark     = "<!-- installed by baton; `baton uninstall` removes this file -->"
	backupsKept = 5
)

// swapCommand 는 /swap 슬래시 명령 파일. 평소엔 UserPromptSubmit 훅이 모델 호출 전에 처리하므로,
// 이 본문은 자동완성 목록과 훅이 실패했을 때의 대비용이다.
const swapCommand = `---
description: Move this conversation to another Claude account with its full transcript (baton)
argument-hint: <account> [--yes]
allowed-tools: Bash({baton} swap:*)
---

> Normally the UserPromptSubmit hook (` + "`baton hook prompt`" + `) handles /swap before any model call and blocks the prompt. If this instruction reached you, the hook failed; use the fallback below.

Run ` + "`{baton} swap $ARGUMENTS`" + ` and show the user its output as is. Reply in the user's language.

By exit code:
- **0**: The swap is under way; this session is about to end and continue on the other account in the same tab. Stop and do nothing else.
- **2**: The target is in another account group. Show the warning and wait. Re-run with --yes only if the user explicitly says to go ahead. **Never add --yes on your own.**
- **3**: Not running under the baton wrapper. Relay the command it printed.
- **1**: Bad argument. Running /swap with no argument lists the accounts.
`

func settingsPath() string {
	p := filepath.Join(defaultDir(), "settings.json")
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r // 링크면 실제 파일을 고친다
	}
	return p
}

func binPath() string {
	if p := os.Getenv("BATON_BIN"); p != "" {
		return p
	}
	p, _ := os.Executable()
	return p
}

// shellPath 는 훅·statusline 명령에 박을 경로(셸로 실행된다). 홈 아래면 "$HOME/…" 로 써서 사용자 이름과 무관하게 둔다.
func shellPath(p string) string {
	if strings.ContainsAny(p, "\"$`\\") {
		return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
	}
	if h := home(); strings.HasPrefix(p, h+"/") {
		return `"$HOME` + p[len(h):] + `"`
	}
	return `"` + p + `"`
}

// ours 는 우리가 넣은 명령인가. 이름에 baton 이 들어간 남의 스크립트까지 지우지 않게 정확히 맞춘다.
func ours(cmd, sub string) bool {
	f := strings.Fields(strings.NewReplacer(`"`, "", "'", "").Replace(cmd))
	return len(f) >= 2 && filepath.Base(f[0]) == "baton" && strings.Join(f[1:], " ") == sub
}

// --- 키 순서를 지키는 JSON 객체 (사용자 settings.json 이 dotfiles 저장소에 있어도 diff 가 우리 키에만 생기게) ---

type kv struct {
	k string
	v json.RawMessage
}
type object []kv

func parseObject(raw []byte) (object, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New(L("expected a JSON object", "JSON 객체가 아닙니다"))
	}
	var o object
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		o = append(o, kv{t.(string), v})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing data after the object")
	}
	return o, nil
}

func (o object) get(k string) json.RawMessage {
	for _, e := range o {
		if e.k == k {
			return e.v
		}
	}
	return nil
}

func (o *object) set(k string, v any) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	raw := json.RawMessage(bytes.TrimSpace(b.Bytes()))
	for i := range *o {
		if (*o)[i].k == k {
			(*o)[i].v = raw
			return
		}
	}
	*o = append(*o, kv{k, raw})
}

func (o *object) del(k string) {
	for i := range *o {
		if (*o)[i].k == k {
			*o = append((*o)[:i], (*o)[i+1:]...)
			return
		}
	}
}

func (o object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, e := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(e.k)
		b.Write(k)
		b.WriteByte(':')
		b.Write(e.v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

type settings struct {
	path string
	obj  object
	orig []byte
}

// openSettings 는 settings.json 을 읽는다. 깨진 JSON 이면 손대지 않게 에러로 멈춘다.
func openSettings() (*settings, error) {
	s := &settings{path: settingsPath()}
	raw, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	s.orig = raw
	if len(bytes.TrimSpace(raw)) == 0 {
		return s, nil
	}
	if s.obj, err = parseObject(raw); err != nil {
		return nil, fmt.Errorf(L("%s isn't valid JSON, so it was left alone (%v). Fix it and run again.",
			"%s 가 올바른 JSON 이 아니라 손대지 않았습니다 (%v). 고친 뒤 다시 실행하세요."), s.path, err)
	}
	if h := s.obj.get("hooks"); h != nil && string(h) != "null" {
		if _, err := parseObject(h); err != nil {
			return nil, fmt.Errorf(L("%s: \"hooks\" isn't an object, so it was left alone.", "%s: \"hooks\" 가 객체가 아니라 손대지 않았습니다."), s.path)
		}
	}
	return s, nil
}

func (s *settings) render() []byte {
	compact, _ := s.obj.MarshalJSON()
	var b bytes.Buffer
	json.Indent(&b, compact, "", "  ")
	b.WriteByte('\n')
	return b.Bytes()
}

// save → 보고 문구. 바뀐 게 없으면 쓰지 않는다. 쓰기 전 시각별 백업(최근 5개), 원래 권한 유지.
func (s *settings) save() (string, error) {
	if s.orig == nil && len(s.obj) == 0 {
		return L("no change", "변경 없음"), nil
	}
	out := s.render()
	if s.orig != nil && bytes.Equal(normalize(s.orig), normalize(out)) {
		return L("no change", "변경 없음"), nil
	}
	mode := os.FileMode(0o600)
	note := L("created", "새로 만듦")
	if fi, err := os.Stat(s.path); err == nil {
		mode = fi.Mode().Perm()
		bak := fmt.Sprintf("%s.bak-baton-%s", s.path, time.Now().Format("20060102150405"))
		if err := os.WriteFile(bak, s.orig, mode); err != nil {
			return "", err
		}
		olds, _ := filepath.Glob(s.path + ".bak-baton-*")
		sort.Strings(olds)
		for len(olds) > backupsKept {
			os.Remove(olds[0])
			olds = olds[1:]
		}
		note = L("backup ", "백업 ") + filepath.Base(bak)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return "", err
	}
	return note, writeFileAtomic(s.path, out, mode)
}

func normalize(b []byte) []byte {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return b
	}
	n, _ := json.Marshal(v)
	return n
}

// setHook 은 event 에서 우리 훅을 빼고, add 면 하나만 다시 넣는다. 남의 훅은 그대로 둔다.
func (s *settings) setHook(event, sub string, add bool) error {
	hooks, _ := parseObject(s.obj.get("hooks"))
	var groups []map[string]any
	if raw := hooks.get(event); raw != nil && string(raw) != "null" {
		if err := json.Unmarshal(raw, &groups); err != nil {
			// 모르는 모양이면 덮어써서 남의 훅을 잃지 않게 손대지 않는다.
			return fmt.Errorf(L("%s: hooks.%s isn't a list of hook groups, so it was left alone", "%s: hooks.%s 가 훅 그룹 목록이 아니라 손대지 않았습니다"), s.path, event)
		}
	}
	var kept []map[string]any
	for _, g := range groups {
		hs, _ := g["hooks"].([]any)
		var mine []any
		for _, h := range hs {
			if m, ok := h.(map[string]any); ok {
				if c, _ := m["command"].(string); ours(c, sub) {
					continue
				}
			}
			mine = append(mine, h)
		}
		if len(mine) == 0 && len(hs) > 0 {
			continue
		}
		g["hooks"] = mine
		kept = append(kept, g)
	}
	if add {
		kept = append(kept, map[string]any{"hooks": []any{map[string]any{
			"type": "command", "command": shellPath(binPath()) + " " + sub, "timeout": 10}}})
	}
	if len(kept) == 0 {
		hooks.del(event)
	} else {
		hooks.set(event, kept)
	}
	if len(hooks) == 0 {
		s.obj.del("hooks")
	} else {
		s.obj.set("hooks", hooks)
	}
	return nil
}

func (s *settings) statusLine() (map[string]any, bool) {
	raw := s.obj.get("statusLine")
	var sl map[string]any
	if raw == nil || string(raw) == "null" || json.Unmarshal(raw, &sl) != nil {
		return nil, raw != nil && string(raw) != "null"
	}
	return sl, true
}

func commandFile() string { return filepath.Join(defaultDir(), "commands", "swap.md") }

func cmdInstall() int {
	s, err := openSettings()
	if err != nil {
		return fail(err)
	}
	var report []string
	if bytes.Contains(s.orig, []byte("cc-baton swap hook")) {
		report = append(report, L("! cc-baton 0.1 (Python) is still installed. Run `cc-baton uninstall` so the two don't both handle /swap.",
			"! cc-baton 0.1(Python)이 아직 설치돼 있습니다. 둘이 /swap 을 같이 처리하지 않게 `cc-baton uninstall` 을 먼저 하세요."))
	}
	if err := s.setHook("UserPromptSubmit", "hook prompt", true); err != nil {
		return fail(err)
	}
	cmd := shellPath(binPath()) + " statusline"
	switch sl, exists := s.statusLine(); {
	case !exists:
		s.obj.set("statusLine", map[string]any{"type": "command", "command": cmd, "refreshInterval": 10})
		report = append(report, L("HUD (statusline): installed", "HUD(statusline): 설치"))
	case sl != nil && ours(fmt.Sprint(sl["command"]), "statusline"):
		sl["command"] = cmd
		s.obj.set("statusLine", sl)
	default:
		report = append(report, fmt.Sprintf(L("HUD: you already have a statusline, left as is. To use baton's HUD, set statusLine.command to: %s",
			"HUD: 이미 statusline 이 있어 그대로 둡니다. baton HUD 를 쓰려면 statusLine.command 를 이렇게: %s"), cmd))
	}
	note, err := s.save()
	if err != nil {
		return fail(err)
	}
	report = append(report, "settings.json: "+note)
	report = append(report, "/swap: "+installCommand())
	report = append(report, L("Orca: in Settings, set the Claude command to `baton claude` (baton doesn't change Orca's settings).",
		"Orca: 설정에서 Claude 실행 명령을 `baton claude` 로 바꾸세요 (baton 은 Orca 설정을 바꾸지 않습니다)."))
	for _, r := range report {
		fmt.Println(r)
	}
	return 0
}

func installCommand() string {
	p := commandFile()
	body := strings.ReplaceAll(swapCommand, "{baton}", shellPath(binPath())) + "\n" + cmdMark + "\n"
	cur, err := os.ReadFile(p)
	switch {
	case err == nil && !bytes.Contains(cur, []byte(cmdMark)):
		return L("skipped (you already have a different /swap command)", "건너뜀 (같은 이름의 다른 명령이 있음)")
	case err == nil && string(cur) == body:
		return L("unchanged", "그대로")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err.Error()
	}
	if err := writeFileAtomic(p, []byte(body), 0o644); err != nil {
		return err.Error()
	}
	return L("installed", "설치")
}

func cmdUninstall() int {
	s, err := openSettings()
	if err != nil {
		return fail(err)
	}
	if err := s.setHook("UserPromptSubmit", "hook prompt", false); err != nil {
		return fail(err)
	}
	if sl, _ := s.statusLine(); sl != nil && ours(fmt.Sprint(sl["command"]), "statusline") {
		s.obj.del("statusLine")
	}
	note, err := s.save()
	if err != nil {
		return fail(err)
	}
	fmt.Println("settings.json: " + note)
	if b, err := os.ReadFile(commandFile()); err == nil && bytes.Contains(b, []byte(cmdMark)) {
		os.Remove(commandFile())
		fmt.Println("/swap: " + L("removed", "제거"))
	}
	fmt.Println(L("Accounts, rules and account folders are kept. Point Orca's Claude command back to `claude`.",
		"계정·규칙·계정 폴더는 그대로 둡니다. Orca 의 Claude 실행 명령은 `claude` 로 되돌리세요."))
	return 0
}
