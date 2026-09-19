#!/usr/bin/env bash
# telegram installer 
# ( the extremely popular messaging application )
##################################
set -e
x="= = = = = telegram installer = = = = ="

install_telegram () {
    local tgramlink="https://telegram.org/dl/desktop/linux"
    wget -O- "$tgramlink" | tar -xJ && doas mv Telegram /opt/telegram && doas ln -sf /opt/telegram/Telegram /usr/local/bin/telegram
}

main () {
    echo "$x"; sleep 1
    install_telegram
    echo "  telegram has been installed - you can find it now in your launcher."
}
# - - - - - |
main
