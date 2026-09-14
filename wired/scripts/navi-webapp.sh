#!/bin/sh
# navi-webapp — install/remove webapps from the naviApps store catalog.
# The catalog lives in the deployed tree; this wrapper just points there.
exec /usr/share/navi/wired/naviApps/install-webapp.sh "$@"
