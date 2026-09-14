#!/usr/bin/env bash
# navi-webapp — public CLI for the naviApps webapp catalog.
#
#   navi-webapp install <name>...   install one or more webapps
#   navi-webapp install all         install every webapp in the catalog
#   navi-webapp uninstall <name>... remove one or more webapps
#   navi-webapp uninstall --all     remove every installed navi webapp
#   navi-webapp list                list available webapps
#   navi-webapp doctor              diagnose launcher/icon issues
#   navi-webapp                     show this help
#
# The catalog lives in the deployed tree; this wrapper translates the
# public subcommand syntax to install-webapp.sh's flag syntax.

set -u

BACKEND="/usr/share/navi/wired/naviApps/install-webapp.sh"

usage() {
  cat <<'EOF'
usage: navi-webapp <command> [args]

commands:
  install <name>...   install one or more webapps
  install all         install every webapp in the catalog
  uninstall <name>... remove one or more webapps
  uninstall --all     remove every installed navi webapp
  list                list available webapps
  doctor              diagnose launcher/icon issues
EOF
}

if [[ $# -eq 0 ]]; then
  usage
  exit 0
fi

cmd="$1"
shift

case "$cmd" in
  install)
    if [[ $# -eq 0 ]]; then
      echo "navi-webapp install: missing <name>... or 'all'" >&2
      usage >&2
      exit 1
    fi
    if [[ "$1" == "all" ]]; then
      exec "$BACKEND" --all
    fi
    exec "$BACKEND" "$@"
    ;;
  uninstall|remove)
    if [[ $# -eq 0 ]]; then
      echo "navi-webapp uninstall: missing <name>... or --all" >&2
      usage >&2
      exit 1
    fi
    if [[ "$1" == "--all" ]]; then
      exec "$BACKEND" --uninstall-all
    fi
    exec "$BACKEND" --uninstall "$@"
    ;;
  list|ls)
    exec "$BACKEND" --list
    ;;
  doctor)
    exec "$BACKEND" --doctor
    ;;
  -h|--help|help)
    usage
    ;;
  *)
    echo "navi-webapp: unknown command '$cmd'" >&2
    usage >&2
    exit 1
    ;;
esac
