package baton

import (
	"os"
	"testing"
)

func TestImportFromPythonEdition(t *testing.T) {
	s := newSandbox(t)
	s.write(".claude-swap-backup/sequence.json", `{"accounts":{"1":{"alias":"personal","email":"me@example.com"},
	  "2":{"alias":"","email":"work@corp.example"}},"activeAccountNumber":2,"sequence":[1,2]}`)
	s.mkdir(".claude-swap-backup/sessions/1-me_example.com")
	s.mkdir(".claude-swap-backup/sessions/2-work_corp.example") // 기본 계정의 옛 세션 폴더보다 ~/.claude 가 우선
	s.write(".config/cc-baton/config.json", `{"1":{"group":"personal"},"2":{"group":"team","color":208},
	  "language":"ko","skipPermissions":{"enabled":true},"autoUpdate":{"enabled":true},"levelChip":true,"onLimit":{"enabled":true}}`)
	c, _ := loadConfig()
	if rc := c.cmdImport(); rc != 0 {
		t.Fatal("import failed")
	}
	c, _ = loadConfig()
	want := []Account{
		{Name: "personal", Dir: "~/.claude-swap-backup/sessions/1-me_example.com", Group: "personal"},
		{Name: "work", Dir: "~/.claude", Group: "team", Color: "208"},
	}
	if len(c.Accounts) != 2 || c.Accounts[0].Name != want[0].Name || c.Accounts[0].Dir != want[0].Dir ||
		c.Accounts[1].Dir != want[1].Dir || c.Accounts[1].Color != "208" || c.Accounts[1].Group != "team" {
		t.Fatalf("accounts = %+v", c.Accounts)
	}
	if c.Default != "work" || c.Language != "ko" || !c.SkipPermissions || !c.AutoUpdate || !c.LevelChip {
		t.Fatalf("settings = %+v", c)
	}
	if rc := c.cmdImport(); rc != 0 {
		t.Fatal("re-import failed")
	}
	if c2, _ := loadConfig(); len(c2.Accounts) != 2 {
		t.Fatal("re-import must not duplicate accounts")
	}
}

func TestAddLinksSharedItemsAndGuardsNames(t *testing.T) {
	s := newSandbox(t)
	s.write(".claude/settings.json", "{}")
	s.write(".claude/CLAUDE.md", "rules")
	c, _ := loadConfig()
	if rc := c.cmdAdd([]string{"side", "--group", "personal", "--no-login"}); rc != 0 {
		t.Fatal("add failed")
	}
	if l, err := os.Readlink(s.path(".claude-accounts/side/CLAUDE.md")); err != nil || l != s.path(".claude/CLAUDE.md") {
		t.Fatalf("CLAUDE.md link = %q %v", l, err)
	}
	if fileExists(s.path(".claude-accounts/side/projects")) {
		t.Fatal("conversations must not be shared")
	}
	for _, bad := range []string{"list", "Bad Name", "side"} {
		if rc := c.cmdAdd([]string{bad, "--no-login"}); rc == 0 {
			t.Errorf("add %q should fail", bad)
		}
	}
	if broken := os.WriteFile(configPath(), []byte("{"), 0o600); broken != nil {
		t.Fatal(broken)
	}
	if _, err := loadConfig(); err == nil {
		t.Fatal("a broken config must not load as empty")
	}
}
