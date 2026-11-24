package baton

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStatuslineSharesRateLimits(t *testing.T) {
	s := newSandbox(t)
	c := s.twoAccounts(nil)
	t.Setenv("BATON_ACCOUNT", "team")
	soon := time.Now().Add(90 * time.Minute).Unix()
	in := `{"model":{"display_name":"Opus 5.5 (1M context)"},"effort":{"level":"high"},"context_window":{"used_percentage":12},
	        "rate_limits":{"five_hour":{"used_percentage":42,"resets_at":` + strconv.FormatInt(soon, 10) + `},"seven_day":{"used_percentage":10,"resets_at":` + strconv.FormatInt(soon, 10) + `}}}`
	var out bytes.Buffer
	c.statusline(strings.NewReader(in), &out)
	plain := ansiRE.ReplaceAllString(out.String(), "")
	for _, want := range []string{"●  team  │ 5h", "42%", "1w", "Opus 5.5[1M]", "high", "ctx 12%"} {
		if !strings.Contains(plain, want) {
			t.Errorf("HUD missing %q:\n%s", want, plain)
		}
	}
	if !fileExists(ratePath("team")) {
		t.Fatal("rate file not written")
	}

	t.Setenv("BATON_ACCOUNT", "personal")
	out.Reset()
	c.statusline(strings.NewReader(`{}`), &out)
	if plain = ansiRE.ReplaceAllString(out.String(), ""); !strings.Contains(plain, "↔ team 42%/10%") {
		t.Fatalf("other account line:\n%s", plain)
	}

	// 낡은 값엔 나이를, 리셋 시각이 지난 창엔 초기화 표시를.
	past := float64(time.Now().Add(-time.Minute).Unix())
	pct := 99.0
	f := rateFile{At: float64(time.Now().Add(-20 * time.Minute).Unix()), rateLimits: rateLimits{FiveHour: &rateWindow{Used: &pct, ResetsAt: &past}}}
	b, _ := json.Marshal(f)
	os.WriteFile(ratePath("team"), b, 0o600)
	out.Reset()
	c.statusline(strings.NewReader(`{}`), &out)
	if plain = ansiRE.ReplaceAllString(out.String(), ""); !strings.Contains(plain, "20m ago ") || !strings.Contains(plain, "team ↺/—") {
		t.Fatalf("stale/reset:\n%s", plain)
	}
}
