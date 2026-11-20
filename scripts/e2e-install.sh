#!/bin/zsh
# 새 Mac 처럼: 빈 HOME, macOS 기본 PATH 에서 scripts/install.sh 로 baton 을 설치하고,
# 다시 설치(변경 없음) → 제거까지. 릴리스 대신 방금 빌드한 tar.gz 를 BATON_URL(file://)로 넘긴다.
#   env -i HOME=… PATH=/usr/bin:/bin:/usr/sbin:/sbin REPO=<clone> BATON_TGZ=<tar.gz> /bin/zsh scripts/e2e-install.sh
set -euo pipefail
fail() { print -u2 "✗ $*"; exit 1; }

[[ -f "$BATON_TGZ" && -f "$BATON_TGZ.sha256" ]] || fail "BATON_TGZ (and .sha256) missing"
export HOME="$(mktemp -d)"
mkdir -p "$HOME/.claude"
print '{"model": "opus"}' > "$HOME/.claude/settings.json"

BATON_URL="file://$BATON_TGZ" /bin/sh "$REPO/scripts/install.sh" | tee "$HOME/install.log"
b="$HOME/.local/bin/baton"
[[ -x "$b" && -L "$HOME/.local/bin/baton-claude" ]] || fail "binary or baton-claude link missing"
"$b" --version | grep -q '^baton ' || fail "--version"
s="$HOME/.claude/settings.json"
grep -q '"model": "opus"' "$s" || fail "existing settings lost"
grep -q 'baton\\" hook prompt' "$s" || fail "hook not installed"
grep -q 'baton\\" statusline' "$s" || fail "statusline not installed"
[[ -f "$HOME/.claude/commands/swap.md" ]] || fail "/swap command missing"
before="$(cat "$s")"

"$b" install | grep -q 'settings.json: no change' || fail "second install changed settings"
[[ "$(cat "$s")" == "$before" ]] || fail "second install rewrote settings.json"
(( $(ls "$s".bak-baton-* | wc -l) == 1 )) || fail "want exactly one backup"

print '{"rate_limits":{"seven_day":{"used_percentage":3,"resets_at":4102444800}}}' | "$b" statusline | grep -q '3%' || fail "HUD"

"$b" uninstall >/dev/null
grep -q baton "$s" && fail "uninstall left baton in settings.json"
[[ -f "$HOME/.claude/commands/swap.md" ]] && fail "uninstall left /swap"
grep -q '"model": "opus"' "$s" || fail "uninstall lost settings"

# 체크섬이 틀리면 아무것도 설치하지 않는다.
bad="$(mktemp -d)"; cp "$BATON_TGZ" "$bad/x.tar.gz"; print "0000  x.tar.gz" > "$bad/x.tar.gz.sha256"
BATON_INSTALL_DIR="$HOME/bad-bin" BATON_URL="file://$bad/x.tar.gz" /bin/sh "$REPO/scripts/install.sh" 2>/dev/null && fail "bad checksum accepted"
[[ -e "$HOME/bad-bin/baton" ]] && fail "installed despite bad checksum"
print "✓ go install e2e passed"
