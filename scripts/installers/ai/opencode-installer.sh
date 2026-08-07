#/usr/bin/env bash
# opencode installer
# ( installs opencode )
##################################
set -e
x="= = = = = opencode installer = = = = ="
install_oc () {
  local link="https://opencode.ai/install"
  curl -fsSL "$link" | bash
  # append to bashrc
  echo 'export PATH="$HOME/.opencode/bin:$PATH"' >> $HOME/.bashrc
}
main () {
  echo "$x"; sleep 1
  install_oc
}  
# - - - - - |
main
