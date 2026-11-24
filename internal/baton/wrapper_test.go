package baton

import (
	"os"
	"strings"
	"testing"
)

func TestWrapperPicksAccountArgsAndEnv(t *testing.T) {
	s := newSandbox(t)
	s.twoAccounts(func(c *Config) {
		c.SkipPermissions = true
		c.Accounts[1].Args = []string{"--model", "opus"}
		c.Accounts[1].Env = map[string]string{"X_ACCT": "p"}
	})
	rc, log := s.run(s.mkdir("personal/proj"), []string{"CLAUDE_CONFIG_DIR=/bogus", "ANTHROPIC_API_KEY=leak"},
		"claude", "--plugin-dir", "/x", "hello")
	want := "run cfg=" + s.path(".claude-accounts/personal") + " acct=personal name= args=[--model][opus][--dangerously-skip-permissions][--plugin-dir][/x][hello]"
	if rc != 0 || len(runs(log)) != 1 || runs(log)[0] != want {
		t.Fatalf("rc=%d\n got %q\nwant %q", rc, log, want)
	}

	// 기본 계정은 CLAUDE_CONFIG_DIR 없이. 권한 인자를 직접 주면 건너뛰기를 붙이지 않는다. 종료 코드는 그대로.
	rc, log = s.run(s.mkdir("elsewhere"), []string{"CLAUDE_CONFIG_DIR=/bogus", "FAKE_RC=7"}, "--permission-mode", "plan")
	if want := "run cfg= acct=team name= args=[--permission-mode][plan]"; rc != 7 || runs(log)[0] != want {
		t.Fatalf("rc=%d log=%q", rc, log)
	}

	// baton <계정> 은 규칙을 덮어쓴다. 다른 계정에만 있는 대화를 --resume 하면 먼저 복사한다.
	s.write(".claude/projects/-w/sid-9.jsonl", "{\"a\":1}\n")
	rc, log = s.run(s.path("elsewhere"), nil, "personal", "--resume", "sid-9")
	if rc != 0 || !strings.Contains(runs(log)[0], "acct=personal") || s.read(".claude-accounts/personal/projects/-w/sid-9.jsonl") != "{\"a\":1}\n" {
		t.Fatalf("explicit resume: rc=%d log=%q", rc, log)
	}
}

func TestOrcaLinkAndOneShot(t *testing.T) {
	s := newSandbox(t)
	s.twoAccounts(func(c *Config) {
		c.SkipPermissions = true
		c.Accounts[1].Args = []string{"--model", "opus"}
	})
	link := s.path("orca-bin/claude")
	s.mkdir("orca-bin")
	if err := os.Symlink(batonBin, link); err != nil {
		t.Fatal(err)
	}
	dir := s.mkdir("personal/proj")
	cfg := "run cfg=" + s.path(".claude-accounts/personal") + " acct=personal name= args="

	// claude 라는 이름으로 불리면 래퍼: 규칙 계정, 계정 인자, 권한 건너뛰기.
	rc, log := s.runAs(link, dir, nil, "--plugin-dir", "/x")
	if want := cfg + "[--model][opus][--dangerously-skip-permissions][--plugin-dir][/x]"; rc != 0 || len(runs(log)) != 1 || runs(log)[0] != want {
		t.Fatalf("link: rc=%d\n got %q\nwant %q", rc, log, want)
	}

	// Orca 일회성 호출(-p, --version)은 계정과 계정 인자만. 권한 건너뛰기를 붙이지 않고 종료 코드는 그대로.
	rc, log = s.runAs(link, dir, []string{"FAKE_RC=3"}, "-p", "--output-format", "stream-json")
	if want := cfg + "[--model][opus][-p][--output-format][stream-json]"; rc != 3 || len(runs(log)) != 1 || runs(log)[0] != want {
		t.Fatalf("-p: rc=%d\n got %q\nwant %q", rc, log, want)
	}
	if rc, log = s.runAs(link, dir, nil, "--version"); rc != 0 || runs(log)[0] != cfg+"[--model][opus][--version]" {
		t.Fatalf("--version: rc=%d log=%q", rc, log)
	}
}
