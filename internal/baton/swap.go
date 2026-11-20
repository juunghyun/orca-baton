package baton

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const markerTTL = time.Hour // 이보다 오래된 예약은 버린다 (예전 예약이 뒤늦게 발화하는 사고 방지)

type marker struct {
	Target string  `json:"target"`
	From   string  `json:"from"`
	SID    string  `json:"sid"`
	At     float64 `json:"at"`
}

func markerPath(tab int) string { return filepath.Join(stateDir(), "swap", strconv.Itoa(tab)+".json") }

func peekMarker(tab int) (marker, bool) {
	var m marker
	if readJSON(markerPath(tab), &m) != nil || m.Target == "" {
		return m, false
	}
	if time.Since(time.Unix(int64(m.At), 0)) > markerTTL || (m.SID != "" && !validSID(m.SID)) {
		return m, false
	}
	return m, true
}

// takeMarker 는 예약을 읽고 지운다. 깨졌거나 낡은 예약도 지운다.
func takeMarker(tab int) (marker, bool) {
	m, ok := peekMarker(tab)
	os.Remove(markerPath(tab))
	return m, ok
}

// 한 줄짜리 `/swap …` 만. 여러 줄 프롬프트의 뒷부분(붙여 넣은 글 속 --yes 등)이 인자로 섞이지 않게.
var swapCmdRE = regexp.MustCompile(`^/swap(?:[ \t]+([^\n]*))?$`)

// requestSwap 은 /swap 을 처리한다(훅과 `baton swap` 이 같이 쓴다). 반환: 0 예약됨·목록, 1 잘못된 인자,
// 2 그룹이 달라 승인 필요, 3 래퍼 밖이라 자동으로 못 이어 감.
func (c *Config) requestSwap(args []string, sid string, w io.Writer) int {
	yes := false
	var rest []string
	for _, a := range args {
		switch a {
		case "--yes", "-y":
			yes = true
		default:
			if !strings.HasPrefix(a, "-") {
				rest = append(rest, a)
			}
		}
	}
	cur := c.current()
	if len(rest) == 0 {
		fmt.Fprintln(w, L("Accounts", "계정 목록")+"  ("+L("current: ", "현재: ")+cur.Name+")")
		for _, a := range c.all() {
			mark := " "
			if a.Name == cur.Name {
				mark = "●"
			}
			fmt.Fprintf(w, "  %s %-12s [%s]  %s\n", mark, a.Name, a.Group, tilde(a.Dir))
		}
		fmt.Fprintln(w, "\n"+L("Usage: /swap <account> [--yes]", "사용법: /swap <계정> [--yes]"))
		return 0
	}
	target, ok := c.find(rest[0])
	if !ok {
		fmt.Fprintf(w, L("✗ No account %q. Run /swap alone to list accounts.\n", "✗ %q 계정이 없습니다. /swap 만 입력하면 목록이 나옵니다.\n"), clean(rest[0]))
		return 1
	}
	if target.Name == cur.Name {
		fmt.Fprintf(w, L("✗ Already on %s.\n", "✗ 이미 %s 계정입니다.\n"), target.Name)
		return 1
	}
	if sid != "" && !validSID(sid) {
		fmt.Fprintf(w, L("✗ Invalid session id %q\n", "✗ 세션 ID 형식이 아닙니다: %q\n"), clean(sid))
		return 1
	}
	if crossing(cur, target) && !yes {
		fmt.Fprintf(w, L("⚠ This moves the conversation to another group: %s → %s\n", "⚠ 그룹이 다른 계정으로 옮깁니다: %s → %s\n"),
			strings.ToUpper(cur.Group), strings.ToUpper(target.Group))
		fmt.Fprintf(w, L("  The whole transcript is copied to %s, and further requests count against that account.\n",
			"  이 대화의 트랜스크립트 전체가 %s 로 복사되고, 이후 요청은 그 계정 사용량으로 처리됩니다.\n"), target.Name)
		fmt.Fprintln(w, L("  Make sure it's OK to move this conversation between these accounts (e.g. work ↔ personal).",
			"  업무 대화를 개인 계정으로(또는 그 반대로) 옮겨도 되는지 확인하세요."))
		fmt.Fprintf(w, L("  To go ahead: /swap %s --yes\n", "  진행하려면: /swap %s --yes\n"), target.Name)
		return 2
	}

	tab, _ := strconv.Atoi(os.Getenv("BATON_TAB"))
	if tab <= 1 || !isAncestor(tab) {
		fmt.Fprintln(w, L("! Not running under the baton wrapper, so it can't relaunch for you. Exit Claude Code, then run:",
			"! baton 래퍼로 띄운 세션이 아니라 자동으로 이어 갈 수 없습니다. Claude Code 를 끝낸 뒤 실행하세요:"))
		if sid != "" {
			fmt.Fprintf(w, "  baton %s --resume %s\n", target.Name, sid)
		} else {
			fmt.Fprintf(w, "  baton %s\n", target.Name)
		}
		return 3
	}
	m := marker{Target: target.Name, From: cur.Name, SID: sid, At: float64(time.Now().Unix())}
	b, _ := json.Marshal(m)
	err := os.MkdirAll(filepath.Dir(markerPath(tab)), 0o700)
	if err == nil {
		err = writeFileAtomic(markerPath(tab), b, 0o600)
	}
	if err == nil {
		err = syscall.Kill(tab, syscall.SIGUSR1)
	}
	if err != nil {
		os.Remove(markerPath(tab))
		fmt.Fprintf(w, "✗ %v\n", err)
		return 1
	}
	fmt.Fprintf(w, L("→ Moving this conversation to %s. It continues in this tab in a moment.\n",
		"→ 이 대화를 %s 계정으로 옮깁니다. 잠시 뒤 이 탭에서 이어집니다.\n"), target.Name)
	return 0
}

func crossing(a, b Account) bool { return a.Group != "" && b.Group != "" && a.Group != b.Group }

// isAncestor: BATON_TAB 이 정말 이 프로세스의 조상(래퍼)인지. 새어 나간 환경 변수로 엉뚱한 프로세스에 신호를 보내지 않게.
func isAncestor(pid int) bool {
	cur := os.Getppid()
	for range 12 {
		if cur == pid {
			return true
		}
		if cur <= 1 {
			return false
		}
		out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(cur)).Output()
		if err != nil {
			return false
		}
		if cur, err = strconv.Atoi(strings.TrimSpace(string(out))); err != nil {
			return false
		}
	}
	return false
}

// hookPrompt 는 UserPromptSubmit 훅. `/swap …` 이면 모델 호출 없이 처리하고 exit 2 로 프롬프트를 막는다
// (exit 2 = 차단 + stderr 를 사용자에게 표시). 그래서 한도에 걸린 세션에서도 동작한다. 아니면 조용히 0.
func (c *Config) hookPrompt(stdin io.Reader) int {
	var in struct {
		Prompt    string `json:"prompt"`
		SessionID string `json:"session_id"`
	}
	b, _ := io.ReadAll(io.LimitReader(stdin, 4<<20))
	if json.Unmarshal(b, &in) != nil {
		return 0
	}
	m := swapCmdRE.FindStringSubmatch(strings.TrimSpace(in.Prompt))
	if m == nil {
		return 0
	}
	c.requestSwap(strings.Fields(m[1]), in.SessionID, os.Stderr)
	return 2
}
