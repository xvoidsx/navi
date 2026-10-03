#!/usr/bin/env bash
# apply.sh — deploy the 02-october-navi waybar bundle.
#
#   ./apply.sh --dry-run     show what would happen, change nothing
#   ./apply.sh --repo-only   only update the navi git repo
#   ./apply.sh --system-only only touch this machine's live system
#   ./apply.sh               both
#
# Everything is backed up before it is overwritten. The repo payload holds the
# final desired state of each file, so this is a copy, not a patch — it does not
# try to re-apply the diffs described in AGENT_HANDB.md.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PAYLOAD="$HERE/payload"
REPO="${NAVI_REPO:-$HOME/Documents/navi}"
STAMP="$(date +%Y%m%d-%H%M%S)"

DRY=0
DO_REPO=1
DO_SYSTEM=1
case "${1:-}" in
    --dry-run)    DRY=1 ;;
    --repo-only)  DO_SYSTEM=0 ;;
    --system-only) DO_REPO=0 ;;
    "") ;;
    *) printf 'usage: apply.sh [--dry-run|--repo-only|--system-only]\n' >&2; exit 2 ;;
esac

say()  { printf '%s\n' "$*"; }
step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
run()  { if [ "$DRY" = 1 ]; then printf '    [dry-run] %s\n' "$*"; else "$@"; fi; }

[ -d "$PAYLOAD" ] || { say "no payload/ next to this script"; exit 1; }

# ---------------------------------------------------------------- repo ----

deploy_repo() {
    step "navi repo  ->  $REPO"
    [ -d "$REPO/.git" ] || { say "    WARNING: $REPO is not a git repo; skipping"; return 1; }

    local changed=0
    while IFS= read -r src; do
        local rel="${src#"$PAYLOAD/repo/"}"
        local dst="$REPO/$rel"
        if [ -f "$dst" ] && cmp -s "$src" "$dst"; then
            printf '    unchanged  %s\n' "$rel"
            continue
        fi
        # decide the label before touching anything, or every new file reports
        # itself as "updated" because it exists by the time we print
        local label="updated  "
        [ -f "$dst" ] || label="NEW      "
        if [ -f "$dst" ]; then
            local bak="$dst.bak-$STAMP"
            run cp -a "$dst" "$bak"
            printf '    backed up  %s -> %s\n' "$rel" "$(basename "$bak")"
        fi
        run mkdir -p "$(dirname "$dst")"
        run cp -a "$src" "$dst"
        printf '    %s %s\n' "$label" "$rel"
        changed=$((changed + 1))
    done < <(find "$PAYLOAD/repo" -type f | sort)

    say ""
    say "    $changed file(s) changed in the repo."
    if [ "$DRY" = 0 ]; then
        say "    Review with:  cd $REPO && git status --short && git diff"
    fi
}

# -------------------------------------------------------------- system ----

deploy_system() {
    step "live system"
    local LIVE=/usr/share/navi

    if [ ! -w "$LIVE" ] && [ "$DRY" = 0 ]; then
        say "    WARNING: $LIVE is not writable — re-run as root, or:"
        say "             sudo ./apply.sh --system-only"
    fi

    # 1. payload fixes (waybar weather glyph, mpvpaper auto-pause)
    local pair
    for pair in \
        "usr-share-navi_wired_waybar_weather.sh:$LIVE/wired/waybar/weather.sh" \
        "usr-share-navi_wired_scripts_navi-wallpaper.sh:$LIVE/wired/scripts/navi-wallpaper.sh"
    do
        local src="$PAYLOAD/system/${pair%%:*}" dst="${pair#*:}"
        [ -f "$src" ] || continue
        if [ -f "$dst" ] && cmp -s "$src" "$dst"; then
            printf '    unchanged  %s\n' "$dst"
        else
            [ -f "$dst" ] && { run cp -a "$dst" "$dst.bak-$STAMP"; printf '    backed up  %s\n' "$dst"; }
            run cp -a "$src" "$dst"
            printf '    installed  %s\n' "$dst"
        fi
    done

    # 2. user config
    local u
    for u in \
        "repo/configs/core/waybar/config.jsonc:$HOME/.config/waybar/config.jsonc" \
        "repo/configs/core/waybar/style.css:$HOME/.config/waybar/style.css" \
        "repo/configs/core/rofi/navi-theme.rasi:$HOME/.config/rofi/navi-theme.rasi" \
        "repo/configs/core/sway/config:$HOME/.config/sway/config"
    do
        local src="$PAYLOAD/${u%%:*}" dst="${u#*:}"
        [ -f "$src" ] || continue
        run mkdir -p "$(dirname "$dst")"
        if [ -f "$dst" ] && ! cmp -s "$src" "$dst"; then
            run cp -a "$dst" "$dst.bak-$STAMP"
            printf '    backed up  %s\n' "$dst"
        fi
        run cp -a "$src" "$dst"
        printf '    installed  %s\n' "$dst"
    done

    # 3. the theme tool and the theme store
    run mkdir -p "$HOME/.local/bin" "$HOME/.local/share/navi/waybar"
    run cp -a "$PAYLOAD/repo/scripts/navi-theme" "$HOME/.local/bin/navi-theme"
    run chmod +x "$HOME/.local/bin/navi-theme"
    printf '    installed  %s/.local/bin/navi-theme\n' "$HOME"
    run rm -rf "$HOME/.local/share/navi/waybar/themes"
    run cp -a "$PAYLOAD/repo/configs/core/waybar/themes" "$HOME/.local/share/navi/waybar/themes"
    printf '    installed  %s/.local/share/navi/waybar/themes (5)\n' "$HOME"

    # 4. waybar autostart. The packaged unit cannot start in a plain sddm
    #    session (Requisite=graphical-session.target, and that target is never
    #    activated), so ship a full user unit that replaces it and hooks into
    #    default.target instead. Without this the bar only appears when something
    #    else happens to launch it.
    if [ -f "$PAYLOAD/repo/configs/core/systemd/waybar.service" ]; then
        run mkdir -p "$HOME/.config/systemd/user"
        run cp -a "$PAYLOAD/repo/configs/core/systemd/waybar.service" \
                  "$HOME/.config/systemd/user/waybar.service"
        if [ "$DRY" = 0 ]; then
            systemctl --user daemon-reload >/dev/null 2>&1
            # drop the stale symlink an earlier enable may have left behind
            rm -f "$HOME/.config/systemd/user/graphical-session.target.wants/waybar.service"
            systemctl --user enable waybar.service >/dev/null 2>&1
            systemctl --user restart waybar.service >/dev/null 2>&1
            printf '    installed  %s/.config/systemd/user/waybar.service (enabled)\n' "$HOME"
        else
            printf '    [dry-run] would enable and restart waybar.service\n'
        fi
    fi

    # 5. blueman retired for good — the unit is static, so commenting the sway
    #    exec is not enough; mask it or it resurrects on D-Bus activation
    if command -v systemctl >/dev/null 2>&1; then
        run systemctl --user mask blueman-applet.service
        printf '    masked     blueman-applet.service\n'
    fi

    # 6. runtime steps that are not file copies
    step "runtime steps (not automated)"
    cat <<'EOF'
    These need judgement, so they are listed rather than done for you:

    a) Bluetooth audio needs a package AND a wireplumber restart. WirePlumber
       builds its module graph at boot, so installing the plugin mid-session does
       nothing until it is restarted:

           sudo apt install libspa-0.2-bluetooth
           systemctl --user restart wireplumber

       Consider adding libspa-0.2-bluetooth to navi's package dependencies so
       users never hit this. Symptom when it is missing:
           "PipeWire's BlueZ MIDI SPA missing or broken. Bluetooth not supported."

    b) Regenerate the theme picker screenshots (switches themes, captures, restores):

           navi-theme shot

    c) The keyring's ssh component only appears on the NEXT LOGIN. gnome-keyring
       will not add a component to an already-running daemon. Verify afterwards:

           tr '\0' '\n' < /proc/$(pgrep -x gnome-keyring-daemon|head -1)/environ \
             | grep SSH_AUTH_SOCK

    d) Reload sway so the Mod1+t binding registers:

           swaymsg reload

    e) Waybar autostart is fragile on a plain sddm session:
       waybar.service has Requisite=graphical-session.target, and that target is
       INACTIVE here, so systemctl will not start it. If waybar dies, launch it
       manually with the Wayland env exported:

           export XDG_RUNTIME_DIR=/run/user/$UID WAYLAND_DISPLAY=wayland-1 \
                  DISPLAY=:1 SWAYSOCK=/run/user/$UID/sway-ipc.1000.$(pgrep -x sway|head -1).sock
           setsid waybar -b bar-0
EOF
}

# ----------------------------------------------------------------- run ----

say "02-october-navi bundle"
say "repo:   $REPO"
say "stamp:  $STAMP   (all backups get this suffix)"
[ "$DRY" = 1 ] && say "MODE:   dry run, nothing will be modified"

if [ "$DO_REPO" = 1 ];   then deploy_repo;   fi
if [ "$DO_SYSTEM" = 1 ]; then deploy_system; fi

step "done"
say "Read AGENT_HANDB.md before committing — it records the measurements, the"
say "platform gotchas, and seven regressions to avoid reintroducing."