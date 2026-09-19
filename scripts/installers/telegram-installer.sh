#!/usr/bin/env bash
# telegram installer
# ( the extremely popular messaging application )
##################################
set -e
x="= = = = = telegram installer = = = = ="

# official app icon, verified against the tdesktop repo
ICON_URL="https://raw.githubusercontent.com/telegramdesktop/tdesktop/dev/Telegram/Resources/art/icon256.png"

install_telegram () {
    local tgramlink="https://telegram.org/dl/desktop/linux"
    local tmpdir
    tmpdir="$(mktemp -d)"
    trap 'rm -rf "$tmpdir"' EXIT
    # unpack in a temp dir so we never litter the caller's cwd
    ( cd "$tmpdir" && wget -O- "$tgramlink" | tar -xJ )
    # clear any previous install so a re-run upgrades cleanly instead of nesting
    doas rm -rf /opt/telegram
    doas mv "$tmpdir/Telegram" /opt/telegram
    doas ln -sf /opt/telegram/Telegram /usr/local/bin/telegram
    # launcher entry + icon, so it actually shows up in rofi
    doas wget -q -O /usr/share/pixmaps/telegram.png "$ICON_URL"
    doas tee /usr/share/applications/telegram.desktop >/dev/null <<'EOF'
[Desktop Entry]
Version=1.0
Name=Telegram
Comment=Fast, cloud-synced messaging
Exec=/opt/telegram/Telegram
Icon=telegram
Terminal=false
Type=Application
Categories=Network;InstantMessaging;
StartupNotify=true
EOF
}

main () {
    echo "$x"; sleep 1
    install_telegram
    echo "  telegram has been installed - you can find it now in your launcher."
}
# - - - - - |
main
