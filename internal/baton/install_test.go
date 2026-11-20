package baton

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallIsIdempotentAndKeepsOthers(t *testing.T) {
	s := newSandbox(t)
	t.Setenv("BATON_BIN", s.path(".local/bin/baton"))
	orig := `{
  "model": "opus",
  "hooks": {
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "other-tool <in>"}]}]
  },
  "env": {"A": "1"}
}
`
	s.write(".claude/settings.json", orig)
	if rc := cmdInstall(); rc != 0 {
		t.Fatal("install failed")
	}
	got := s.read(".claude/settings.json")
	for _, want := range []string{`"other-tool <in>"`, `"command": "\"$HOME/.local/bin/baton\" hook prompt"`,
		`"command": "\"$HOME/.local/bin/baton\" statusline"`} {
		if !strings.Contains(got, want) {
			t.Errorf("settings missing %s:\n%s", want, got)
		}
	}
	if i, j, k := strings.Index(got, `"model"`), strings.Index(got, `"hooks"`), strings.Index(got, `"env"`); !(i < j && j < k) {
		t.Errorf("key order changed:\n%s", got)
	}
	if !strings.Contains(s.read(".claude/commands/swap.md"), cmdMark) {
		t.Error("/swap command not installed")
	}
	baks, _ := filepath.Glob(s.path(".claude/settings.json.bak-baton-*"))
	if len(baks) != 1 {
		t.Fatalf("want one backup, got %v", baks)
	}

	cmdInstall()
	if again := s.read(".claude/settings.json"); again != got {
		t.Fatalf("second install changed settings:\n%s", again)
	}
	if baks2, _ := filepath.Glob(s.path(".claude/settings.json.bak-baton-*")); len(baks2) != 1 {
		t.Fatalf("second install must not back up again: %v", baks2)
	}
	if n := strings.Count(s.read(".claude/settings.json"), "hook prompt"); n != 1 {
		t.Fatalf("hook added %d times", n)
	}

	cmdUninstall()
	got = s.read(".claude/settings.json")
	if strings.Contains(got, "baton") || !strings.Contains(got, "other-tool <in>") || fileExists(s.path(".claude/commands/swap.md")) {
		t.Fatalf("uninstall should remove only ours:\n%s", got)
	}
}

func TestInstallStopsOnBrokenJSONAndForeignStatusline(t *testing.T) {
	s := newSandbox(t)
	s.write(".claude/settings.json", "{broken")
	if rc := cmdInstall(); rc != 1 || s.read(".claude/settings.json") != "{broken" {
		t.Fatal("broken settings.json must be left alone")
	}
	s.write(".claude/settings.json", `{"hooks": {"UserPromptSubmit": {"odd": true}}}`)
	if rc := cmdInstall(); rc != 1 || !strings.Contains(s.read(".claude/settings.json"), "odd") {
		t.Fatal("an unexpected hooks shape must be left alone")
	}
	s.write(".claude/settings.json", `{"statusLine": {"type": "command", "command": "my-line"}}`)
	s.write(".claude/commands/swap.md", "mine")
	cmdInstall()
	if !strings.Contains(s.read(".claude/settings.json"), "my-line") || s.read(".claude/commands/swap.md") != "mine" {
		t.Fatal("someone else's statusline and /swap command stay")
	}
}
