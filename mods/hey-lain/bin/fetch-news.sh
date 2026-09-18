#!/usr/bin/env bash
# fetch-news.sh [topic words...] — prints speakable top headlines.
# Empty topic = top stories. Source: Google News RSS (no key needed).
# Exit nonzero on failure (caller falls back to opening a browser search).
set -uo pipefail

Q="$*"
if [ -z "${Q// }" ]; then
  URL='https://news.google.com/rss?hl=en-US&gl=US&ceid=US:en'
  LEAD="Here are the top headlines."
else
  QE=$(python3 -c "import urllib.parse,sys; print(urllib.parse.quote_plus(sys.argv[1]))" "$Q")
  URL="https://news.google.com/rss/search?q=$QE&hl=en-US&gl=US&ceid=US:en"
  LEAD="Here are the headlines on $Q."
fi

python3 - "$URL" "$LEAD" <<'EOF' 2>/dev/null || exit 1
import sys, re, html, urllib.request, xml.etree.ElementTree as ET
url, lead = sys.argv[1], sys.argv[2]
req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
xml = urllib.request.urlopen(req, timeout=10).read()
root = ET.fromstring(xml)
titles = []
for item in root.findall(".//item")[:4]:
    t = (item.findtext("title") or "").strip()
    t = html.unescape(t)
    t = re.sub(r"\s+-\s+[^-]+$", "", t)   # strip " - Source"
    t = re.sub(r"\s+", " ", t).strip()
    if t:
        titles.append(t[:160])
if not titles:
    sys.exit(3)
parts = [lead] + [f"{i}: {t}." for i, t in enumerate(titles, 1)]
print(" ".join(parts))
EOF
