package baton

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var batonBin string // TestMain 이 빌드한 진짜 바이너리. 래퍼·훅·신호는 프로세스 경계를 넘어야 시험이 된다.

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "baton-bin-")
	if err != nil {
		panic(err)
	}
	batonBin = filepath.Join(dir, "baton")
	if out, err := exec.Command("go", "build", "-o", batonBin, "../../cmd/baton").CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// 가짜 claude: 받은 계정·인자를 기록한다. FAKE_SWAP 이 있으면 첫 실행에서 대화를 만들고 /swap 을 친 것처럼 훅을 부른다.

const fakeClaude = `#!/bin/sh
{ printf 'run cfg=%s acct=%s name=%s args=' "${CLAUDE_CONFIG_DIR-}" "$BATON_ACCOUNT" "${CLAUDE_CODE_SESSION_NAME-}"; printf '[%s]' "$@"; echo; } >> "$FAKE_LOG"
case " $* " in *" --resume "*) exit "${FAKE_RC:-0}" ;; esac
if [ -n "$FAKE_SWAP" ]; then
  cfg="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
  mkdir -p "$cfg/projects/-work/sid-1/tool-results" "$cfg/sessions"
  echo '{"type":"user"}' > "$cfg/projects/-work/sid-1.jsonl"
  echo out > "$cfg/projects/-work/sid-1/tool-results/a.txt"
  printf '{"name":"work-tab","sessionId":"sid-1"}' > "$cfg/sessions/$$.json"
  printf '{"prompt":"/swap %s","session_id":"sid-1"}' "$FAKE_SWAP" | "$BATON_BIN" hook prompt 2>>"$FAKE_LOG"
  echo "hook=$?" >> "$FAKE_LOG"
  if [ -n "$FAKE_WAIT" ]; then
    trap 'echo term >> "$FAKE_LOG"; exit 0' TERM
    sleep 5 & wait
    echo not-killed >> "$FAKE_LOG"
  fi
fi
exit "${FAKE_RC:-0}"
`

type sandbox struct {
	t    *testing.T
	home string
	fake string
	log  string
}

// newSandbox 는 가짜 HOME 을 만들고 이 프로세스의 환경도 그쪽으로 돌린다. 실사용 설정·계정 폴더에 닿지 않게.

func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	h := t.TempDir()
	if r, err := filepath.EvalSymlinks(h); err == nil {
		h = r // macOS 의 /var → /private/var
	}
	t.Setenv("HOME", h)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "BATON_TAB", "BATON_ACCOUNT", "BATON_BIN", "COLUMNS"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	t.Setenv("LANG", "en_US.UTF-8")
	setLang("")
	s := &sandbox{t: t, home: h, fake: filepath.Join(h, "fake-claude"), log: filepath.Join(h, "fake.log")}
	if err := os.WriteFile(s.fake, []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	s.mkdir(".claude")
	return s
}

func (s *sandbox) path(rel string) string { return filepath.Join(s.home, rel) }

func (s *sandbox) mkdir(rel string) string {
	p := s.path(rel)
	if err := os.MkdirAll(p, 0o755); err != nil {
		s.t.Fatal(err)
	}
	return p
}

func (s *sandbox) write(rel, body string) {
	s.mkdir(filepath.Dir(rel))
	if err := os.WriteFile(s.path(rel), []byte(body), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

func (s *sandbox) read(rel string) string {
	b, _ := os.ReadFile(s.path(rel))
	return string(b)
}

// twoAccounts: team = ~/.claude (기본), personal = 자기 폴더. ~/personal 은 personal 규칙.

func (s *sandbox) twoAccounts(edit func(*Config)) *Config {
	c := &Config{
		Accounts: []Account{
			{Name: "team", Dir: "~/.claude", Group: "team"},
			{Name: "personal", Dir: "~/.claude-accounts/personal", Group: "personal"},
		},
		Rules: []Rule{{Path: "~/personal", Account: "personal"}},
	}
	if edit != nil {
		edit(c)
	}
	s.mkdir(".claude-accounts/personal")
	if err := c.save(); err != nil {
		s.t.Fatal(err)
	}
	return c
}

// run 은 진짜 baton 바이너리를 가짜 HOME·가짜 claude 로 띄운다. PATH 엔 진짜 claude 가 없다.

func (s *sandbox) run(dir string, env []string, args ...string) (int, string) {
	s.t.Helper()
	os.Remove(s.log)
	cmd := exec.Command(batonBin, args...)
	cmd.Dir = dir
	cmd.Env = append([]string{"HOME=" + s.home, "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=en_US.UTF-8",
		"BATON_CLAUDE=" + s.fake, "BATON_BIN=" + batonBin, "FAKE_LOG=" + s.log}, env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		s.t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		s.t.Logf("baton %v\n%s", args, out.String())
		return exitCode(err), s.read("fake.log")
	case <-time.After(20 * time.Second):
		cmd.Process.Kill()
		s.t.Fatalf("baton %v hung\n%s\nlog:\n%s", args, out.String(), s.read("fake.log"))
	}
	return -1, ""
}

func runs(log string) []string {
	var out []string
	for _, l := range strings.Split(log, "\n") {
		if strings.HasPrefix(l, "run ") {
			out = append(out, l)
		}
	}
	return out
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
