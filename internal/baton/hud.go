package baton

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// statusline 입력의 rate_limits (Claude Code 2.1.329 실행 파일의 statusLine 입력 설명에서 확인).
// shortcut: 실제 입력은 아직 캡처 전(S0). 모양이 다르면 이 구조체만 고친다.
type rateWindow struct {
	Used     *float64 `json:"used_percentage"`
	ResetsAt *float64 `json:"resets_at"`
}

type rateLimits struct {
	FiveHour *rateWindow `json:"five_hour,omitempty"`
	SevenDay *rateWindow `json:"seven_day,omitempty"`
}

type rateFile struct {
	At float64 `json:"at"`
	rateLimits
}

const (
	R        = "\033[0m"
	dim      = "\033[2m"
	bold     = "\033[1m"
	staleAge = 5 * time.Minute // 이보다 낡은 다른 계정 수치엔 나이를 붙인다
)

func rateDir() string                { return filepath.Join(stateDir(), "rate") }
func ratePath(account string) string { return filepath.Join(rateDir(), account+".json") }

// saveRate 는 이 계정의 잔량을 계정별 파일에 남긴다. 다른 계정 세션의 HUD 가 읽는다(네트워크·토큰 없음).
// 같은 값이면 1분에 한 번만 다시 쓴다(나이 표시는 마지막으로 본 시각이라).
func saveRate(account string, rl rateLimits) {
	if validName(account) != nil && account != "default" {
		return
	}
	var prev rateFile
	if readJSON(ratePath(account), &prev) == nil && sameRate(prev.rateLimits, rl) && time.Since(unix(prev.At)) < time.Minute {
		return
	}
	b, _ := json.Marshal(rateFile{At: float64(time.Now().Unix()), rateLimits: rl})
	if os.MkdirAll(rateDir(), 0o700) == nil {
		writeFileAtomic(ratePath(account), b, 0o600)
	}
}

func sameRate(a, b rateLimits) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func unix(f float64) time.Time { return time.Unix(int64(f), 0) }

func pctColor(p float64) string {
	switch {
	case p >= 90:
		return "\033[38;5;196m"
	case p >= 80:
		return "\033[38;5;208m"
	case p >= 50:
		return "\033[38;5;220m"
	}
	return "\033[38;5;42m"
}

func gauge(p float64) string {
	n := int(math.Round(math.Max(0, math.Min(100, p)) / 100 * 6))
	return pctColor(p) + strings.Repeat("█", n) + dim + strings.Repeat("░", 6-n) + R
}

func short(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm", max(1, int(d.Minutes())))
}

// resetPassed: 리셋 시각이 지난 창은 그 뒤로 새로 쓴 게 없다면 0 에서 다시 시작한 것이다.
func resetPassed(w *rateWindow, now time.Time) bool {
	return w != nil && w.ResetsAt != nil && now.After(unix(*w.ResetsAt))
}

// ownWindow 는 지금 계정의 창: 게이지 + 퍼센트 + 리셋까지 남은 시간.
func ownWindow(label string, w *rateWindow, now time.Time) string {
	if w == nil || w.Used == nil {
		return dim + label + " —" + R
	}
	if resetPassed(w, now) {
		return dim + label + R + " " + gauge(0) + " " + L("reset", "초기화")
	}
	s := dim + label + R + " " + gauge(*w.Used) + " " + pctColor(*w.Used) + fmt.Sprintf("%.0f%%", *w.Used) + R
	if w.ResetsAt != nil {
		s += " " + dim + short(unix(*w.ResetsAt).Sub(now)) + R
	}
	return s
}

func otherPct(w *rateWindow, now time.Time) string {
	switch {
	case w == nil || w.Used == nil:
		return dim + "—" + R
	case resetPassed(w, now):
		return dim + "↺" + R
	}
	return pctColor(*w.Used) + fmt.Sprintf("%.0f%%", *w.Used) + R
}

var palette = []int{39, 208, 42, 171, 220, 81}

func (c *Config) color(a Account) string {
	if n, err := strconv.Atoi(a.Color); err == nil && n >= 0 && n < 256 {
		return fmt.Sprintf("\033[38;5;%dm", n)
	}
	if len(a.Color) == 7 && a.Color[0] == '#' {
		if v, err := strconv.ParseUint(a.Color[1:], 16, 32); err == nil {
			return fmt.Sprintf("\033[38;2;%d;%d;%dm", v>>16, v>>8&0xff, v&0xff)
		}
	}
	// 색을 안 정했으면 그룹 순서대로. 같은 그룹은 같은 색.
	var groups []string
	for _, x := range c.all() {
		key := x.Group
		if key == "" {
			key = "·" + x.Name
		}
		if !slices.Contains(groups, key) {
			groups = append(groups, key)
		}
		if x.Name == a.Name {
			return fmt.Sprintf("\033[38;5;%dm", palette[(len(groups)-1)%len(palette)])
		}
	}
	return "\033[38;5;245m"
}

// effortBit: 시그널 미터 + 이름 (+ 켰으면 레벨 칩). Python 판 HUD 와 같은 모양.
var effortLevels = []string{"low", "medium", "high", "xhigh", "max"}
var effortLv = map[string]int{"low": 1, "medium": 30, "high": 60, "xhigh": 100, "max": 999}

// hue 는 colorsys.hls_to_rgb(deg/360, 0.63, 0.88) 와 같은 색 (Python 판 무지개와 같게).
func hue(deg float64) string {
	h, l, sat := math.Mod(deg, 360)/360, 0.63, 0.88
	m2 := l + sat - l*sat
	m1 := 2*l - m2
	v := func(x float64) int {
		x = math.Mod(x+1, 1)
		c := m1
		switch {
		case x < 1.0/6:
			c = m1 + (m2-m1)*x*6
		case x < 0.5:
			c = m2
		case x < 2.0/3:
			c = m1 + (m2-m1)*(2.0/3-x)*6
		}
		return int(math.Round(c * 255))
	}
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", v(h+1.0/3), v(h), v(h-1.0/3))
}

func (c *Config) effortBit(level string, now time.Time) string {
	k := 0
	for i, l := range effortLevels {
		if l == level {
			k = i + 1
		}
	}
	if k == 0 {
		return dim + "(" + clean(level) + ")" + R
	}
	shift := float64(now.Unix()/10) * 45 // statusline 갱신(10초)마다 무지개가 45° 흐른다
	cells := make([]string, k)
	for i := range cells {
		switch level {
		case "max":
			cells[i] = hue(float64(i)*60 + shift)
		case "xhigh":
			cells[i] = fmt.Sprintf("\033[38;2;255;%d;0m", int(math.Round(208-113*float64(i)/3)))
		default:
			cells[i] = fmt.Sprintf("\033[38;5;%dm", map[string]int{"low": 242, "medium": 67, "high": 33}[level])
		}
	}
	off := map[string]int{"low": 236, "medium": 237}[level]
	if off == 0 {
		off = 238
	}
	meter := ""
	for _, col := range cells {
		meter += col + "▰"
	}
	meter += fmt.Sprintf("\033[38;5;%dm", off) + strings.Repeat("▱", 5-k) + R
	name := map[string]string{"low": dim + "\033[38;5;240mlow", "medium": "\033[38;5;67mmedium",
		"high": bold + "\033[38;5;75mhigh", "xhigh": bold + "\033[38;5;208mxhigh", "max": bold + "\033[38;5;231mMAX"}[level] + R
	if !c.LevelChip {
		return meter + " " + name
	}
	stripe := map[string]string{"low": "\033[38;5;240m", "medium": "\033[38;5;67m", "high": "\033[38;5;33m",
		"xhigh": "\033[38;2;255;135;0m", "max": hue(shift)}[level]
	chip := "\033[1;3;38;5;231;48;5;237m"
	if level == "low" {
		chip = "\033[3;38;5;245;48;5;235m"
	}
	return fmt.Sprintf("%s %s %s▌%s%s Lv.%d %s", meter, name, stripe, R, chip, effortLv[level], R)
}

var ansiRE = regexp.MustCompile(`\033\[[0-9;?]*[A-Za-z]`)

// vislen 은 ANSI 를 뺀 표시 폭 (한글 등 전각은 2칸).
func vislen(s string) int {
	n := 0
	for _, r := range ansiRE.ReplaceAllString(s, "") {
		if r >= 0x1100 && (r <= 0x115f || (r >= 0x2e80 && r <= 0xa4cf) || (r >= 0xac00 && r <= 0xd7a3) ||
			(r >= 0xf900 && r <= 0xfaff) || (r >= 0xfe30 && r <= 0xfe4f) || (r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6)) {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// wrap 은 세그먼트 단위로 줄을 나눈다. 한 줄이 폭을 넘으면 Claude Code 가 '…' 로 잘라 버린다.
func wrap(parts []string, sep string, limit int) []string {
	var lines []string
	for _, p := range parts {
		if n := len(lines); n > 0 && vislen(lines[n-1]+sep+p) <= limit {
			lines[n-1] += sep + p
		} else {
			lines = append(lines, p)
		}
	}
	return lines
}

// statusline: 1행 지금 계정·그룹·이 계정 잔량, 2행 다른 계정 잔량(파일), 3행 모델·effort·컨텍스트·켠 기능.
// 무슨 일이 있어도 한 줄은 낸다.
func (c *Config) statusline(in io.Reader, out io.Writer) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(out, "%s[baton] statusline: %v%s\n", dim, r, R)
		}
	}()
	var d struct {
		Model struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
		Effort struct {
			Level string `json:"level"`
		} `json:"effort"`
		Ctx struct {
			Used *float64 `json:"used_percentage"`
		} `json:"context_window"`
		Rate *rateLimits `json:"rate_limits"`
	}
	b, _ := io.ReadAll(io.LimitReader(in, 4<<20))
	json.Unmarshal(b, &d)
	now := time.Now()
	acc := c.current()
	own := d.Rate
	if own != nil {
		saveRate(acc.Name, *own)
	} else {
		// 첫 응답 전에는 입력에 rate_limits 가 없다. 이 계정이 남긴 마지막 값을 쓴다.
		var f rateFile
		if readJSON(ratePath(acc.Name), &f) == nil {
			own = &f.rateLimits
		}
	}
	limit := 106
	if w, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && w > 0 {
		limit = max(20, w-4)
	}
	sep, pipe := dim+" · "+R, dim+" │ "+R

	col := c.color(acc)
	badge := col + "●" + R + " " + bold + col + acc.Name + R
	if acc.Group != "" && !strings.EqualFold(acc.Group, acc.Name) {
		badge += " " + col + "[" + strings.ToUpper(acc.Group) + "]" + R
	}
	line1 := []string{badge}
	if own != nil {
		if own.FiveHour != nil {
			line1 = append(line1, ownWindow("5h", own.FiveHour, now))
		}
		line1 = append(line1, ownWindow("1w", own.SevenDay, now))
	}
	lines := wrap(line1, pipe, limit)

	var others []string
	for _, a := range c.all() {
		if a.Name == acc.Name {
			continue
		}
		var f rateFile
		if readJSON(ratePath(a.Name), &f) != nil {
			continue
		}
		s := c.color(a) + a.Name + R + " " + otherPct(f.FiveHour, now) + "/" + otherPct(f.SevenDay, now)
		if age := now.Sub(unix(f.At)); age > staleAge {
			s = dim + L(short(age)+" ago ", short(age)+" 전 ") + R + s
		}
		others = append(others, s)
	}
	if len(others) > 0 {
		others[0] = dim + "↔" + R + " " + others[0]
		lines = append(lines, wrap(others, dim+", "+R, limit)...)
	}

	var line3 []string
	if m := strings.NewReplacer(" (1M context)", "[1M]", " (1m context)", "[1M]").Replace(clean(d.Model.DisplayName)); m != "" {
		if d.Effort.Level != "" {
			m += " " + c.effortBit(d.Effort.Level, now)
		}
		line3 = append(line3, m)
	}
	if d.Ctx.Used != nil {
		line3 = append(line3, dim+"ctx"+R+" "+pctColor(*d.Ctx.Used)+fmt.Sprintf("%.0f%%", *d.Ctx.Used)+R)
	}
	if c.SkipPermissions {
		line3 = append(line3, "\033[38;5;214m⚡"+R+L("skip permissions", "권한 확인 건너뜀"))
	}
	if c.AutoUpdate {
		u := "\033[38;5;42m⟳" + R + L("auto-update", "자동 최신화")
		if _, err := os.Stat(filepath.Join(stateDir(), "update-failed")); err == nil {
			u += " " + bold + "\033[38;5;214m" + L("failed", "실패") + R
		}
		line3 = append(line3, u)
	}
	lines = append(lines, wrap(line3, sep, limit)...)
	fmt.Fprintln(out, strings.Join(lines, "\n"))
}
