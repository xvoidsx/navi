#!/usr/bin/env bash
# hermes installer
# ( installs Hermes Agent by Nous Research — navi's default agent, "Lain" )
# NOTE: we do NOT pipe-to-bash. The upstream install script is downloaded,
# inspected, and run explicitly. (navi rule, cf. Etcher.)
##################################
set -e
x="= = = = = hermes installer = = = = ="

HERMES_INSTALL_URL="https://raw.githubusercontent.com/NousResearch/hermes-agent/main/scripts/install.sh"

install_hermes () {
    # python 3.11+ is required
    if ! command -v python3 >/dev/null 2>&1; then
        echo "  python3 not found — installing..."
        doas apt install -y python3 python3-venv python3-pip
    fi
    local pyver
    pyver=$(python3 -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')
    echo "  python $pyver detected"

    # fetch the upstream installer where we can see it
    local tmp
    tmp=$(mktemp -d)
    # shellcheck disable=SC2064
    trap "rm -rf '$tmp'" EXIT
    echo "  downloading hermes install script..."
    curl -fsSL "$HERMES_INSTALL_URL" -o "$tmp/hermes-install.sh"
    echo "  --- upstream script head (first 20 lines) ---"
    head -20 "$tmp/hermes-install.sh"
    echo "  --- end head ---"
    echo "  running hermes install script..."
    bash "$tmp/hermes-install.sh"

    # make sure hermes is on PATH
    if ! command -v hermes >/dev/null 2>&1; then
        echo "  WARNING: hermes not on PATH after install — check $HOME/.local/bin"
    fi
}

deploy_lain_layer () {
    # the Lain identity layer: persona, theme, and knowledge scaffolding.
    # Hermes is the engine (Nous Research, MIT); Lain is the navi experience.
    local dest="$HOME/.config/hermes/lain"
    local src
    src="$(dirname "$0")/../../wired/lain"
    # when installed, wired lives at /usr/share/navi/wired
    [ -d "$src" ] || src="/usr/share/navi/wired/lain"
    if [ -d "$src" ]; then
        mkdir -p "$dest"
        cp -r "$src/." "$dest/"
        echo "  lain identity layer deployed to $dest"
    else
        echo "  WARNING: lain source not found — skipping identity layer"
    fi
}

main () {
    echo "$x"; sleep 1
    install_hermes
    deploy_lain_layer
    echo ""
    echo "  hermes has been installed — navi's default agent, Lain."
    echo "  run 'hermes setup' to choose your model (local or cloud),"
    echo "  or launch it now with 'navi-Q'."
}
# - - - - - |
main
