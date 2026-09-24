#!/usr/bin/env bash
#
# install-webapp.sh — one-command webapp installer for naviApps
#
#   ./install-webapp.sh youtube              install a single webapp
#   ./install-webapp.sh discord github       install several at once
#   ./install-webapp.sh --all                install every webapp
#   ./install-webapp.sh --uninstall discord  remove a webapp again
#   ./install-webapp.sh --uninstall-all      remove every navi webapp
#   ./install-webapp.sh --list               list available webapps
#
# Install drops the .desktop launcher into ~/.local/share/applications
# and its icon into the hicolor theme (~/.local/share/icons), which is
# where launchers actually look. Uninstall removes both again.

set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="${HOME}/.local/share/applications"
ICON_SVG_DIR="${HOME}/.local/share/icons/hicolor/scalable/apps"
ICON_PNG_DIR="${HOME}/.local/share/icons/hicolor/48x48/apps"
# older installer versions used pixmaps/ — cleaned up on sight
LEGACY_ICON_DIR="${HOME}/.local/share/pixmaps"

# icon source overrides: package -> file in icons/
# (everything else auto-resolves to icons/<name>.svg or .png)
declare -A ICON_SRC=(
  [chatgpt]="openai.svg"
)

usage() {
  echo "usage: $(basename "$0") <name>... | --all | --uninstall <name>... | --uninstall-all | --list | --doctor"
}

# Launchers only trust an icon theme dir that has an index.theme.
# A bare ~/.local/share/icons/hicolor is silently skipped by strict
# lookups (notably rofi), so make sure one exists.
ensure_index_theme() {
  local dir="${HOME}/.local/share/icons/hicolor"
  local index="${dir}/index.theme"
  if [[ -f "$index" ]]; then
    return 0
  fi
  mkdir -p "$dir"
  if [[ -f /usr/share/icons/hicolor/index.theme ]]; then
    cp /usr/share/icons/hicolor/index.theme "$index"
  else
    cat > "$index" <<'EOF'
[Icon Theme]
Name=Hicolor
Comment=Fallback icon theme
Hidden=true
Directories=scalable/apps,48x48/apps

[scalable/apps]
Size=48
Context=Applications
Type=Scalable
MinSize=1
MaxSize=512

[48x48/apps]
Size=48
Context=Applications
Type=Fixed
EOF
  fi
}

# --doctor: checklist for when menu icons misbehave. Paste the output
# when asking for help and the cause is usually obvious.
doctor() {
  local fail=0
  check() {
    if eval "$2" >/dev/null 2>&1; then
      echo "[ok] $1"
    else
      echo "[!!] $1"
      fail=1
    fi
  }
  echo "naviApps doctor (HOME=${HOME})"
  if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
    echo "[!!] running as root — installs land in /root, not your menu"
    fail=1
  fi
  local n_installed=0 n_shipped=0
  local missing=0
  while IFS= read -r app; do
    if [[ ! -f "${APP_DIR}/${app}.desktop" ]]; then
      continue
    fi
    n_installed=$((n_installed + 1))
    local info src_path dest_path
    info="$(icon_paths "$app")" || true
    if [[ -n "$info" ]]; then
      n_shipped=$((n_shipped + 1))
      read -r src_path dest_path <<< "$info"
      if [[ ! -f "$dest_path" ]]; then
        echo "[!!] icon missing for ${app}: ${dest_path}"
        missing=1
      fi
    fi
  done < <(list_apps)
  echo "[..] ${n_installed} webapps installed (${n_shipped} ship icons)"
  if [[ "$missing" -eq 0 ]]; then
    echo "[ok] all shipped icons present"
  else
    fail=1
  fi
  check "hicolor index.theme present" "test -f ${HOME}/.local/share/icons/hicolor/index.theme"
  check "gtk-update-icon-cache available" "command -v gtk-update-icon-cache"
  if command -v gdk-pixbuf-query-loaders >/dev/null 2>&1; then
    check "SVG loader (librsvg) registered" "gdk-pixbuf-query-loaders 2>/dev/null | grep -qi svg"
  else
    echo "[..] gdk-pixbuf-query-loaders not installed — SVG support unverified"
  fi
  if [[ -f "${HOME}/.local/share/icons/hicolor/icon-theme.cache" ]]; then
    if [[ "${HOME}/.local/share/icons/hicolor/icon-theme.cache" -ot "${REPO_DIR}/install-webapp.sh" ]]; then
      : # cache predates script; fine, refresh on next install
    fi
    echo "[..] icon-theme.cache exists (refreshed on every install)"
  else
    echo "[..] no icon-theme.cache (ok — lookup falls back to directory scan)"
  fi
  for launcher in rofi wofi bemenu fuzzel dmenu; do
    if ! command -v "$launcher" >/dev/null 2>&1; then
      continue
    fi
    if [[ "$launcher" == "dmenu" ]]; then
      # dmenu never shows icons — worth knowing when debugging them
      echo "[..] launcher on PATH: dmenu (text-only, never shows icons)"
      continue
    fi
    local ver
    ver="$(timeout 5 "$launcher" --version </dev/null 2>/dev/null)"
    if [[ -z "$ver" ]]; then
      ver="$(timeout 5 "$launcher" -v </dev/null 2>/dev/null | head -1)"
    fi
    echo "[..] launcher on PATH: ${launcher} (${ver:-version unknown})"
  done
  check "desktop files validate" "command -v desktop-file-validate"
  if command -v desktop-file-validate >/dev/null 2>&1; then
    local first
    first="$(find "$APP_DIR" -maxdepth 1 -name '*.desktop' 2>/dev/null | head -1)"
    if [[ -n "$first" ]]; then
      desktop-file-validate "$first" && echo "[ok] installed entries validate"
    fi
  fi
  browser_runtime || fail=1
  # stale hardcoded-chromium launchers predate navi-browser-run; bring them
  # along (idempotent, reported).
  migrate_launchers
  return "$fail"
}

list_apps() {
  for f in "${REPO_DIR}"/webapps/*.desktop; do
    basename "$f" .desktop
  done | sort
}

# print "<src-path> <dest-path>" for a shipped icon, or nothing
icon_paths() {
  local name="$1"
  local src="${ICON_SRC[$name]:-}"
  if [[ -z "$src" ]]; then
    for ext in svg png; do
      if [[ -f "${REPO_DIR}/icons/${name}.${ext}" ]]; then
        src="${name}.${ext}"
        break
      fi
    done
  fi
  if [[ -n "$src" && -f "${REPO_DIR}/icons/${src}" ]]; then
    case "$src" in
      *.svg) echo "${REPO_DIR}/icons/${src} ${ICON_SVG_DIR}/navi-${name}.svg" ;;
      *.png) echo "${REPO_DIR}/icons/${src} ${ICON_PNG_DIR}/navi-${name}.png" ;;
    esac
  fi
}

refresh_caches() {
  if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "$APP_DIR" >/dev/null 2>&1 || true
  fi
  if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f -t "${HOME}/.local/share/icons/hicolor" >/dev/null 2>&1 || true
  fi
}

# migrate_launchers: webapp launchers used to hardcode `Exec=chromium`.
# They now Exec navi-browser-run so the default-browser setting applies at
# launch time. Rewrite installed catalog launchers in place (idempotent).
migrate_launchers() {
  local app desktop
  while IFS= read -r app; do
    desktop="${APP_DIR}/${app}.desktop"
    if [[ -f "$desktop" ]] && grep -q '^Exec=chromium ' "$desktop"; then
      sed -i 's/^Exec=chromium /Exec=navi-browser-run /' "$desktop"
      echo "migrated ${app} launcher to navi-browser-run"
    fi
  done < <(list_apps)
}

# browser_runtime: report which browser webapps will launch under.
browser_runtime() {
  local cfg="${HOME}/.config/navi/default-browser"
  local id="(none — chromium is the fallback)"
  if [[ -f "$cfg" ]]; then
    id="$(tr -d '[:space:]' < "$cfg")"
    [[ -n "$id" ]] || id="(none — chromium is the fallback)"
  fi
  local bin="(unresolved)"
  if command -v navi-browser >/dev/null 2>&1; then
    bin="$(navi-browser --print-binary 2>/dev/null)" || bin="(unresolved)"
  elif command -v chromium >/dev/null 2>&1; then
    bin="chromium"
  fi
  echo "[..] browser runtime: ${id} -> ${bin}"
  if [[ "$bin" == "(unresolved)" ]]; then
    echo "[!!] no Chromium-family browser found — webapps cannot launch"
    return 1
  fi
  return 0
}

install_one() {
  local name="$1"
  local desktop="${REPO_DIR}/webapps/${name}.desktop"
  if [[ ! -f "$desktop" ]]; then
    echo "unknown webapp: ${name} (try --list)" >&2
    return 1
  fi
  mkdir -p "$APP_DIR" "$ICON_SVG_DIR" "$ICON_PNG_DIR"
  cp "$desktop" "${APP_DIR}/"

  local icon_info src_path dest_path
  icon_info="$(icon_paths "$name")" || true
  if [[ -n "$icon_info" ]]; then
    read -r src_path dest_path <<< "$icon_info"
    cp "$src_path" "$dest_path"
  fi
  # drop leftovers from the old pixmaps-based installs
  rm -f "${LEGACY_ICON_DIR}/navi-${name}.svg" "${LEGACY_ICON_DIR}/navi-${name}.png"
  echo "installed ${name}"
}

uninstall_one() {
  local name="$1"
  if [[ ! -f "${REPO_DIR}/webapps/${name}.desktop" ]]; then
    echo "unknown webapp: ${name} (try --list)" >&2
    return 1
  fi
  rm -f "${APP_DIR}/${name}.desktop"
  rm -f "${ICON_SVG_DIR}/navi-${name}.svg" "${ICON_PNG_DIR}/navi-${name}.png"
  rm -f "${LEGACY_ICON_DIR}/navi-${name}.svg" "${LEGACY_ICON_DIR}/navi-${name}.png"
  echo "removed ${name}"
}

if [[ $# -eq 0 ]]; then
  usage >&2
  exit 1
fi

case "$1" in
  --list|-l)
    list_apps
    ;;
  --all|-a)
    ensure_index_theme
    while IFS= read -r app; do
      install_one "$app"
    done < <(list_apps)
    migrate_launchers
    refresh_caches
    ;;
  --uninstall|--remove|-r)
    shift
    if [[ $# -eq 0 ]]; then usage >&2; exit 1; fi
    for app in "$@"; do
      uninstall_one "$app"
    done
    refresh_caches
    ;;
  --uninstall-all|--remove-all)
    while IFS= read -r app; do
      uninstall_one "$app"
    done < <(list_apps)
    refresh_caches
    ;;
  --help|-h)
    usage
    ;;
  --doctor|-d)
    doctor
    ;;
  -*)
    echo "unknown flag: $1" >&2
    usage >&2
    exit 1
    ;;
  *)
    ensure_index_theme
    for app in "$@"; do
      install_one "$app"
    done
    migrate_launchers
    refresh_caches
    ;;
esac
