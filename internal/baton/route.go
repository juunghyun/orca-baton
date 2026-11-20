package baton

import (
	"os"
	"path/filepath"
	"strings"
)

// route 는 이 실행이 쓸 계정을 정한다 (orca-plan 결정 3).
//  1. --resume <id> 면 그 대화 파일이 있는 계정 (여럿이면 가장 최근에 쓴 쪽)
//  2. 워크트리면 git 원본 저장소 경로, 아니면 작업 폴더로 규칙(긴 접두사 우선)
//  3. 규칙에 없으면 기본 계정
func (c *Config) route(cwd string, args []string) (Account, string) {
	if sid := resumeSID(args); sid != "" {
		if a, _, ok := c.transcriptOwner(sid); ok {
			return a, "resume"
		}
	}
	paths := []string{cwd}
	if origin := worktreeOrigin(cwd); origin != "" {
		paths = []string{origin, cwd}
	}
	for _, p := range paths {
		if a, ok := c.ruleFor(p); ok {
			return a, "rule"
		}
	}
	return c.fallback(), "default"
}

func realpath(p string) string {
	p = expand(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func under(p, root string) bool { return p == root || strings.HasPrefix(p, root+"/") }

func (c *Config) ruleFor(path string) (Account, bool) {
	path = realpath(path)
	best, bestLen := Account{}, -1
	for _, r := range c.Rules {
		root := realpath(r.Path)
		if !under(path, root) || len(root) <= bestLen {
			continue
		}
		if a, ok := c.find(r.Account); ok {
			best, bestLen = a, len(root)
		}
	}
	return best, bestLen >= 0
}

// worktreeOrigin 은 연결된 git 워크트리면 원본 저장소 경로를 돌려준다. Orca 워크트리 작업 공간
// (~/orca/workspaces/<저장소>/<이름>)은 경로만으로는 소속을 모른다. git 을 띄우지 않고 .git 파일만 읽는다.
func worktreeOrigin(dir string) string {
	dir = realpath(dir)
	for d := dir; ; d = filepath.Dir(d) {
		dotgit := filepath.Join(d, ".git")
		if fi, err := os.Lstat(dotgit); err == nil {
			if fi.IsDir() {
				return "" // 원본 저장소 자신
			}
			b, err := os.ReadFile(dotgit)
			if err != nil {
				return ""
			}
			gd, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
			if !ok {
				return ""
			}
			gd = strings.TrimSpace(gd)
			if !filepath.IsAbs(gd) {
				gd = filepath.Join(d, gd)
			}
			if i := strings.Index(filepath.ToSlash(gd), "/.git/worktrees/"); i > 0 {
				return gd[:i]
			}
			return "" // 서브모듈 등
		}
		if d == "/" || d == "." || d == filepath.Dir(d) {
			return ""
		}
	}
}

// transcriptOwner 는 세션 ID 의 대화 파일을 가진 계정. 스왑을 거치면 여러 계정에 사본이 있으니 가장 최근에 쓴 쪽.
func (c *Config) transcriptOwner(sid string) (Account, string, bool) {
	if !validSID(sid) {
		return Account{}, "", false
	}
	var best Account
	bestPath, bestT := "", int64(-1)
	for _, a := range c.all() {
		if p := findTranscript(a.dir(), sid); p != "" {
			if fi, err := os.Stat(p); err == nil && fi.ModTime().UnixNano() > bestT {
				best, bestPath, bestT = a, p, fi.ModTime().UnixNano()
			}
		}
	}
	return best, bestPath, bestT >= 0
}

func findTranscript(dir, sid string) string {
	if !validSID(sid) {
		return ""
	}
	hits, _ := filepath.Glob(filepath.Join(dir, "projects", "*", sid+".jsonl"))
	if len(hits) == 0 {
		return ""
	}
	return hits[0]
}
