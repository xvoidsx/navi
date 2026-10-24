#!/usr/bin/env bash
# thinkrail installer
# ( ThinkRail is JetBrains' open-source agentic IDE — a thin host around
#   the pi coding agent, with git-worktree workspaces and a spec graph )
# ( https://thinkrail.ai — https://github.com/JetBrains/thinkrail )
#
# Installs the thinkrail CLI via upstream's installer (non-interactive:
# downloads the binary, verifies its SHA-256, lands at ~/.local/bin),
# then wires it into the desktop the navi way: a chromeless webapp
# launcher, same as navi's other webapps. No desktop (Electrobun) build,
# no new system packages — the browser UI on localhost:24243 is the app.
#
# First launch still needs an authenticated pi provider: sign in with
# JetBrains AI from inside the app, or use pi's own provider auth.
# Update later with: thinkrail update
# Remove later with: thinkrail uninstall, then delete the two launcher
# files this script writes (thinkrail-webapp + thinkrail.desktop).
set -euo pipefail
x="= = = = = thinkrail installer = = = = ="

BIN_DIR="$HOME/.local/bin"
WRAPPER="$BIN_DIR/thinkrail-webapp"
DESKTOP_DIR="$HOME/.local/share/applications"
DESKTOP_FILE="$DESKTOP_DIR/thinkrail.desktop"

find_thinkrail() {
    if command -v thinkrail >/dev/null 2>&1; then
        command -v thinkrail
    elif [ -x "$BIN_DIR/thinkrail" ]; then
        echo "$BIN_DIR/thinkrail"
    fi
}

main() {
    echo "$x"; sleep 1
    if [ -n "$(find_thinkrail)" ]; then
        echo "  thinkrail already installed at $(find_thinkrail) — refreshing launcher only."
    else
        tmp="$(mktemp)"
        trap 'rm -f "$tmp"' EXIT
        echo "  downloading ThinkRail installer from github.com/JetBrains/thinkrail..."
        curl -fsSL --connect-timeout 20 --max-time 600 \
            --retry 3 --retry-all-errors \
            -o "$tmp" https://raw.githubusercontent.com/JetBrains/thinkrail/main/install.sh
        echo "  running ThinkRail installer (stable channel, user-local prefix)..."
        bash "$tmp"
        [ -n "$(find_thinkrail)" ] \
            || { echo "  thinkrail did not land on PATH — install may have failed" >&2; exit 1; }
        echo "  thinkrail installed at $(find_thinkrail)."
    fi

    echo "  writing webapp launcher..."
    mkdir -p "$BIN_DIR" "$DESKTOP_DIR"
    cat > "$WRAPPER" <<'EOF'
#!/usr/bin/env bash
# thinkrail-webapp — open ThinkRail as a chromeless webapp window.
# Starts the local server on :24243 if it isn't answering yet, then
# opens the app window through navi's browser runner (falls back to
# brave nightly, then chromium). Same ~/.thinkrail state as the CLI
# and the desktop build — the window is just a view onto the server.
set -u
BIN="$(command -v thinkrail 2>/dev/null || true)"
[ -n "$BIN" ] || BIN="$HOME/.local/bin/thinkrail"
if ! curl -fsS --max-time 2 http://localhost:24243 >/dev/null 2>&1; then
    if [ -x "$BIN" ]; then
        # NOTE: thinkrail itself may open a browser window on first
        # start — close it; the server stays up and this window is
        # the one you keep.
        setsid nohup "$BIN" >/dev/null 2>&1 < /dev/null &
    fi
    for _ in $(seq 1 40); do
        curl -fsS --max-time 2 http://localhost:24243 >/dev/null 2>&1 && break
        sleep 0.5
    done
fi
if command -v navi-browser-run >/dev/null 2>&1; then
    exec navi-browser-run --force-dark-mode --app=http://localhost:24243
elif command -v brave-browser-nightly >/dev/null 2>&1; then
    exec brave-browser-nightly --force-dark-mode --app=http://localhost:24243
elif command -v chromium >/dev/null 2>&1; then
    exec chromium --force-dark-mode --app=http://localhost:24243
else
    echo "thinkrail-webapp: no Chromium-family browser found" >&2
    exit 1
fi
EOF
    chmod 0755 "$WRAPPER"

    cat > "$DESKTOP_FILE" <<EOF
[Desktop Entry]
Type=Application
Name=ThinkRail
Comment=JetBrains' open-source agentic IDE (pi engine) — local webapp window
Exec=$WRAPPER
Icon=thinkrail
Terminal=false
Categories=Development;
EOF
    if command -v update-desktop-database >/dev/null 2>&1; then
        update-desktop-database "$DESKTOP_DIR" >/dev/null 2>&1 || true
    fi
    echo "  launcher installed: $DESKTOP_FILE"
    echo "  run it from your app launcher, or: $WRAPPER"
    echo "  first launch: sign in with JetBrains AI, or your pi provider, inside the app."
}
# - - - - - |
main
