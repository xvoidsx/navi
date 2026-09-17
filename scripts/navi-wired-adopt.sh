#!/usr/bin/env bash
#
# navi-wired-adopt — take the new stock wired configs wholesale.
#
# navi-update preserves your customized configs by design — so when a
# release ships new wired defaults (a reworked waybar, new sway rules,
# restyled modules), your machine keeps the old ones until you say
# otherwise. this is the otherwise: it lays the current stock wired
# configs over the top of yours.
#
# LOUD WARNING, up front: every file listed below gets REPLACED. your
# customizations in those files go away (a backup is taken first, so
# nothing is ever truly lost — but adopt means adopt).
#
# Runs as your normal user — no elevation needed for $HOME configs.
#
#   navi-wired-adopt [--yes] [--dry-run] [component]
#
#   navi-wired-adopt            list what would change, warn, ask, adopt
#   navi-wired-adopt sway       only the sway component
#   navi-wired-adopt --dry-run  list what would change, touch nothing
#   navi-wired-adopt --yes      skip the confirmation (still backs up)

set -euo pipefail

SHARE_DIR="/usr/share/navi"
WIRED_SHARE="$SHARE_DIR/wired"
MANIFEST_FILE="/var/lib/navi/deploy-manifest.tsv"
BACKUP_BASE="$HOME/.local/share/navi/backups"

MODE="ask"   # ask | dry-run | yes
COMPONENT=""

say()  { echo "──▶ $1"; }
ok()   { echo "    ✓ $1"; }
info() { echo "    · $1"; }
warn() { echo "    ! $1"; }
die()  { echo "    ✗ $1" >&2; exit 1; }

usage() { sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'; exit 0; }

# ---------------------------------------------------------------- manifest

declare -a A_REL=() A_DEST=() A_MODE=() A_VER=()

load_manifest() {
  [ -s "$MANIFEST_FILE" ] \
    || die "no deploy manifest at $MANIFEST_FILE — run navi-update once to establish one."
  [ -d "$WIRED_SHARE" ] \
    || die "/usr/share/navi/wired is missing or damaged — run navi-update to re-deploy, then try again."
  local sha ver rel dest mode
  while IFS=$'\t' read -r sha ver rel dest mode; do
    [ -n "${dest:-}" ] || continue
    # adopt is about YOUR configs — system files are navi-update's job
    # and need elevation. only $HOME destinations here.
    case "$dest" in
      "$HOME"/*) ;;
      *) continue ;;
    esac
    if [ -n "$COMPONENT" ] && [[ "$rel" != "$COMPONENT"* ]]; then
      continue
    fi
    A_REL+=("$rel"); A_DEST+=("$dest")
    A_MODE+=("${mode:-644}"); A_VER+=("$ver")
  done < "$MANIFEST_FILE"
  [ "${#A_DEST[@]}" -gt 0 ] || die "nothing adoptable under \$HOME in the manifest."
}

# ---------------------------------------------------------------- what changes

declare -a C_IDX=()
declare -a C_KIND=()   # changed | new-file

find_changes() {
  local i src dest
  for i in "${!A_DEST[@]}"; do
    src="$WIRED_SHARE/${A_REL[$i]}"
    dest="${A_DEST[$i]}"
    if [ ! -e "$src" ]; then
      warn "stock default missing for ${dest#$HOME/} (rel: ${A_REL[$i]}) — skipping"
      continue
    fi
    if [ ! -e "$dest" ]; then
      C_IDX+=("$i"); C_KIND+=("new-file")
    elif ! cmp -s "$src" "$dest"; then
      C_IDX+=("$i"); C_KIND+=("changed")
    fi
  done
}

show_changes() {
  local n i kind
  n="${#C_IDX[@]}"
  if [ "$n" -eq 0 ]; then
    ok "your configs already match the stock wired set — nothing to adopt"
    return 1
  fi
  say "$n file(s) would be replaced with the stock wired versions"
  for n in "${!C_IDX[@]}"; do
    i="${C_IDX[$n]}"; kind="${C_KIND[$n]}"
    printf '    %-9s %-42s (stock: navi %s)\n' "[$kind]" \
      "${A_DEST[$i]#$HOME/}" "${A_VER[$i]}"
  done
  return 0
}

warn_loudly() {
  echo
  echo "    ═══════════════════════════════════════════════════════════"
  echo "    YOU ARE ABOUT TO OVERWRITE YOUR WIRED CONFIGS."
  echo "    every file listed above gets REPLACED by navi's stock"
  echo "    version. your customizations in those files go away —"
  echo "    a backup is taken first, so you can always go back,"
  echo "    but adopt means adopt."
  echo "    ═══════════════════════════════════════════════════════════"
  echo
}

confirm() {
  [ "$MODE" = "yes" ] && { info "confirmation skipped (--yes)"; return 0; }
  local ans
  read -r -p "    type YES to replace these configs with the stock set: " ans
  [ "$ans" = "YES" ] || die "aborted — nothing changed."
}

# ---------------------------------------------------------------- adopt

do_adopt() {
  local ts backup_dir n i src dest
  ts="$(date +%Y%m%d-%H%M%S)"
  backup_dir="$BACKUP_BASE/wired-adopt-$ts"
  mkdir -p "$backup_dir"

  for n in "${!C_IDX[@]}"; do
    i="${C_IDX[$n]}"
    src="$WIRED_SHARE/${A_REL[$i]}"; dest="${A_DEST[$i]}"
    if [ -e "$dest" ]; then
      mkdir -p "$backup_dir/$(dirname "${A_REL[$i]}")"
      cp -a "$dest" "$backup_dir/${A_REL[$i]}"
    fi
    mkdir -p "$(dirname "$dest")"
    install -m "${A_MODE[$i]}" "$src" "$dest"
    ok "adopted ${dest#$HOME/}"
  done
  echo
  ok "your previous configs are backed up at $backup_dir"
}

maybe_reload_wm() {
  local reloader=""
  if [ -n "${WAYLAND_DISPLAY:-}" ] && command -v swaymsg >/dev/null 2>&1; then
    reloader="swaymsg reload"
  elif command -v i3-msg >/dev/null 2>&1 && [ "${XDG_SESSION_TYPE:-}" = "x11" ]; then
    reloader="i3-msg restart"
  fi
  # the bar reads its config at startup — restart it so the new
  # modules and styles take effect immediately.
  restart_bar() {
    if pgrep -x waybar >/dev/null 2>&1; then
      pkill -x waybar
      (waybar >/dev/null 2>&1 &)
      ok "waybar restarted"
    elif pgrep -x polybar >/dev/null 2>&1; then
      pkill -x polybar
      info "polybar stopped — it restarts with the i3 session"
    fi
  }
  [ -n "$reloader" ] || { info "no live sway/i3 session — your next login picks up the adopted configs"; return 0; }
  if [ "$MODE" = "yes" ]; then
    eval "$reloader" >/dev/null 2>&1 && ok "window manager reloaded" \
      || warn "reload failed — do it by hand: $reloader"
    restart_bar
    return 0
  fi
  local ans
  read -r -p "    reload the window manager + bar now? [Y/n] " ans
  case "${ans:-Y}" in
    [Yy]*) eval "$reloader" >/dev/null 2>&1 \
             && ok "window manager reloaded" \
             || warn "reload failed — do it by hand: $reloader"
           restart_bar ;;
    *) info "not reloaded — your next login picks up the adopted configs" ;;
  esac
}

pause_if_tty() {
  if [ -t 0 ]; then read -r -n1 -s -p "    press any key to close…" _; echo; fi
}

# ---------------------------------------------------------------- main

while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) MODE="dry-run" ;;
    --yes)     MODE="yes" ;;
    --help|-h) usage ;;
    -*) die "unknown option: $1 (try --help)" ;;
    *)
      [ -z "$COMPONENT" ] || die "only one component at a time"
      COMPONENT="$1" ;;
  esac
  shift
done

load_manifest
find_changes
show_changes || exit 0

if [ "$MODE" = "dry-run" ]; then
  info "dry run — nothing changed."
  exit 0
fi

warn_loudly
confirm
do_adopt
maybe_reload_wm
pause_if_tty
