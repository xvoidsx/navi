#!/usr/bin/env bash
# navi hardware detection helpers.
#
# sourced, never executed:
#   source "$REPO_DIR/scripts/lib/hardware.sh"
#
# deliberately memory-based, not model-based: a weak machine is a weak
# machine, whatever the badge says. no DMI allowlist to maintain — the
# cloudbook bench, old thinkpads, and little pis all answer the same
# question: how much RAM is there?
#
# override the low-memory threshold (KiB) with NAVI_LOWMEM_KB.

# total RAM in KiB, from /proc/meminfo
navi_memtotal_kb() {
    awk '/^MemTotal:/ {print $2}' /proc/meminfo 2>/dev/null
}

# total swap in KiB across all swap devices (zram included)
navi_swaptotal_kb() {
    awk '/^SwapTotal:/ {print $2}' /proc/meminfo 2>/dev/null
}

# true (exit 0) when this machine is low on memory.
# default threshold: 2 GiB.
navi_is_lowmem() {
    local threshold="${NAVI_LOWMEM_KB:-2097152}"
    local total
    total="$(navi_memtotal_kb)"
    [ -n "$total" ] && [ "$total" -le "$threshold" ]
}

# true (exit 0) when a zram swap device is already active
navi_zram_active() {
    [ -e /sys/block/zram0 ]
}
