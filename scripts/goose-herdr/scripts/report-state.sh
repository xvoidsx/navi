#!/usr/bin/env bash
# navi-goose-herdr: report goose lifecycle state to the herdr pane hosting
# this goose session, so herdr — and navi's agent center / waybar agent mod —
# show goose as working/idle instead of unknown.
#
# Usage: report-state.sh <working|idle|blocked|unknown|release>
#
# This is herdr's official "custom socket integration" path: herdr sets
# HERDR_ENV=1 and HERDR_PANE_ID in every pane, and agents report their own
# state via `herdr pane report-agent`. No herdr fork, no upstream wait.
#
# No-op unless goose is running inside a herdr-managed pane. Every herdr
# call is best-effort and failures are swallowed — a broken hook must never
# break a goose session.

set -u

[ "${HERDR_ENV:-}" = "1" ] || exit 0
pane_id="${HERDR_PANE_ID:-}"
[ -n "$pane_id" ] || exit 0

herdr_bin="${HERDR_BIN_PATH:-}"
if [ -z "$herdr_bin" ] || [ ! -x "$herdr_bin" ]; then
  herdr_bin="herdr"
fi
command -v "$herdr_bin" >/dev/null 2>&1 || exit 0

state="${1:-idle}"
# This pair keys herdr's persistent per-(pane, source, agent) sequence
# watermark — it must stay stable forever; changing it orphans every pane
# already reported under the old pair.
source_id="navi:goose-herdr"
agent_label="goose"

case "$state" in
  release)
    "$herdr_bin" pane release-agent "$pane_id" \
      --source "$source_id" --agent "$agent_label" \
      >/dev/null 2>&1 || true
    ;;
  working|idle|blocked|unknown)
    "$herdr_bin" pane report-agent "$pane_id" \
      --source "$source_id" --agent "$agent_label" --state "$state" \
      >/dev/null 2>&1 || true
    ;;
  *)
    exit 0
    ;;
esac
