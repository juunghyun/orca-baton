// Package baton 은 Orca·일반 터미널에서 Claude Code 를 계정별로 띄우는 래퍼다.
// 로그인은 계정 폴더가 직접 갖는다. baton 은 토큰을 저장·갱신·읽지 않는다.
package baton

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Account 는 Claude Code 설정 폴더 하나. Dir 이 ~/.claude 면 CLAUDE_CONFIG_DIR 없이 띄운다.
type Account struct {
	Name  string            `json:"name"`
	Dir   string            `json:"dir"`
	Group string            `json:"group,omitempty"`
	Color string            `json:"color,omitempty"` // 256색 번호 또는 #RRGGBB
	Args  []string          `json:"args,omitempty"`  // 이 계정으로 띄울 때만 붙는 claude 인자
	Env   map[string]string `json:"env,omitempty"`
}

// Rule 은 폴더 경로 접두사 → 계정. 긴 경로가 이긴다.
type Rule struct {
	Path    string `json:"path"`
	Account string `json:"account"`
}

type Config struct {
	Accounts        []Account `json:"accounts"`
	Rules           []Rule    `json:"rules,omitempty"`
	Default         string    `json:"default,omitempty"` // 규칙에 없을 때. 비면 ~/.claude 계정
	Language        string    `json:"language,omitempty"`
	SkipPermissions bool      `json:"skipPermissions,omitempty"`
	AutoUpdate      bool      `json:"autoUpdate,omitempty"`
	LevelChip       bool      `json:"levelChip,omitempty"`
}

func home() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func xdg(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return filepath.Join(home(), fallback)
}

// cc-baton(Python 판)의 ~/.config/cc-baton 과 다른 폴더. 전환 중에 서로 덮어쓰지 않게.
func configPath() string {
	return filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), "orca-baton", "config.json")
}
func stateDir() string   { return filepath.Join(xdg("XDG_STATE_HOME", ".local/state"), "orca-baton") }
func defaultDir() string { return filepath.Join(home(), ".claude") }

// expand 는 ~/ 를 풀고 정리한다. 설정 파일엔 ~/ 로 적어 사용자 이름과 무관하게 둔다.
func expand(p string) string {
	if p == "~" {
		return home()
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(home(), p[2:])
	}
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}

func tilde(p string) string {
	p = expand(p)
	if h := home(); p == h || strings.HasPrefix(p, h+"/") {
		return "~" + p[len(h):]
	}
	return p
}

func (a Account) dir() string     { return expand(a.Dir) }
func (a Account) isDefault() bool { return a.dir() == defaultDir() }

var errBrokenConfig = errors.New("broken config")

func loadConfig() (*Config, error) {
	b, err := os.ReadFile(configPath())
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		// 깨진 설정을 빈 설정으로 덮어쓰지 않게 여기서 멈춘다.
		return nil, fmt.Errorf("%w: %s: %v", errBrokenConfig, configPath(), err)
	}
	return &c, nil
}

func (c *Config) save() error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(p, append(b, '\n'), 0o600)
}

// writeFileAtomic 은 같은 폴더의 임시 파일에 쓰고 rename 한다. 중간에 죽어도 원본은 온전하다.
func writeFileAtomic(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // rename 뒤엔 없는 파일이라 무해하다
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Config) find(name string) (Account, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, a := range c.Accounts {
		if a.Name == name {
			return a, true
		}
	}
	return Account{}, false
}

// fallback 은 규칙에 없을 때 쓰는 계정. 등록이 하나도 없어도 ~/.claude 로는 뜬다.
func (c *Config) fallback() Account {
	if a, ok := c.find(c.Default); ok {
		return a
	}
	for _, a := range c.Accounts {
		if a.isDefault() {
			return a
		}
	}
	return Account{Name: "default", Dir: "~/.claude"}
}

// byDir 은 설정 폴더로 계정을 찾는다 (CLAUDE_CONFIG_DIR 로 띄운 세션이 자기 계정을 알 때).
func (c *Config) byDir(dir string) (Account, bool) {
	dir = expand(dir)
	for _, a := range c.Accounts {
		if a.dir() == dir {
			return a, true
		}
	}
	return Account{}, false
}

// current 는 지금 프로세스가 속한 계정. 래퍼가 BATON_ACCOUNT 를 넘긴다.
func (c *Config) current() Account {
	if a, ok := c.find(os.Getenv("BATON_ACCOUNT")); ok {
		return a
	}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		if a, ok := c.byDir(d); ok {
			return a
		}
		return Account{Name: filepath.Base(d), Dir: d}
	}
	return c.fallback()
}

// all 은 등록된 계정 + (없으면) 기본 폴더. 트랜스크립트를 찾을 때 기본 폴더를 빠뜨리지 않게.
func (c *Config) all() []Account {
	out := slices.Clone(c.Accounts)
	if !slices.ContainsFunc(out, Account.isDefault) {
		out = append(out, c.fallback())
	}
	return out
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,31}$`)

func validName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf(L("account names use a-z, 0-9, _ . - (got %q)", "계정 이름은 a-z, 0-9, _ . - 만 씁니다 (%q)"), name)
	}
	if slices.Contains(subcommands, name) {
		return fmt.Errorf(L("%q is a baton command, pick another name", "%q 는 baton 명령 이름이라 쓸 수 없습니다"), name)
	}
	return nil
}

// sidRE: 세션 ID 는 경로·glob 에 들어간다. 슬래시·와일드카드·.. 가 못 들어오게 좁힌다.
var sidRE = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]{0,127}$`)

func validSID(s string) bool { return sidRE.MatchString(s) && !strings.Contains(s, "..") }

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
