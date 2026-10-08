#!/usr/bin/env bash
# brave-seed-lib.sh — shared Brave vision seeding for navi's installer scripts.
#
# Source from a Brave-variant installer after installing the browser:
#   source "$(dirname "${BASH_SOURCE[0]}")/brave-seed-lib.sh"
#   seed_brave_variant "Brave-Browser-Nightly"
#
# Applies navi's Brave vision (ultradark, dark color scheme, wide address
# bar, compact tabs) to the variant's profile dir — the same defaults
# install.sh's setup_brave seeds for release Brave. Existing files are
# never overwritten: the user can change everything afterwards.
# No side effects on source; safe under `set -e`.

# seed_brave_variant <BraveSoftware-subdir> — e.g. "Brave-Browser-Nightly".
# Seeds $HOME and (best-effort, via doas) /etc/skel.
seed_brave_variant() {
  local subdir="$1"
  local dir="$HOME/.config/BraveSoftware/$subdir"
  local skel_dir="/etc/skel/.config/BraveSoftware/$subdir"

  if pgrep -x brave >/dev/null 2>&1; then
    echo "  Brave is running — skipping $subdir seed (retry after quitting it)"
    return 0
  fi

  _seed_brave_dir "$dir" ""
  _seed_brave_dir "$skel_dir" "doas"
}

# _seed_brave_dir <dir> [doas] — write the two seeded files if missing.
_seed_brave_dir() {
  local dir="$1" priv="${2:-}"
  local run=""
  [ -n "$priv" ] && run="doas"

  $run install -d -m 755 "$dir/Default" 2>/dev/null || {
    echo "  (could not write $dir — skipping)"
    return 0
  }

  if [ ! -f "$dir/Default/Preferences" ]; then
    $run tee "$dir/Default/Preferences" >/dev/null <<'EOF'
{
  "browser": {
    "theme": {
      "color_scheme2": 2
    }
  },
  "brave": {
    "darker_mode": true,
    "location_bar_is_wide": true
  }
}
EOF
    echo "  navi vision seeded: $dir/Default/Preferences"
  fi

  if [ ! -f "$dir/Local State" ]; then
    $run tee "$dir/Local State" >/dev/null <<'EOF'
{
  "brave": {
    "tabs": {
      "compact_horizontal_tabs": true
    }
  }
}
EOF
    echo "  navi vision seeded: $dir/Local State"
  fi
}
