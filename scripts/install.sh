#!/bin/sh
# baton 설치: 릴리스 바이너리를 받아 체크섬을 확인하고 ~/.local/bin 에 넣은 뒤 `baton install` 을 돌린다.
#   curl -fsSL https://raw.githubusercontent.com/juunghyun/orca-baton/main/scripts/install.sh | sh
# BATON_VERSION=v0.1.0 으로 버전 고정, BATON_URL 로 받을 tar.gz 를 직접 지정(CI 의 새 맥 설치 시험).
set -eu

REPO=juunghyun/orca-baton
case "$(uname -s)/$(uname -m)" in
  Darwin/arm64) arch=arm64 ;;
  Darwin/x86_64) arch=amd64 ;;
  *) echo "baton supports macOS (Apple silicon, Intel) only." >&2; exit 1 ;;
esac
asset="baton-darwin-$arch.tar.gz"
if [ -n "${BATON_URL:-}" ]; then
  url="$BATON_URL"
elif [ -n "${BATON_VERSION:-}" ]; then
  url="https://github.com/$REPO/releases/download/$BATON_VERSION/$asset"
else
  url="https://github.com/$REPO/releases/latest/download/$asset"
fi
dir="${BATON_INSTALL_DIR:-$HOME/.local/bin}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$url" -o "$tmp/$asset"
curl -fsSL "$url.sha256" -o "$tmp/$asset.sha256"
want="$(cut -d' ' -f1 "$tmp/$asset.sha256")"
got="$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)"
if [ -z "$want" ] || [ "$want" != "$got" ]; then
  echo "Checksum mismatch for $asset; nothing was installed." >&2
  exit 1
fi
tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$dir"
install -m 0755 "$tmp/baton" "$dir/baton"
ln -sf baton "$dir/baton-claude"   # Orca 실행 명령 칸이 공백 있는 명령을 못 받을 때

"$dir/baton" install
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Add $dir to your PATH (e.g. in ~/.zshrc: export PATH=\"$dir:\$PATH\")." ;;
esac
echo "Next: baton add <name> for each extra account, then baton rule add <folder> <name>."
