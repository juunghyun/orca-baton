package baton

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// 인증을 덮어써 고른 계정과 다르게 돌게 만드는 환경 변수. 계정 폴더 로그인으로만 돌게 뺀다(계정 env 로는 다시 넣을 수 있다).
var authOverrides = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN",
	"CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR", "CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR"}

//go:embed claude_update.sh
var updateScript string

func claudeBin() (string, error) {
	p := os.Getenv("BATON_CLAUDE") // 시험용 가짜 claude
	if p == "" {
		var err error
		if p, err = exec.LookPath("claude"); err != nil {
			return "", errors.New(L("claude isn't on PATH. Install Claude Code first.", "PATH 에 claude 가 없습니다. Claude Code 부터 설치하세요."))
		}
	}
	self, _ := os.Executable()
	if realpath(p) == realpath(self) {
		return "", errors.New(L("`claude` points back to baton. Point Orca's Claude command at `baton claude` instead of renaming baton.",
			"`claude` 가 baton 자신을 가리킵니다. baton 이름을 바꾸지 말고 Orca 의 Claude 실행 명령을 `baton claude` 로 두세요."))
	}
	return p, nil
}

func launchEnv(acc Account, sessionName string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "CLAUDE_CONFIG_DIR" || k == "CLAUDE_CODE_SESSION_NAME" || slices.Contains(authOverrides, k) {
			continue
		}
		env = append(env, kv)
	}
	if !acc.isDefault() {
		env = append(env, "CLAUDE_CONFIG_DIR="+acc.dir())
	}
	for k, v := range acc.Env {
		env = append(env, k+"="+v)
	}
	// 훅·statusline 은 claude 의 자식이라 이 값을 물려받는다. BATON_TAB 은 /swap 이 신호를 보낼 래퍼.
	env = append(env, "BATON_TAB="+strconv.Itoa(os.Getpid()), "BATON_ACCOUNT="+acc.Name)
	if sessionName != "" {
		// claude 는 띄울 때마다 이름 꼬리를 새로 뽑는다. --name 은 트랜스크립트 제목까지 덮어 쓰지 않는다.
		env = append(env, "CLAUDE_CODE_SESSION_NAME="+sessionName)
	}
	return env
}

func (c *Config) bypass(argv []string) []string {
	if !c.SkipPermissions || os.Getenv("BATON_NO_BYPASS") != "" || hasPermissionFlag(argv) {
		return nil
	}
	return []string{"--dangerously-skip-permissions"}
}

// runWrapper 는 `baton claude …` / `baton <계정> …`. claude 를 자식으로 띄우고, /swap 이 오면 끝내고
// 같은 탭에서 대상 계정으로 이어 띄운다. 그 밖엔 claude 의 종료 코드로 끝난다.
func (c *Config) runWrapper(acc Account, args []string, explicit bool) int {
	bin, err := claudeBin()
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ "+err.Error())
		return 127
	}
	if explicit {
		c.carryResume(acc, args)
	}
	sessionName := ""
	for {
		if c.AutoUpdate {
			updateClaude()
		}
		argv := slices.Concat(acc.Args, c.bypass(slices.Concat(acc.Args, args)), args)
		cmd := exec.Command(bin, argv...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		cmd.Env = launchEnv(acc, sessionName)
		rc, name := supervise(cmd, acc)

		m, ok := takeMarker(os.Getpid())
		if !ok {
			return rc
		}
		target, ok := c.find(m.Target)
		if !ok || target.Name == acc.Name {
			fmt.Fprintf(os.Stderr, L("! [baton] account %q is gone; not switching.\n", "! [baton] %q 계정이 없어 전환하지 않습니다.\n"), m.Target)
			return rc
		}
		next := relaunchArgs(args)
		if m.SID != "" {
			if err := c.moveConversation(acc, target, m.SID); err != nil {
				fmt.Fprintf(os.Stderr, L("! [baton] couldn't copy the conversation (%v); starting a new one on %s.\n",
					"! [baton] 대화를 복사하지 못해(%v) %s 에서 새 대화로 엽니다.\n"), err, target.Name)
			} else {
				next = append([]string{"--resume", m.SID}, next...)
			}
		}
		fmt.Fprintf(os.Stderr, "→ %s\n", L("continuing on "+target.Name, target.Name+" 계정에서 이어 갑니다"))
		acc, args, sessionName = target, next, name
	}
}

// supervise 는 claude 가 끝날 때까지 기다린다. → (종료 코드, /swap 요청 때 읽어 둔 세션 이름)
func supervise(cmd *exec.Cmd, acc Account) (int, string) {
	sigs := make(chan os.Signal, 4)
	// Ignore 가 아니라 Notify: 무시한 신호는 exec 뒤 자식에도 무시된 채 남는다.
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR1)
	defer signal.Stop(sigs)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "✗ "+err.Error())
		return 127, ""
	}
	done, handled := make(chan struct{}), make(chan string)
	go func() {
		name := ""
		for {
			select {
			case <-done:
				handled <- name
				return
			case s := <-sigs:
				switch s {
				case syscall.SIGINT, syscall.SIGQUIT:
					// 터미널이 같은 프로세스 그룹의 claude 에도 보낸다. 래퍼는 버틴다.
				case syscall.SIGUSR1:
					if _, ok := peekMarker(os.Getpid()); ok {
						// 등록 파일은 claude 가 끝나면 남지 않을 수 있어 지금 읽는다.
						name = sessionName(acc, cmd.Process.Pid)
						go stopChild(cmd.Process, done)
					}
				default:
					cmd.Process.Signal(s) // SIGTERM·SIGHUP 은 claude 에 넘기고 그 종료를 따른다
				}
			}
		}
	}()
	err := cmd.Wait()
	close(done)
	name := <-handled
	return exitCode(err), name
}

// stopChild 는 훅 안내가 화면에 남을 틈을 준 뒤 SIGTERM. claude 는 SIGTERM 을 1초 안에 정상 처리한다(절전 666회 실측).
func stopChild(p *os.Process, done <-chan struct{}) {
	select {
	case <-done:
		return
	case <-time.After(300 * time.Millisecond):
	}
	p.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		p.Kill()
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	return 1
}

// sessionName 은 claude 의 세션 등록 파일(<설정 폴더>/sessions/<pid>.json)에 적힌 탭 이름.
func sessionName(acc Account, pid int) string {
	var reg struct {
		Name string `json:"name"`
	}
	if readJSON(filepath.Join(acc.dir(), "sessions", strconv.Itoa(pid)+".json"), &reg) != nil {
		return ""
	}
	return clean(reg.Name)
}

// carryResume: `baton <계정> --resume <id>` 인데 대화가 다른 계정에만 있으면 먼저 복사한다(수동 이관 경로).
func (c *Config) carryResume(acc Account, args []string) {
	sid := resumeSID(args)
	if sid == "" || findTranscript(acc.dir(), sid) != "" {
		return
	}
	owner, _, ok := c.transcriptOwner(sid)
	if !ok {
		return
	}
	if err := c.moveConversation(owner, acc, sid); err != nil {
		fmt.Fprintf(os.Stderr, L("! [baton] couldn't copy the conversation from %s: %v\n", "! [baton] %s 의 대화를 복사하지 못했습니다: %v\n"), owner.Name, err)
		return
	}
	fmt.Fprintf(os.Stderr, L("→ copied the conversation from %s\n", "→ %s 의 대화를 복사했습니다\n"), owner.Name)
}

// moveConversation 은 트랜스크립트 1개와 옆의 <sid>/ 폴더(떼어 저장한 도구 출력, 서브에이전트 기록)를
// 대상 계정의 같은 프로젝트 폴더로 복사한다. 원본은 남긴다. 다른 대화와 history.jsonl 은 계정별로 남는다.
func (c *Config) moveConversation(from, to Account, sid string) error {
	src := findTranscript(from.dir(), sid)
	if src == "" {
		_, p, ok := c.transcriptOwner(sid)
		if !ok {
			return errors.New(L("transcript not found", "트랜스크립트를 찾지 못함"))
		}
		src = p
	}
	dstDir := filepath.Join(to.dir(), "projects", filepath.Base(filepath.Dir(src)))
	if err := copyFile(src, filepath.Join(dstDir, filepath.Base(src))); err != nil {
		return err
	}
	side := strings.TrimSuffix(src, ".jsonl")
	if fi, err := os.Stat(side); err == nil && fi.IsDir() {
		return filepath.WalkDir(side, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !d.Type().IsRegular() {
				return err
			}
			rel, _ := filepath.Rel(filepath.Dir(side), p)
			return copyFile(p, filepath.Join(dstDir, rel))
		})
	}
	return nil
}

// copyFile 은 임시 파일에 쓰고 rename 한다. 수정 시각은 지금이 된다: 다음 --resume 이 최근 사본(이어 간 쪽)을 고른다.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Chmod(0o600); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(out.Name(), dst)
}

// updateClaude 는 npm 으로 깐 Claude Code 를 띄우기 전에 최신화한다. 실패해도 실행은 막지 않는다(스크립트가 항상 0).
func updateClaude() {
	if os.Getenv("CC_NO_UPDATE") != "" {
		return
	}
	cmd := exec.Command("/bin/bash", "-c", updateScript, "claude-update")
	cmd.Env = append(os.Environ(), "CC_BATON_LANG="+lang)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	cmd.Run()
}

// clean 은 화면에 찍을 외부 문자열에서 제어 문자를 뺀다(터미널 제어 시퀀스 주입 방지).
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
}
