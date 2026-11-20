package baton

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestSwapContinuesInSameTab(t *testing.T) {
	s := newSandbox(t)
	s.twoAccounts(nil)
	rc, log := s.run(s.mkdir("work"), []string{"FAKE_SWAP=personal --yes", "FAKE_WAIT=1"},
		"claude", "--plugin-dir", "/x", "first prompt")
	r := runs(log)
	if rc != 0 || len(r) != 2 {
		t.Fatalf("want two runs, rc=%d log=\n%s", rc, log)
	}
	if !strings.Contains(log, "hook=2") || !strings.Contains(log, "term") || strings.Contains(log, "not-killed") {
		t.Fatalf("hook should block the prompt and the wrapper should SIGTERM claude:\n%s", log)
	}
	want := "run cfg=" + s.path(".claude-accounts/personal") + " acct=personal name=work-tab args=[--resume][sid-1][--plugin-dir][/x]"
	if r[1] != want {
		t.Fatalf("relaunch\n got %q\nwant %q", r[1], want)
	}
	if s.read(".claude-accounts/personal/projects/-work/sid-1.jsonl") == "" || s.read(".claude-accounts/personal/projects/-work/sid-1/tool-results/a.txt") != "out\n" {
		t.Fatal("transcript and its side folder should be copied")
	}
	if s.read(".claude/projects/-work/sid-1.jsonl") == "" {
		t.Fatal("the source copy stays")
	}
	if m, _ := filepath.Glob(s.path(".local/state/orca-baton/swap/*.json")); len(m) != 0 {
		t.Fatalf("marker left behind: %v", m)
	}
}

func TestSwapAcrossGroupsAsksFirst(t *testing.T) {
	s := newSandbox(t)
	s.twoAccounts(nil)
	rc, log := s.run(s.mkdir("work"), []string{"FAKE_SWAP=personal"}, "claude")
	if rc != 0 || len(runs(log)) != 1 || !strings.Contains(log, "another group") || !strings.Contains(log, "/swap personal --yes") {
		t.Fatalf("crossing groups must wait for --yes, rc=%d log=\n%s", rc, log)
	}
	if m, _ := filepath.Glob(s.path(".local/state/orca-baton/swap/*.json")); len(m) != 0 {
		t.Fatalf("no marker without approval: %v", m)
	}
}

func TestSwapRequestOutsideWrapper(t *testing.T) {
	s := newSandbox(t)
	c := s.twoAccounts(nil)
	var out bytes.Buffer
	if rc := c.requestSwap([]string{"personal", "--yes"}, "sid-1", &out); rc != 3 || !strings.Contains(out.String(), "baton personal --resume sid-1") {
		t.Fatalf("rc=%d out=%s", rc, out.String())
	}
	t.Setenv("BATON_TAB", "1") // launchd: 조상이 아니면 신호를 보내지 않는다
	if rc := c.requestSwap([]string{"personal", "--yes"}, "sid-1", &out); rc != 3 {
		t.Fatalf("pid 1 must not be signalled, rc=%d", rc)
	}
	out.Reset()
	if rc := c.requestSwap(nil, "", &out); rc != 0 || !strings.Contains(out.String(), "● team") {
		t.Fatalf("list: rc=%d out=%s", rc, out.String())
	}
	for _, args := range [][]string{{"nobody"}, {"team"}} {
		if rc := c.requestSwap(args, "", &out); rc != 1 {
			t.Errorf("%v → %d, want 1", args, rc)
		}
	}
	if rc := c.hookPrompt(strings.NewReader(`{"prompt":"fix /swap handling","session_id":"s"}`)); rc != 0 {
		t.Errorf("ordinary prompts pass through, got %d", rc)
	}
	if rc := c.hookPrompt(strings.NewReader(`{"prompt":"/swap personal\n--yes"}`)); rc != 0 {
		t.Errorf("multi-line prompt is not a /swap command, got %d", rc)
	}
}
