#!/bin/sh
# naviApps — regenerate catalog.js from apps.json.
#
# The store page loads the catalog through a classic <script> include
# instead of fetch(): script tags work from file:// with no local server,
# no Chromium flags, and no CORS workaround. (Chromium also ignores
# command-line flags when it is already running, so a flag-based fix
# would only work when the browser wasn't open yet.)
#
# Re-run this whenever apps.json changes, then push both files.
set -eu
cd "$(dirname "$0")"
{
  printf '/* generated from apps.json — do not edit by hand. run ./build-catalog.sh */\n'
  printf 'window.NAVIAPPS_CATALOG = '
  python3 -c "import json; print(json.dumps(json.load(open('apps.json')), ensure_ascii=False, separators=(',',':')))"
  printf ';\n'
} > catalog.js
echo "catalog.js regenerated ($(python3 -c "import json; print(len(json.load(open('apps.json'))))") apps)"
