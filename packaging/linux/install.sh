#!/bin/sh
# Installs fas-cli with its desktop entry and icons from this directory.
#
#   sh install.sh              install into ~/.local (no root needed)
#   PREFIX=/usr/local sh install.sh    system-wide (run as root)
#   sh install.sh uninstall    remove what was installed
#
# A Linux executable cannot carry an icon inside itself; desktop environments take it from
# the desktop entry and the freedesktop "hicolor" icon theme, which is what this installs.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
prefix=${PREFIX:-$HOME/.local}
bin=$prefix/bin
apps=$prefix/share/applications
icons=$prefix/share/icons/hicolor

refresh() {
    command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q -t -f "$icons" 2>/dev/null || true
    command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database -q "$apps" 2>/dev/null || true
}

if [ "${1:-}" = "uninstall" ]; then
    rm -f "$bin/fas-cli" "$apps/fas-cli.desktop"
    for png in "$icons"/*/apps/fas-cli.png; do
        [ -e "$png" ] && rm -f "$png"
    done
    refresh
    echo "fas-cli removed from $prefix"
    exit 0
fi

[ -f "$here/fas-cli" ] || { echo "fas-cli binary not found next to install.sh" >&2; exit 1; }

mkdir -p "$bin" "$apps"
install -m 755 "$here/fas-cli" "$bin/fas-cli"
# the launcher must start the installed binary by its full path (quoted when it has spaces)
case "$bin" in
    *" "*) exec_line="Exec=\"$bin/fas-cli\"" ;;
    *) exec_line="Exec=$bin/fas-cli" ;;
esac
while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
        Exec=*) printf '%s\n' "$exec_line" ;;
        *) printf '%s\n' "$line" ;;
    esac
done < "$here/fas-cli.desktop" > "$apps/fas-cli.desktop"
chmod 644 "$apps/fas-cli.desktop"

for dir in "$here"/icons/hicolor/*/apps; do
    [ -d "$dir" ] || continue
    size=$(basename "$(dirname "$dir")")
    mkdir -p "$icons/$size/apps"
    install -m 644 "$dir/fas-cli.png" "$icons/$size/apps/fas-cli.png"
done
refresh

echo "fas-cli installed to $bin"
case ":$PATH:" in
    *":$bin:"*) ;;
    *) echo "note: $bin is not in PATH" ;;
esac
