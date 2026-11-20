package baton

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const Version = "dev"

var subcommands = []string{"claude", "add", "list", "ls", "rm", "alias", "rule", "set", "lang", "import",
	"install", "uninstall", "hook", "statusline", "swap", "version", "help"}

// 새 계정 폴더에서 ~/.claude 로 링크해 같이 쓰는 것. 대화·메모리(projects)와 history.jsonl 은 계정별로 둔다.
var sharedItems = []string{"settings.json", "keybindings.json", "CLAUDE.md", "skills", "commands", "agents"}

func usage(w io.Writer) {
	fmt.Fprint(w, L(`baton: start Claude Code on the right account and move conversations between accounts

  baton [claude args…]          Start Claude Code; the account follows your folder rules
  baton claude [claude args…]   Same (use this as Orca's Claude command)
  baton <account> [args…]       Start on that account (overrides rules)

  baton add <name> [--dir D] [--group G] [--color C] [--no-login]
  baton list | rm <name> | alias <name> <new-name>
  baton rule [add <path> <account> | rm <path> | default <account>]
  baton set bypass|update|chip on|off
  baton lang [en|ko]
  baton import                  Bring over accounts and groups from cc-baton 0.1 (Python)
  baton install | uninstall     Add or remove /swap, its hook and the HUD in ~/.claude/settings.json

Inside Claude Code: /swap <account> [--yes]
`, `baton: Claude Code 를 맞는 계정으로 띄우고, 대화를 계정 사이로 옮긴다

  baton [claude 인자…]          Claude Code 실행. 계정은 폴더 규칙을 따른다
  baton claude [claude 인자…]   위와 같음 (Orca 의 Claude 실행 명령에 쓴다)
  baton <계정> [인자…]          그 계정으로 실행 (규칙보다 우선)

  baton add <이름> [--dir 폴더] [--group 그룹] [--color 색] [--no-login]
  baton list | rm <이름> | alias <이름> <새 이름>
  baton rule [add <경로> <계정> | rm <경로> | default <계정>]
  baton set bypass|update|chip on|off
  baton lang [en|ko]
  baton import                  cc-baton 0.1(Python) 의 계정·그룹 가져오기
  baton install | uninstall     ~/.claude/settings.json 에 /swap·훅·HUD 넣기/빼기

Claude Code 안에서: /swap <계정> [--yes]
`))
}

// Main 은 baton 바이너리의 진입점. argv[0] 이 baton-claude 면 `baton claude` 와 같다
// (Orca 실행 명령 칸이 공백 있는 명령을 못 받을 때 쓰는 링크).
func Main(argv []string) int {
	cfg, err := loadConfig()
	args := argv[1:]
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	if err != nil {
		setLang("")
		switch sub {
		case "hook":
			return 0 // 깨진 설정 때문에 모든 프롬프트가 막히면 안 된다
		case "statusline":
			fmt.Println("[baton] " + err.Error())
			return 0
		}
		fmt.Fprintln(os.Stderr, "✗ "+err.Error())
		return 1
	}
	setLang(cfg.Language)
	if filepath.Base(argv[0]) == "baton-claude" {
		return cfg.wrapRouted(args)
	}
	if sub == "" || (strings.HasPrefix(sub, "-") && !slices.Contains([]string{"-h", "--help", "-V", "--version"}, sub)) {
		return cfg.wrapRouted(args)
	}
	rest := args[1:]
	switch sub {
	case "claude":
		return cfg.wrapRouted(rest)
	case "-V", "--version", "version":
		fmt.Println("baton " + Version)
		return 0
	case "-h", "--help", "help":
		usage(os.Stdout)
		return 0
	case "add":
		return cfg.cmdAdd(rest)
	case "list", "ls":
		return cfg.cmdList()
	case "rm":
		return cfg.cmdRm(rest)
	case "alias":
		return cfg.cmdAlias(rest)
	case "rule":
		return cfg.cmdRule(rest)
	case "set":
		return cfg.cmdSet(rest)
	case "lang":
		return cfg.cmdLang(rest)
	case "import":
		return cfg.cmdImport()
	case "hook":
		if len(rest) == 1 && rest[0] == "prompt" {
			return cfg.hookPrompt(os.Stdin)
		}
		fmt.Fprintln(os.Stderr, "usage: baton hook prompt   (hook JSON on stdin)")
		return 1
	case "swap":
		return cfg.requestSwap(rest, os.Getenv("CLAUDE_CODE_SESSION_ID"), os.Stdout)
	}
	if acc, ok := cfg.find(sub); ok {
		if len(rest) > 0 && rest[0] == "--" {
			rest = rest[1:]
		}
		return cfg.runWrapper(acc, rest, true)
	}
	fmt.Fprintf(os.Stderr, L("✗ %q is neither a command nor an account. `baton list` shows accounts.\n",
		"✗ %q 는 명령도 계정도 아닙니다. 계정 목록은 `baton list`.\n"), sub)
	return 2
}

func (c *Config) wrapRouted(args []string) int {
	cwd, _ := os.Getwd()
	acc, _ := c.route(cwd, args)
	return c.runWrapper(acc, args, false)
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "✗ "+err.Error())
	return 1
}

func (c *Config) cmdAdd(args []string) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fail(errors.New(L("usage: baton add <name> [--dir D] [--group G] [--color C] [--no-login]",
			"사용법: baton add <이름> [--dir 폴더] [--group 그룹] [--color 색] [--no-login]")))
	}
	name := strings.ToLower(args[0])
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	dir := fs.String("dir", "", "")
	group := fs.String("group", "", "")
	color := fs.String("color", "", "")
	noLogin := fs.Bool("no-login", false, "")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if err := validName(name); err != nil {
		return fail(err)
	}
	if _, ok := c.find(name); ok {
		return fail(fmt.Errorf(L("account %q already exists", "%q 계정이 이미 있습니다"), name))
	}
	if *dir == "" {
		*dir = filepath.Join(home(), ".claude-accounts", name)
	}
	acc := Account{Name: name, Dir: tilde(*dir), Group: strings.ToLower(*group), Color: *color}
	if other, ok := c.byDir(acc.Dir); ok {
		return fail(fmt.Errorf(L("%s is already account %q", "%s 는 이미 %q 계정의 폴더입니다"), acc.Dir, other.Name))
	}
	if !acc.isDefault() {
		if err := os.MkdirAll(acc.dir(), 0o700); err != nil {
			return fail(err)
		}
		linkShared(acc.dir())
	}
	c.Accounts = append(c.Accounts, acc)
	if err := c.save(); err != nil {
		return fail(err)
	}
	fmt.Printf(L("✓ added %s (%s)\n", "✓ %s 추가 (%s)\n"), name, acc.Dir)
	if *noLogin || acc.isDefault() {
		return 0
	}
	bin, err := claudeBin()
	if err != nil {
		return fail(err)
	}
	fmt.Println(L("Claude Code opens on the new account folder. Log in with /login, then exit.",
		"새 계정 폴더로 Claude Code 를 띄웁니다. /login 으로 로그인한 뒤 끝내세요."))
	cmd := exec.Command(bin)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = launchEnv(acc, "")
	return exitCode(cmd.Run())
}

// linkShared 는 새 계정 폴더에 공유 항목 링크를 만든다. 이미 있는 건(사용자 파일) 건드리지 않는다.
func linkShared(dir string) {
	for _, name := range sharedItems {
		from, to := filepath.Join(defaultDir(), name), filepath.Join(dir, name)
		li, err := os.Lstat(from)
		if err != nil {
			continue
		}
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		target := from
		if li.Mode()&os.ModeSymlink != 0 {
			if r, err := filepath.EvalSymlinks(from); err == nil {
				target = r
			}
		}
		os.Symlink(target, to)
	}
}

func (c *Config) cmdList() int {
	fb := c.fallback()
	for _, a := range c.all() {
		mark := " "
		if a.Name == fb.Name {
			mark = "*"
		}
		group := a.Group
		if group == "" {
			group = "-"
		}
		fmt.Printf("%s %-12s %-10s %s\n", mark, a.Name, group, tilde(a.Dir))
	}
	fmt.Println(L("  (* = when no rule matches)", "  (* = 규칙에 없을 때)"))
	if len(c.Rules) > 0 {
		fmt.Println(L("Rules:", "규칙:"))
		for _, r := range c.Rules {
			fmt.Printf("  %s → %s\n", r.Path, r.Account)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		acc, why := c.route(cwd, nil)
		fmt.Printf(L("This folder: %s (%s)\n", "이 폴더: %s (%s)\n"), acc.Name, why)
	}
	return 0
}

func (c *Config) cmdRm(args []string) int {
	if len(args) != 1 {
		return fail(errors.New(L("usage: baton rm <name>", "사용법: baton rm <이름>")))
	}
	acc, ok := c.find(args[0])
	if !ok {
		return fail(fmt.Errorf(L("no account %q", "%q 계정이 없습니다"), args[0]))
	}
	c.Accounts = slices.DeleteFunc(c.Accounts, func(a Account) bool { return a.Name == acc.Name })
	c.Rules = slices.DeleteFunc(c.Rules, func(r Rule) bool { return r.Account == acc.Name })
	if c.Default == acc.Name {
		c.Default = ""
	}
	if err := c.save(); err != nil {
		return fail(err)
	}
	fmt.Printf(L("✓ removed %s. Its folder %s is left as is.\n", "✓ %s 제거. 폴더 %s 는 그대로 둡니다.\n"), acc.Name, acc.Dir)
	return 0
}

func (c *Config) cmdAlias(args []string) int {
	if len(args) != 2 {
		return fail(errors.New(L("usage: baton alias <name> <new-name>", "사용법: baton alias <이름> <새 이름>")))
	}
	old, nu := strings.ToLower(args[0]), strings.ToLower(args[1])
	if _, ok := c.find(old); !ok {
		return fail(fmt.Errorf(L("no account %q", "%q 계정이 없습니다"), old))
	}
	if err := validName(nu); err != nil {
		return fail(err)
	}
	if _, ok := c.find(nu); ok {
		return fail(fmt.Errorf(L("account %q already exists", "%q 계정이 이미 있습니다"), nu))
	}
	for i := range c.Accounts {
		if c.Accounts[i].Name == old {
			c.Accounts[i].Name = nu
		}
	}
	for i := range c.Rules {
		if c.Rules[i].Account == old {
			c.Rules[i].Account = nu
		}
	}
	if c.Default == old {
		c.Default = nu
	}
	if err := c.save(); err != nil {
		return fail(err)
	}
	fmt.Printf("✓ %s → %s\n", old, nu)
	return 0
}

func (c *Config) cmdRule(args []string) int {
	if len(args) == 0 {
		for _, r := range c.Rules {
			fmt.Printf("%s → %s\n", r.Path, r.Account)
		}
		fmt.Printf(L("otherwise → %s\n", "그 밖 → %s\n"), c.fallback().Name)
		return 0
	}
	switch {
	case args[0] == "add" && len(args) == 3:
		if _, ok := c.find(args[2]); !ok {
			return fail(fmt.Errorf(L("no account %q", "%q 계정이 없습니다"), args[2]))
		}
		p := tilde(args[1])
		c.Rules = slices.DeleteFunc(c.Rules, func(r Rule) bool { return tilde(r.Path) == p })
		c.Rules = append(c.Rules, Rule{Path: p, Account: strings.ToLower(args[2])})
	case args[0] == "rm" && len(args) == 2:
		p := tilde(args[1])
		n := len(c.Rules)
		c.Rules = slices.DeleteFunc(c.Rules, func(r Rule) bool { return tilde(r.Path) == p })
		if len(c.Rules) == n {
			return fail(fmt.Errorf(L("no rule for %s", "%s 규칙이 없습니다"), p))
		}
	case args[0] == "default" && len(args) == 2:
		if _, ok := c.find(args[1]); !ok {
			return fail(fmt.Errorf(L("no account %q", "%q 계정이 없습니다"), args[1]))
		}
		c.Default = strings.ToLower(args[1])
	default:
		return fail(errors.New(L("usage: baton rule [add <path> <account> | rm <path> | default <account>]",
			"사용법: baton rule [add <경로> <계정> | rm <경로> | default <계정>]")))
	}
	if err := c.save(); err != nil {
		return fail(err)
	}
	return c.cmdRule(nil)
}

func (c *Config) cmdSet(args []string) int {
	if len(args) != 2 || (args[1] != "on" && args[1] != "off") {
		return fail(errors.New(L("usage: baton set bypass|update|chip on|off", "사용법: baton set bypass|update|chip on|off")))
	}
	on := args[1] == "on"
	switch args[0] {
	case "bypass":
		c.SkipPermissions = on
	case "update":
		c.AutoUpdate = on
	case "chip":
		c.LevelChip = on
	default:
		return fail(fmt.Errorf(L("unknown switch %q", "모르는 스위치 %q"), args[0]))
	}
	if err := c.save(); err != nil {
		return fail(err)
	}
	fmt.Printf("✓ %s %s\n", args[0], args[1])
	return 0
}

func (c *Config) cmdLang(args []string) int {
	if len(args) == 0 {
		fmt.Println(lang)
		return 0
	}
	if args[0] != "en" && args[0] != "ko" {
		return fail(errors.New("usage: baton lang en|ko"))
	}
	c.Language = args[0]
	if err := c.save(); err != nil {
		return fail(err)
	}
	setLang(args[0])
	return 0
}
