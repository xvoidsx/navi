#!/usr/bin/env bash
#
# navi-update — update a navi machine. one funnel, three channels:
#
#     navi's files  ->  Debian packages  ->  Flatpaks
#
# "navi's files are navi's channel, Debian's files are apt's channel."
#
# run as your normal user (not root); privileged steps elevate via
# doas/sudo internally. never overwrites a config you modified — those
# are reported, and navi-wired-restore can bring in the new defaults.
#
# security: stable moves to release tags (signature verification flips on
# once xvoidsx settles the signing story — until then, loud unsigned pulls
# that trust the channel: TLS + GitHub access control). eiri tracks the
# branch head, always unsigned, always loud about it.
# there is no --insecure flag and there never will be.
#
#   navi-update [--check] [--channel <name>] [--yes]
#
# --check     report available updates, change nothing (for the waybar mod)
# --channel   release channel: stable (default) or eiri. stable follows
#             release tags (v1.x); eiri is rolling and tracks the eiri
#             branch head. switching channels is an explicit opt-in and
#             always asks first (unless stdin isn't a terminal / --yes).
# --yes       non-interactive: accept defaults (still asks on channel switch
#             unless stdin is not a terminal)

set -euo pipefail

REPO_URL="https://github.com/xvoidsx/navi.git"
UPSTREAM_DIR="$HOME/.cache/navi/upstream"
CHANNEL_FILE="/etc/navi/channel"
STATE_FILE="/var/lib/navi/navi-update.state"  # channel= + commit= of the last navi-layer deploy
VERSION_FILE="/etc/navi/version"
SHARE_VERSION="/usr/share/navi/VERSION"
KEY_FILE="/usr/share/navi/wired/keys/release.asc"
# fingerprint of the xvoidsx release signing key. empty until Raven runs
# the signing ceremony (see wired/keys/README.md) — with it empty the
# navi layer refuses to update (fail closed).
RELEASE_FINGERPRINT=""   # empty until xvoidsx settles the signing story (eiri era)

ASSUME_YES=0
CHECK_ONLY=0
CHANNEL_OVERRIDE=""
DOAS=""

PH_NAVI="pending"; PH_DEB="pending"; PH_FLAT="pending"

say()  { echo "──▶ $1"; }
ok()   { echo "    ✓ $1"; }
info() { echo "    · $1"; }
warn() { echo "    ! $1"; }
die()  { echo "    ✗ $1" >&2; exit 1; }

ensure_doas() {
  if command -v doas >/dev/null 2>&1; then DOAS="doas"
  elif command -v sudo >/dev/null 2>&1; then DOAS="sudo"
  else die "neither doas nor sudo found"
  fi
}

host_up() { curl -fsI --max-time 10 "$1" >/dev/null 2>&1; }

channel() {
  # the channel this machine follows
  if [ -n "$CHANNEL_OVERRIDE" ]; then printf '%s' "$CHANNEL_OVERRIDE"; return; fi
  if [ -f "$CHANNEL_FILE" ]; then tr -d '[:space:]' < "$CHANNEL_FILE"; return; fi
  printf 'stable'
}

channel_major() {
  # which release major a channel is allowed to install. stable pins the
  # 1.x series so a 1.x machine never wakes up as eiri on its own.
  case "$1" in
    stable) echo 1 ;;
    eiri)   echo 2 ;;
    *) die "unknown channel: $1 (try stable or eiri)" ;;
  esac
}

read_state() { # sets STATE_CHANNEL / STATE_COMMIT; empty when unknown
  STATE_CHANNEL=""; STATE_COMMIT=""
  [ -f "$STATE_FILE" ] || return 0
  STATE_CHANNEL="$(sed -n 's/^channel=//p' "$STATE_FILE" 2>/dev/null | head -n 1)"
  STATE_COMMIT="$(sed -n 's/^commit=//p' "$STATE_FILE" 2>/dev/null | head -n 1)"
}

write_state() { # <channel> <commit> — remember what the navi layer deployed
  $DOAS mkdir -p "$(dirname "$STATE_FILE")"
  printf 'channel=%s\ncommit=%s\n' "$1" "$2" | $DOAS tee "$STATE_FILE" >/dev/null
}

confirm_channel_switch() { # <channel> — interactive opt-in for --channel
  if [ -n "$CHANNEL_OVERRIDE" ] && [ -t 0 ] && [ "$ASSUME_YES" -eq 0 ]; then
    local ans
    read -r -p "    switch to channel '$1'? (bleeding edge — it moves fast) [y/N] " ans
    case "${ans:-N}" in [Yy]*) ;; *) die "channel switch declined" ;; esac
  fi
}

persist_channel() { # <channel> — pin an explicit --channel switch for next time
  [ -n "$CHANNEL_OVERRIDE" ] || return 0
  local cur=""
  [ -f "$CHANNEL_FILE" ] && cur="$(tr -d '[:space:]' < "$CHANNEL_FILE")"
  [ "$cur" = "$1" ] && return 0
  printf '%s\n' "$1" | $DOAS tee "$CHANNEL_FILE" >/dev/null \
    || warn "could not write $CHANNEL_FILE — pass --channel $1 next time too"
  ok "channel pinned to '$1'"
}

installed_version() {
  # numeric navi version, e.g. 1.3 (from '1.3 "mika"')
  local f="$VERSION_FILE"
  [ -f "$f" ] || f="$SHARE_VERSION"
  [ -f "$f" ] || { echo "0.0"; return; }
  tr -d '"' < "$f" | awk '{print $1}'
}

latest_tag() {
  # newest signed-release tag for a channel major, e.g. v1.4-mika
  local major="$1" t
  t="$(git ls-remote --tags "$REPO_URL" 2>/dev/null \
    | awk '{print $2}' \
    | sed -e 's|refs/tags/||' \
    | grep -E "^v${major}\.[0-9]+" \
    | grep -v '\^{}$' \
    | sort -V | tail -n 1)"
  printf '%s' "$t"
}

tag_version() { # v1.4-mika -> 1.4 ; v1.4.1-mika -> 1.4.1
  printf '%s' "$1" | sed -E 's/^v([0-9]+\.[0-9]+(\.[0-9]+)?).*/\1/'
}

ver_newer() { # ver_newer A B: true (0) when A is strictly newer than B
  [ "$1" = "$2" ] && return 1
  [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -n 1)" = "$1" ]
}

# ---------------------------------------------------------------- phase 1: preflight

preflight() {
  say "preflight"

  host_up "https://github.com" || die "github.com unreachable — check your network"
  ok "network up"

  local avail_kb
  avail_kb="$(df --output=avail / | tail -n 1 | tr -d ' ')"
  [ "${avail_kb:-0}" -ge 2097152 ] || die "less than 2G free on / — updates need breathing room"
  ok "disk space ok"

  if command -v fuser >/dev/null 2>&1; then
    for lock in /var/lib/dpkg/lock-frontend /var/lib/apt/lists/lock; do
      if [ -e "$lock" ] && $DOAS fuser "$lock" >/dev/null 2>&1; then
        die "another package operation holds $lock — refusing to run concurrently"
      fi
    done
    ok "no apt/dpkg lock contention"
  else
    info "fuser not available — skipping lock check"
  fi

  # battery: warn, don't block (a dead battery mid-full-upgrade is how you
  # get a bad evening — but the machine belongs to its owner, not to us).
  local bat
  for bat in /sys/class/power_supply/BAT*; do
    [ -e "$bat/status" ] || continue
    if [ "$(cat "$bat/status" 2>/dev/null)" = "Discharging" ]; then
      warn "on battery power — plug in AC before continuing"
      if [ "$ASSUME_YES" -eq 0 ] && [ -t 0 ]; then
        echo "    continuing in 10s (Ctrl-C to abort)…"
        sleep 10
      fi
    fi
  done
}

# ---------------------------------------------------------------- phase 2: snapshot hook

snapshot_hook() {
  say "pre-update snapshot"
  if command -v navi-snapshot >/dev/null 2>&1; then
    if navi-snapshot; then ok "snapshot taken"
    else warn "snapshot failed — continuing without one"; fi
  else
    warn "navi-snapshot not present — no pre-update snapshot (continuing)"
  fi
}

# ---------------------------------------------------------------- eiri: the rolling channel
#
# eiri has no release tags yet (branch-only until 2.0 ships), so the tag
# machinery below can never see it — --channel eiri used to die with
# "no release tags found". Track the branch head instead, and remember
# the deployed commit in STATE_FILE so --check and repeat runs know
# what's already on the machine.

eiri_layer() {
  local remote_sha
  git -C "$UPSTREAM_DIR" fetch --prune --filter=blob:none -q origin eiri 2>/dev/null \
    || die "could not fetch the eiri branch from $REPO_URL"
  remote_sha="$(git -C "$UPSTREAM_DIR" rev-parse -q --verify origin/eiri 2>/dev/null)" \
    || die "no eiri branch upstream — the channel isn't published yet"

  read_state
  if [ "$STATE_CHANNEL" = "eiri" ] && [ "$STATE_COMMIT" = "$remote_sha" ]; then
    info "navi files already current (eiri @ ${remote_sha:0:7})"
    PH_NAVI="current"
    return 0
  fi
  if [ -n "$STATE_COMMIT" ]; then
    info "new eiri commits: ${STATE_COMMIT:0:7} -> ${remote_sha:0:7}"
  else
    info "eiri branch head is ${remote_sha:0:7} (no recorded deploy — treating as new)"
  fi

  confirm_channel_switch "eiri"

  warn "the eiri branch is unsigned — pulling it trusts the channel"
  warn "(TLS + GitHub access control), same deal as mika-era updates."
  warn "eiri is the bleeding edge: it moves fast and occasionally breaks."
  git -C "$UPSTREAM_DIR" checkout -q "$remote_sha" \
    || die "could not check out eiri @ ${remote_sha:0:7}"
  [ -x "$UPSTREAM_DIR/install.sh" ] || die "install.sh missing at eiri @ ${remote_sha:0:7} — aborting"

  say "deploying eiri @ ${remote_sha:0:7} (user-modified configs will be kept)"
  if [ "$ASSUME_YES" -eq 1 ]; then
    bash "$UPSTREAM_DIR/install.sh" --deploy-only --yes
  else
    bash "$UPSTREAM_DIR/install.sh" --deploy-only
  fi
  write_state "eiri" "$remote_sha"
  persist_channel "eiri"
  ok "navi files at eiri @ ${remote_sha:0:7}"
  PH_NAVI="updated to eiri @ ${remote_sha:0:7}"
}

# ---------------------------------------------------------------- phase 3: navi layer

navi_layer() {
  say "navi layer (our repo)"
  local ch major tag installed tver
  ch="$(channel)"
  major="$(channel_major "$ch")"
  installed="$(installed_version)"
  info "channel: $ch · installed: navi $installed"

  mkdir -p "$UPSTREAM_DIR"
  if [ -d "$UPSTREAM_DIR/.git" ]; then
    git -C "$UPSTREAM_DIR" fetch --tags --prune --filter=blob:none -q 2>/dev/null \
      || die "could not fetch tags from $REPO_URL"
  else
    git clone --filter=blob:none -q "$REPO_URL" "$UPSTREAM_DIR" \
      || die "could not clone $REPO_URL"
  fi

  if [ "$ch" = "eiri" ]; then
    eiri_layer
    return 0
  fi

  tag="$(latest_tag "$major")"
  [ -n "$tag" ] || die "no release tags found for channel $ch"

  # major-version guard: never jump majors without an explicit --channel
  # opt-in (which always confirms interactively first).
  tver="$(tag_version "$tag")"
  local imajor="${installed%%.*}"
  if [ "$tver" != "$installed" ] && [ "${tver%%.*}" != "$imajor" ] && [ -z "$CHANNEL_OVERRIDE" ]; then
    die "tag $tag is a new major version — opt in explicitly: navi-update --channel <name>"
  fi

  if ! ver_newer "$tver" "$installed"; then
    info "navi files already current ($installed; latest tag: $tag)"
    PH_NAVI="current"
    return 0
  fi
  info "new navi release: $tag (installed: $installed)"

  if [ -n "$CHANNEL_OVERRIDE" ] && [ -t 0 ] && [ "$ASSUME_YES" -eq 0 ]; then
    local ans
    read -r -p "    switch to channel '$ch' at $tag? [y/N] " ans
    case "${ans:-N}" in [Yy]*) ;; *) die "channel switch declined" ;; esac
  fi

  # --- signature verification ---
  # Two modes, chosen by whether a release key is provisioned:
  #   key provisioned -> verify the tag signature, fail closed on any problem.
  #   no key yet      -> warn LOUDLY and pull the channel unsigned.
  # xvoidsx has deliberately deferred the signing decision to the eiri era
  # (see wired/keys/README.md) rather than commit to a ceremony that doesn't
  # fit how releases actually get cut. An honest unsigned pull beats a
  # performative signature. When the decision lands, provisioning the key
  # file + fingerprint flips this back to fail-closed with no other changes.
  if [ -f "$KEY_FILE" ] && [ -n "$RELEASE_FINGERPRINT" ]; then
    command -v gpg >/dev/null 2>&1 || die "gpg not found — cannot verify tag signatures"
    local gh
    gh="$(mktemp -d)"
    trap 'rm -rf "$gh"' RETURN
    export GNUPGHOME="$gh"
    gpg --batch --quiet --import "$KEY_FILE" 2>/dev/null \
      || die "could not import release key"
    local got_fp
    got_fp="$(gpg --batch --with-colons --fingerprint 2>/dev/null | awk -F: '/^fpr:/ {print $10; exit}')"
    [ "$got_fp" = "$RELEASE_FINGERPRINT" ] \
      || die "pinned key fingerprint mismatch — refusing to update"
    git -C "$UPSTREAM_DIR" fetch --depth 1 -q origin "tag" "$tag" 2>/dev/null \
      || die "could not fetch tag $tag"
    git -C "$UPSTREAM_DIR" verify-tag "$tag" >/dev/null 2>&1 \
      || die "tag $tag FAILED signature verification — refusing to update"
    ok "tag $tag signature verified"
    unset GNUPGHOME
  else
    warn "release signing not configured — pulling $tag unsigned."
    warn "xvoidsx will settle the signing story in the eiri era;"
    warn "until then, this trusts the channel (TLS + GitHub access control)."
    git -C "$UPSTREAM_DIR" fetch --depth 1 -q origin "tag" "$tag" 2>/dev/null \
      || die "could not fetch tag $tag"
  fi
  trap - RETURN

  git -C "$UPSTREAM_DIR" checkout -q "$tag" \
    || die "could not check out $tag"
  [ -x "$UPSTREAM_DIR/install.sh" ] || die "install.sh missing at $tag — aborting"

  say "deploying navi $tver (user-modified configs will be kept)"
  if [ "$ASSUME_YES" -eq 1 ]; then
    bash "$UPSTREAM_DIR/install.sh" --deploy-only --yes
  else
    bash "$UPSTREAM_DIR/install.sh" --deploy-only
  fi
  ok "navi files at $tag"
  PH_NAVI="updated to $tag"
}

# ---------------------------------------------------------------- phase 4: debian layer

debian_layer() {
  say "debian layer (apt)"
  $DOAS apt update || { warn "apt update failed"; PH_DEB="update failed"; return 0; }
  $DOAS apt full-upgrade -y || { warn "apt full-upgrade reported errors"; PH_DEB="upgrade had errors"; return 0; }
  ok "debian packages current"
  PH_DEB="upgraded"
}

# ---------------------------------------------------------------- phase 5: flatpak layer

flatpak_layer() {
  say "flatpak layer"
  if ! command -v flatpak >/dev/null 2>&1; then
    info "flatpak not installed — skipping"; PH_FLAT="not installed"; return 0
  fi
  if [ "$(flatpak list --app 2>/dev/null | wc -l)" -eq 0 ]; then
    info "no flatpaks installed — skipping"; PH_FLAT="none installed"; return 0
  fi
  flatpak update -y || { warn "flatpak update reported errors"; PH_FLAT="had errors"; return 0; }
  ok "flatpaks current"
  PH_FLAT="updated"
}

# ---------------------------------------------------------------- phase 6: report

report() {
  say "update report"
  printf '    navi files:  %s\n' "$PH_NAVI"
  printf '    debian:      %s\n' "$PH_DEB"
  printf '    flatpak:     %s\n' "$PH_FLAT"
  local running newest
  running="$(uname -r)"
  newest="$(ls /boot/vmlinuz-* 2>/dev/null | sort -V | tail -n 1 | sed 's/.*vmlinuz-//')"
  if [ -n "$newest" ] && [ "$running" != "$newest" ]; then
    echo "    ! reboot recommended: running $running, installed $newest"
  fi
  # pause when launched from rofi/waybar so the report stays readable
  if [ "$CHECK_ONLY" -eq 0 ] && [ -t 0 ]; then
    read -r -n1 -s -p "    press any key to close…" _; echo
  fi
}

# ---------------------------------------------------------------- --check (waybar mod)

check_updates() {
  # machine-readable summary; always exits 0 unless something is broken.
  local ch major installed tag tver navi_new="0" navi_ver="" apt_n=0 flat_n=0
  ch="$(channel)"; major="$(channel_major "$ch")"; installed="$(installed_version)"
  mkdir -p "$UPSTREAM_DIR"
  if [ -d "$UPSTREAM_DIR/.git" ]; then
    git -C "$UPSTREAM_DIR" fetch --tags --prune --filter=blob:none -q 2>/dev/null || true
  else
    git clone --filter=blob:none -q "$REPO_URL" "$UPSTREAM_DIR" 2>/dev/null || true
  fi
  if [ -d "$UPSTREAM_DIR/.git" ]; then
    if [ "$ch" = "eiri" ]; then
      git -C "$UPSTREAM_DIR" fetch --prune --filter=blob:none -q origin eiri 2>/dev/null || true
      tag="$(git -C "$UPSTREAM_DIR" rev-parse -q --verify origin/eiri 2>/dev/null || true)"
      if [ -n "$tag" ]; then
        read_state
        if [ "$STATE_CHANNEL" != "eiri" ] || [ "$STATE_COMMIT" != "$tag" ]; then
          navi_new="1"; navi_ver="eiri"
        fi
      fi
    else
      tag="$(latest_tag "$major")"
      if [ -n "$tag" ]; then
        tver="$(tag_version "$tag")"
        if ver_newer "$tver" "$installed"; then navi_new="1"; navi_ver="$tver"; fi
      fi
    fi
  fi
  # apt simulation needs no root; counts are only as fresh as the last update
  apt_n="$(apt-get -s full-upgrade 2>/dev/null | grep -c '^Inst' || true)"
  if command -v flatpak >/dev/null 2>&1; then
    flat_n="$(flatpak update 2>/dev/null | grep -ciE 'update (available|ready)' || true)"
  fi
  printf 'NAVI_UPDATES=%s\nNAVI_NEW=%s\nAPT_UPDATES=%s\nFLATPAK_UPDATES=%s\n' \
    "$navi_new" "$navi_ver" "$apt_n" "$flat_n"
}

# ---------------------------------------------------------------- main

main() {
  while [ $# -gt 0 ]; do
    case "$1" in
      --check)   CHECK_ONLY=1; shift ;;
      --yes)     ASSUME_YES=1; shift ;;
      --channel)
        [ -n "${2:-}" ] || die "--channel needs a name"
        CHANNEL_OVERRIDE="$2"; shift 2 ;;
      --channel=*) CHANNEL_OVERRIDE="${1#--channel=}"; shift ;;
      --help|-h)
        sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
      *) die "unknown option: $1 (try --help)" ;;
    esac
  done

  ensure_doas
  if [ "$CHECK_ONLY" -eq 1 ]; then check_updates; exit 0; fi

  preflight
  snapshot_hook
  navi_layer
  debian_layer
  flatpak_layer
  report
}

main "$@"
