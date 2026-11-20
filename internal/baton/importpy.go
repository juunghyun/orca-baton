package baton

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// cmdImport 는 cc-baton 0.1(Python)과 claude-swap 의 계정을 그대로 등록한다. 변환·복사 없이 읽기만 한다.
//   - claude-swap sequence.json: 계정 번호·별칭·이메일, 기본 폴더(~/.claude)를 쥔 계정(activeAccountNumber)
//   - 계정 폴더: 기본 계정이면 ~/.claude, 아니면 ~/.claude-swap-backup/sessions/<번호>-*
//   - cc-baton config.json: 번호별 그룹·색, 언어, 권한 건너뛰기, 자동 최신화, 레벨 칩
func (c *Config) cmdImport() int {
	backup := filepath.Join(home(), ".claude-swap-backup")
	var seq struct {
		Accounts map[string]struct {
			Alias string `json:"alias"`
			Email string `json:"email"`
		} `json:"accounts"`
		Active   json.Number   `json:"activeAccountNumber"`
		Sequence []json.Number `json:"sequence"`
	}
	if err := readJSON(filepath.Join(backup, "sequence.json"), &seq); err != nil {
		return fail(fmt.Errorf(L("couldn't read claude-swap accounts: %v", "claude-swap 계정을 읽지 못했습니다: %v"), err))
	}
	var old map[string]json.RawMessage
	readJSON(filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), "cc-baton", "config.json"), &old)

	order := make([]string, 0, len(seq.Accounts))
	for _, n := range seq.Sequence {
		order = append(order, n.String())
	}
	if len(order) == 0 {
		for n := range seq.Accounts {
			order = append(order, n)
		}
		sort.Slice(order, func(i, j int) bool { a, _ := strconv.Atoi(order[i]); b, _ := strconv.Atoi(order[j]); return a < b })
	}

	added := 0
	for _, num := range order {
		a, ok := seq.Accounts[num]
		if !ok {
			continue
		}
		name := strings.ToLower(a.Alias)
		if name == "" {
			name, _, _ = strings.Cut(strings.ToLower(a.Email), "@")
		}
		// 기본 폴더를 쥔 계정은 ~/.claude 가 살아 있는 로그인이다. 나머지는 claude-swap 세션 폴더.
		dir := ""
		if num == seq.Active.String() {
			dir = "~/.claude"
		} else if hits, _ := filepath.Glob(filepath.Join(backup, "sessions", num+"-*")); len(hits) == 1 {
			dir = tilde(hits[0])
		}
		if dir == "" {
			fmt.Printf(L("- account %s (%s): no folder of its own, skipped. Add it later with `baton add`.\n",
				"- %s번 계정(%s): 자기 폴더가 없어 건너뜀. 나중에 `baton add` 로 넣으세요.\n"), num, name)
			continue
		}
		if err := validName(name); err != nil {
			name = "account" + num
		}
		if _, dup := c.find(name); dup {
			fmt.Printf(L("- %s: already registered\n", "- %s: 이미 등록됨\n"), name)
			continue
		}
		if other, dup := c.byDir(dir); dup {
			fmt.Printf(L("- %s: its folder is already account %s\n", "- %s: 폴더가 이미 %s 계정\n"), name, other.Name)
			continue
		}
		var meta struct {
			Group string          `json:"group"`
			Color json.RawMessage `json:"color"`
		}
		json.Unmarshal(old[num], &meta)
		acc := Account{Name: name, Dir: dir, Group: strings.ToLower(meta.Group), Color: strings.Trim(string(meta.Color), `"`)}
		if acc.Group == "unknown" {
			acc.Group = ""
		}
		c.Accounts = append(c.Accounts, acc)
		if dir == "~/.claude" && c.Default == "" {
			c.Default = name
		}
		added++
		fmt.Printf("+ %-12s %-10s %s\n", name, acc.Group, dir)
	}

	flagOn := func(key string) bool {
		var v struct {
			Enabled bool `json:"enabled"`
		}
		return json.Unmarshal(old[key], &v) == nil && v.Enabled
	}
	if old != nil {
		var s string
		if json.Unmarshal(old["language"], &s) == nil && (s == "en" || s == "ko") {
			c.Language = s
		}
		c.SkipPermissions = c.SkipPermissions || flagOn("skipPermissions")
		c.AutoUpdate = c.AutoUpdate || flagOn("autoUpdate")
		var chip bool
		c.LevelChip = c.LevelChip || (json.Unmarshal(old["levelChip"], &chip) == nil && chip)
	}
	if err := c.save(); err != nil {
		return fail(err)
	}
	fmt.Printf(L("✓ %d account(s) imported into %s\n", "✓ 계정 %d개를 %s 에 넣었습니다\n"), added, tilde(configPath()))
	fmt.Println(L("Next: tell baton which folders belong to which account, e.g. `baton rule add ~/personal personal`.",
		"다음: 어느 폴더가 어느 계정인지 알려 주세요. 예) `baton rule add ~/personal personal`"))
	if _, err := exec.LookPath("cc-baton"); err == nil {
		fmt.Println(L("Remove cc-baton 0.1 first (`cc-baton uninstall`) so claude-swap stops touching account logins.",
			"cc-baton 0.1 은 먼저 걷으세요(`cc-baton uninstall`). claude-swap 이 계정 로그인을 더 건드리지 않게."))
	}
	return 0
}
