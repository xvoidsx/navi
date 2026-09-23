# Microsoft Edge on Linux — navi feedback tracker

This is xvoidsx's public tracker for Microsoft Edge issues on Linux that matter
to navi users. It records the bugs we've found, the feedback we've sent
upstream to the Edge team, and the status of each item. If you're on the Edge
Linux team: hello, this page is for you — it's the single place to see what
we've reported and what's still open.

## Why navi cares about Edge

navi is about choosing your favorite tools and personalizing the web your way.
Edge is a legitimate Chromium-based option, and it has real strengths: its
energy-saving features are excellent, with granular sleeping-tab controls that
go beyond what most Chromium browsers expose. On a low-power navi mini, that
matters.

To be upfront: Edge is a **user choice** on navi, not a default. Its telemetry
story doesn't fit navi's zero-telemetry ethos, and we'd rather say that
plainly than pretend otherwise. What we want is for the choice to be a good
one — a reliable, well-behaved Edge on Linux.

## How Chromium integrates with navi

For context on where these issues bite:

- navi's webapps (navi radio, neighborli, glyyph, naviApps, and user-added
  ones) launch through `/usr/bin/navi-webapp` as Chromium `--app` windows.
- navi manages Chromium-family browsers with managed policies: force-installed
  nightshadeNeon theme, the blackice ad-blocker, and Proton Pass.
- Planned: a settings TUI to choose which installed Chromium-based browser the
  webapps launch through (tracked in the navi repo). The picker stays
  Chromium-based — `--app` windows and policy management only work there.

## Open issues

| ID | Issue | Detail | Reported | Status |
|----|-------|--------|----------|--------|
| EDGE-LINUX-001 | Copilot button broken on Linux | The Copilot button in the toolbar does not function on Linux builds. | 2026-09-23, via Edge in-app feedback (screenshot + video + write-up) | Open — awaiting Edge team response |
| EDGE-LINUX-002 | "Use system title bar and borders" has no effect | Enabling the option produces no change at all — the custom Edge chrome stays exactly as it was, as if the toggle weren't there. | 2026-09-23, via Edge in-app feedback (screenshot + video + write-up) | Open — awaiting Edge team response |

## Feedback log

- **2026-09-23** — Raven (rav3ndust), navi maintainer, submitted both issues
  above to the Edge Linux team through Edge's built-in feedback system, with a
  screenshot, a screen recording demonstrating each bug, and a detailed
  write-up. The submission noted he maintains navi and wants to include Edge
  as a reliable browser option in the distro.

  > Lain's thoughts: the unusual thing here isn't the bugs — it's the
  > direction of the pressure. Distro maintainers almost never lobby Microsoft
  > for better Linux support, and that novelty is leverage worth spending
  > deliberately: two well-documented bugs with video evidence from a real
  > maintainer beats a hundred vague forum complaints. Also glad we're being
  > upfront about the telemetry tradeoff in the same doc where we ask for
  > fixes — "we like your browser, we won't make it the default, here's why"
  > is a more credible posture, and credibility is the whole currency here.

## Verification

When the Edge team ships fixes, we'll verify on navi eiri (Debian trixie
base) against Edge Dev and Stable, and update the statuses above. A fix isn't
"done" until it's confirmed on real navi hardware.

## Contributing

Found an Edge-on-Linux bug that affects navi? Open an issue on the navi repo
and it'll be triaged into this tracker.
