package baton

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestRouteRules(t *testing.T) {
	s := newSandbox(t)
	c := s.twoAccounts(func(c *Config) {
		c.Rules = append(c.Rules, Rule{Path: "~/personal/team-side", Account: "team"})
	})
	cases := map[string]string{
		"personal/proj":          "personal",
		"personal/team-side/sub": "team", // 긴 접두사가 이긴다
		"elsewhere":              "team", // 규칙에 없으면 ~/.claude 계정
		"personalx":              "team", // 접두사는 폴더 경계로만
	}
	for rel, want := range cases {
		if got, _ := c.route(s.mkdir(rel), nil); got.Name != want {
			t.Errorf("%s → %s, want %s", rel, got.Name, want)
		}
	}

	// Orca 워크트리 작업 공간: 경로는 규칙 밖이지만 원본 저장소가 ~/personal 아래.
	s.mkdir("personal/repo/.git/worktrees/wt")
	wt := s.mkdir("orca/workspaces/repo/wt")
	s.write("orca/workspaces/repo/wt/.git", "gitdir: "+s.path("personal/repo/.git/worktrees/wt")+"\n")
	if got, why := c.route(filepath.Join(wt), nil); got.Name != "personal" || why != "rule" {
		t.Errorf("worktree → %s (%s), want personal", got.Name, why)
	}
	if got := worktreeOrigin(s.mkdir("orca/workspaces/repo/wt/deep")); got != s.path("personal/repo") {
		t.Errorf("origin from a subfolder = %q", got)
	}

	// --resume 은 대화 파일이 있는 계정. 둘 다 있으면 최근에 쓴 쪽.
	s.write(".claude-accounts/personal/projects/-x/sid-a.jsonl", "{}")
	if got, why := c.route(s.path("elsewhere"), []string{"--resume", "sid-a"}); got.Name != "personal" || why != "resume" {
		t.Errorf("resume → %s (%s), want personal", got.Name, why)
	}
	s.write(".claude/projects/-x/sid-a.jsonl", "{}")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(s.path(".claude-accounts/personal/projects/-x/sid-a.jsonl"), old, old)
	if got, _ := c.route(s.path("personal/proj"), []string{"-r", "sid-a"}); got.Name != "team" {
		t.Errorf("resume with two copies → %s, want the newer (team)", got.Name)
	}
	if got, why := c.route(s.path("personal/proj"), []string{"--resume=../etc"}); got.Name != "personal" || why != "rule" {
		t.Errorf("bad session id must fall back to rules, got %s (%s)", got.Name, why)
	}
}

func TestClaudeArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		sid  string
	}{
		{[]string{"--resume", "abc-1"}, "abc-1"},
		{[]string{"--model", "opus", "-r", "abc"}, "abc"},
		{[]string{"--resume=abc"}, "abc"},
		{[]string{"--resume", "--model", "x"}, ""}, // 피커로 고르는 --resume
		{[]string{"-c"}, ""},
	} {
		if got := resumeSID(tc.args); got != tc.sid {
			t.Errorf("resumeSID(%v) = %q, want %q", tc.args, got, tc.sid)
		}
	}
	in := []string{"--plugin-dir", "/orca/view", "--mcp-config=/x.json", "--resume", "old", "-c", "--session-id", "u",
		"--dangerously-skip-permissions", "-w", "branch", "fix the bug"}
	want := []string{"--plugin-dir", "/orca/view", "--mcp-config=/x.json", "--dangerously-skip-permissions"}
	if got := relaunchArgs(in); !slices.Equal(got, want) {
		t.Errorf("relaunchArgs = %v, want %v", got, want)
	}
	if !hasPermissionFlag([]string{"--permission-mode=plan"}) || hasPermissionFlag([]string{"--model", "x"}) {
		t.Error("hasPermissionFlag")
	}
}
