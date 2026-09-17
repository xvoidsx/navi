#!/usr/bin/env bash
#
# navi-wired-restore — the factory reset for the wired desktop.
#
# Deliberate, transparent, forgiving: shows what drifted from navi's
# defaults, always backs up first, and only touches what you ask it to.
# Reads the deploy manifest (/var/lib/navi/deploy-manifest.tsv) and the
# pristine defaults in /usr/share/navi/wired.
#
# Runs as your normal user — no elevation needed for $HOME configs.
#
#   navi-wired-restore [component] [--diff] [--dry-run] [--all|--yes]
#
#   navi-wired-restore            interactive: show drift, confirm, restore
#   navi-wired-restore sway       only the sway component ("I broke my keybindings")
#   navi-wired-restore --diff     show diffs, change nothing
#   navi-wired-restore --dry-run  list what would be restored
#   navi-wired-restore --all      non-interactive full restore (still backs up)

set -euo pipefail

SHARE_DIR="/usr/share/navi"
WIRED_SHARE="$SHARE_DIR/wired"
MANIFEST_FILE="/var/lib/navi/deploy-manifest.tsv"
BACKUP_BASE="$HOME/.local/share/navi/backups"

MODE="interactive"   # interactive | diff | dry-run | all
COMPONENT=""

say()  { echo "──▶ $1"; }
ok()   { echo "    ✓ $1"; }
info() { echo "    · $1"; }
warn() { echo "    ! $1"; }
die()  { echo "    ✗ $1" >&2; exit 1; }

usage() { sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; exit 0; }

# ---------------------------------------------------------------- manifest

declare -a M_SHA=() M_VER=() M_REL=() M_DEST=() M_MODE=()

load_manifest() {
  [ -s "$MANIFEST_FILE" ] \
    || die "no deploy manifest at $MANIFEST_FILE — this machine was installed before manifests existed. run navi-update once to establish one."
  local sha ver rel dest mode
  while IFS=$'\t' read -r sha ver rel dest mode; do
    [ -n "${dest:-}" ] || continue
    M_SHA+=("$sha"); M_VER+=("$ver"); M_REL+=("$rel")
    M_DEST+=("$dest"); M_MODE+=("${mode:-644}")
  done < "$MANIFEST_FILE"
  [ "${#M_DEST[@]}" -gt 0 ] || die "deploy manifest is empty — run navi-update to rebuild it."
}

# ---------------------------------------------------------------- drift detection

# DRIFT_IDX holds manifest indexes whose live file differs from the default.
declare -a DRIFT_IDX=()
declare -a DRIFT_KIND=()   # modified | deleted

detect_drift() {
  [ -d "$WIRED_SHARE" ] \
    || die "/usr/share/navi/wired is missing or damaged — restore can't help with that. run navi-update to re-deploy the desktop layer, then try again."
  local i src dest
  for i in "${!M_DEST[@]}"; do
    if [ -n "$COMPONENT" ] && [[ "${M_REL[$i]}" != "$COMPONENT"* ]]; then
      continue
    fi
    src="$WIRED_SHARE/${M_REL[$i]}"
    dest="${M_DEST[$i]}"
    if [ ! -e "$src" ]; then
      warn "default missing for ${dest#$HOME/} (rel: ${M_REL[$i]}) — skipping"
      continue
    fi
    if [ ! -e "$dest" ]; then
      DRIFT_IDX+=("$i"); DRIFT_KIND+=("deleted")
    elif ! cmp -s "$src" "$dest"; then
      DRIFT_IDX+=("$i"); DRIFT_KIND+=("modified")
    fi
  done
}

show_drift() {
  local n i kind
  n="${#DRIFT_IDX[@]}"
  if [ "$n" -eq 0 ]; then
    ok "everything matches navi's defaults — nothing to restore"
    return 1
  fi
  say "$n file(s) drifted from defaults"
  for n in "${!DRIFT_IDX[@]}"; do
    i="${DRIFT_IDX[$n]}"; kind="${DRIFT_KIND[$n]}"
    printf '    %-9s %-42s (default: navi %s)\n' "[$kind]" \
      "${M_DEST[$i]#$HOME/}" "${M_VER[$i]}"
  done
  return 0
}

show_diffs() {
  local n i src dest
  for n in "${!DRIFT_IDX[@]}"; do
    i="${DRIFT_IDX[$n]}"
    src="$WIRED_SHARE/${M_REL[$i]}"; dest="${M_DEST[$i]}"
    echo "─── ${dest#$HOME/} (${DRIFT_KIND[$n]}, default: navi ${M_VER[$i]})"
    if [ -e "$dest" ]; then
      diff -u "$src" "$dest" || true
    else
      echo "    (deleted — the default would be re-created)"
    fi
    echo
  done
}

# ---------------------------------------------------------------- restore

do_restore() {
  local ts backup_dir n i src dest
  ts="$(date +%Y%m%d-%H%M%S)"
  backup_dir="$BACKUP_BASE/wired-restore-$ts"
  mkdir -p "$backup_dir"

  for n in "${!DRIFT_IDX[@]}"; do
    i="${DRIFT_IDX[$n]}"
    src="$WIRED_SHARE/${M_REL[$i]}"; dest="${M_DEST[$i]}"
    # back up current state first — always, even for deleted files' parents
    if [ -e "$dest" ]; then
      mkdir -p "$backup_dir/$(dirname "${M_REL[$i]}")"
      cp -a "$dest" "$backup_dir/${M_REL[$i]}"
    fi
    mkdir -p "$(dirname "$dest")"
    install -m "${M_MODE[$i]}" "$src" "$dest"
    ok "restored ${dest#$HOME/}"
  done
  echo
  ok "previous state backed up to $backup_dir"
}

maybe_reload_wm() {
  # offer (or perform, in --all mode) a WM reload so the fix is immediate
  local reloader=""
  if [ -n "${WAYLAND_DISPLAY:-}" ] && command -v swaymsg >/dev/null 2>&1; then
    reloader="swaymsg reload"
  elif command -v i3-msg >/dev/null 2>&1 && [ "${XDG_SESSION_TYPE:-}" = "x11" ]; then
    reloader="i3-msg restart"
  fi
  [ -n "$reloader" ] || { info "no live sway/i3 session detected — skipping reload"; return 0; }
  if [ "$MODE" = "all" ]; then
    info "reloading the window manager…"
    eval "$reloader" >/dev/null 2>&1 || warn "reload failed — do it by hand: $reloader"
  else
    local ans
    read -r -p "    reload the window manager now? [Y/n] " ans
    case "${ans:-Y}" in
      [Yy]*) eval "$reloader" >/dev/null 2>&1 \
        && ok "window manager reloaded" \
        || warn "reload failed — do it by hand: $reloader" ;;
      *) info "not reloaded — your next login picks up the restored configs" ;;
    esac
  fi
}

pause_if_tty() {
  if [ -t 0 ]; then read -r -n1 -s -p "    press any key to close…" _; echo; fi
}

# ---------------------------------------------------------------- main

main() {
  while [ $# -gt 0 ]; do
    case "$1" in
      --diff)    MODE="diff" ;;
      --dry-run) MODE="dry-run" ;;
      --all|--yes) MODE="all" ;;
      --help|-h) usage ;;
      -*) die "unknown option: $1 (try --help)" ;;
      *)
        [ -z "$COMPONENT" ] || die "only one component at a time"
        COMPONENT="$1" ;;
    esac
    shift
  done

  load_manifest
  detect_drift

  case "$MODE" in
    diff)
      show_drift || exit 0
      echo; show_diffs ;;
    dry-run)
      if show_drift; then
        echo; info "dry run — nothing was changed"
      fi ;;
    all)
      show_drift || exit 0
      echo; do_restore; echo; maybe_reload_wm ;;
    interactive)
      show_drift || exit 0
      echo
      local ans
      read -r -p "    restore these ${#DRIFT_IDX[@]} file(s) to navi's defaults? [y/N] " ans
      case "${ans:-N}" in
        [Yy]*) echo; do_restore; echo; maybe_reload_wm ;;
        *) info "nothing changed — your drift is safe with you" ;;
      esac ;;
  esac
  pause_if_tty
}

main "$@"
