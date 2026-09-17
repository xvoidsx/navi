# AGENTS.md — navi

> **Read this first.** This file is the project's shared brain: everything an
> agent *or* a human needs to hit the ground running on navi development.
> It's maintained by the people (and agents) who do the work — when you learn
> something the hard way, add it here so the next one doesn't have to.

## What navi is

navi is xvoidsx's Linux distro: a complete, opinionated, **agent-native**
desktop OS on a dependable **Debian (trixie) base**. Wayland-first
(**sway**, our "wired" session), X11 fallback (**i3**). Deep blacks, neon
pink, phosphor green — the **nightshadeNeon** design language across the
whole desktop.

Guiding ideas:

- **Perma-computing.** navi keeps old hardware useful. The test bench is a
  2015–16 Acer Aspire Cloudbook: *if the installer works there, it works
  anywhere.* Never assume a fast machine; never ship something untested on
  slow hardware.
- **Additive, not purist.** We add our stack alongside upstream tools; we
  don't purge theirs. doas is the navi-native way up, sudo stays as the
  compatibility layer. GTK3 stays as long as possible; choice is preserved.
- **Agent-native.** ollama, opencode, omp, goose, and herdr ship out of the
  box. The OS is a window to the smallweb (amfora for Gemini, glyyph for
  Nostr, neighborli fediverse instance, navi radio).
- **Customized configs are sacred.** The updater *preserves* user-customized
  configs by design. New stock defaults never silently overwrite them —
  that's what `navi-wired-adopt` is for (explicit, backed up, loud warning).

## Who you're working with

- **Raven (rav3ndust)** — project lead, programmer, final arbiter. His word
  on scope and taste is law. He works the Cloudbook bench; **Cloudbook
  results outrank assumptions.** Mocks passing means nothing until hardware
  agrees.
- **Ryoko** — co-founder, owns all design.
- **Lain** — the xvoidsx agent (that's me, usually). Other agents (Codex,
  etc.) are *supplementary collaborators*, never replacements.
- **You** — whether you're an agent or a human reading this to learn the
  project's conventions: welcome. This file is for you.

## Repo map

```
navi/
├── install.sh            # installer AND deploy script (--deploy-only)
├── wired/                # the desktop layer — see "The /wired boundary"
│   ├── VERSION             # <-- source of truth for the version string
│   ├── sway/ i3/           # window manager configs (the "wired" sessions)
│   ├── waybar/             # bar config, style.css (GTK CSS!), modules,
│   │                       # mod-open.sh (the floating launcher)
│   ├── identity/           # os-release, lsb-release, issue, issue.net
│   │                       # (re-stamped from VERSION at every deploy)
│   ├── naviApps/           # the app store: apps.json + build-catalog.sh
│   │                       # → catalog.js (inlined; never fetch() it)
│   ├── scripts/            # navi-notifs, helpers
│   ├── manual/             # built-in manual (Super+Shift+H); mirrored on site
│   ├── applications/       # .desktop launchers incl. rofi-visible entries
│   └── ...                 # per-app configs: rofi, alacritty, dunst, tmux…
├── scripts/                # system-level scripts (NOT the desktop layer)
│   ├── navi-update.sh        # self-updater: pulls release tags, redeploys
│   ├── navi-wired-restore.sh # drift diff / restore / backup vs stock
│   ├── navi-wired-adopt.sh   # lay stock wired configs over customized ones
│   └── installers/           # codex, antigravity, etc.
├── iso/
│   ├── stage.sh            # stages /opt/navi-iso for the ISO build
│   ├── installer/          # the CLI installer (preflight lives here)
│   ├── config/             # iso.yml inputs: RELEASE, ISO_BASENAME…
│   └── README.md
├── .github/workflows/
│   ├── iso.yml             # builds the ISO; RELEASE CHECKLIST lives here
│   └── publish-stable.yml  # publishes tag → GitHub release + assets
├── src-navi/ src-wiredWM/  # source trees (see RESTRUCTURE.md)
├── screenshots/
└── _sources/
```

## The /wired boundary

`/wired` is the **desktop layer only**. Distro-level concerns (base system,
users, doas/sudo, services, firewall) live elsewhere in the repo. **Never
move system-level pieces into /wired**, and never reach out of it for them.

## Source of truth: `wired/VERSION`

The version string (e.g. `1.4.6 "mika"`) lives in `wired/VERSION`. Everything
human-facing is derived from it at deploy time:

- `install.sh`'s `install_identity` re-stamps `wired/identity/` (os-release,
  lsb-release, issue, issue.net) from VERSION **on every deploy** — including
  `--deploy-only`, so `navi-update` refreshes them. These files go stale
  silently otherwise (they read 1.3 all through the 1.4 cycle once).
- The installer banner and success box, `iso/config` RELEASE/ISO_BASENAME/
  volume label, and `iso/README.md` must all be checked per release.

**Rule:** `iso.yml` carries a RELEASE CHECKLIST (including a POST-RELEASE
section). Follow it every release. It exists because every item on it was
once a shipped bug.

## install.sh — installer and deployer

`install.sh` does double duty: fresh installs *and* redeploys onto a live
system via `--deploy-only` (this is what `navi-update` runs after pulling).

- Writes a deploy manifest to `/var/lib/navi/deploy-manifest.tsv`
  (`sha256/version/relpath/dest/mode`) so drift can be detected later.
- Sets `install -m 0755` on `/usr/bin` copies and `chmod 0755` on every
  `*.sh` under `/usr/share/navi` — scripts run no matter how the repo was
  fetched (clone, zip, tarball).
- After deploying configs, it **reloads long-running processes that don't
  reread configs themselves** — e.g. it sends a running waybar its
  documented `SIGUSR2` config-reload. If you deploy a config for a daemon,
  check whether the daemon picks it up on its own; if not, signal it.
- sudo layer: writes `/etc/sudoers.d/10-navi-user` (`<user> ALL=(ALL:ALL)
  ALL`, password-required), validated with `visudo -c`, self-removing on
  check failure. Effective on the *next* sudo call — no re-login (group
  membership needs a re-login; a sudoers rule doesn't).

## The self-maintenance layer

Three commands, three jobs. Don't confuse them:

- **`navi-update`** — pulls the release *channel* (release **tags**, never
  arbitrary main commits — a fix on main is invisible to the updater until
  it's in a tag) and re-runs deploy. `--check` for dry status; `--channel`
  for channel selection. Two-mode trust: signing key provisioned → verify
  fail-closed; no key → **loud warning** + unsigned pull. Preserves
  customized configs by design.
- **`navi-wired-restore`** — shows how the live setup drifted from stock and
  restores what you choose, with backups of everything it touches.
- **`navi-wired-adopt`** — the explicit "give me the new stock desktop"
  path: lays current stock wired configs over customized ones, backup first,
  loud warning, `--dry-run` to preview, offers WM/bar reload when done.
  Has a rofi launcher.

## navi mods always float (standing rule)

Panel modules (notification bell, update indicator, …) open floating
Alacritty TUIs. **Launchers enforce floating themselves** — never rely on the
shipped sway config's `for_window` rules (customized configs are preserved,
so the rule may not exist) and never sleep-then-float (a race slow machines
lose).

The convention is `wired/waybar/mod-open.sh <title> <cmd…>`:

1. **Probe, don't guess, the compositor.** Real `swaymsg -t get_version`
   round-trip (then `i3-msg -t get_version`); never trust `$SWAYSOCK` alone.
2. Install a **runtime `for_window` rule BEFORE the window maps** — no race.
3. **Poll-and-float by PID as the backstop.** Titles are mutable (TUIs
   rename themselves); PIDs aren't. Anchor the match so PID `1234` can't
   match `12345`.
4. Log one line per launch to `~/.local/share/navi/mod-open.log` — future
   misbehavior must leave footprints.
5. If there's no compositor at all, just run the command (plain-open path).

## Release process, end to end

1. Bump `wired/VERSION`; run the **iso.yml RELEASE CHECKLIST** (every
   human-facing string: VERSION, installer banner + success box, iso config
   RELEASE/ISO_BASENAME/volume label, iso/README.md, identity files).
2. Commit to `main` (the working branch; the `mika` line merged into main).
3. Tag **`v<version>-mika`** (e.g. `v1.4.6-mika`). All 1.x releases are
   **intentionally unsigned** — signing is deferred to the eiri era. (The
   release-signing GPG key *exists*; it's just not wired into 1.x.)
4. `iso.yml` builds the ISO artifact (`navi-<version>-mika.iso`).
5. `publish-stable.yml` (parameterized by tag) publishes the GitHub release
   with the ISO + `SHA256SUMS.txt`. `/releases/latest` then resolves to it.
6. POST-RELEASE: bump the website (`xvoidsx/enter-the-wired`:
   index.html, roadmap.html, news.html — keep download buttons on
   `/releases/latest`, never a pinned tag; follow the news-archive
   convention below) and the navi README version strings.
7. Write release notes; keep a copy under `~/workspace/your_files/`.

**Release naming:** 1.x = **"mika"**, 2.x = **"eiri"**. Tags look like
`v1.4.6-mika`. The updater discovers "newer" via `sort -V` over tags
matching `^v1\.[0-9]+`, so patch versions sort correctly.

**Never claim** publication, updater success, hardware validation, or public
deployment without verifying it. The ping/announcement is a contract — keep
it, and only for what's actually done.

## Hard-won rules (each one cost us a release)

- **Test against the REAL shape, never a hand-made mock.** Scripts parsing
  external-command output must be tested against live captured output.
  `dunstctl history` is busctl JSON: every D-Bus variant arrives wrapped as
  `{"type": "<sig>", "data": <value>}` — mocks with plain strings passed
  while real data crashed.
- **`python3 -c` has two traps:** no backslashes inside f-string expressions
  (`f"{x(\"y\")}"` is a SyntaxError on our Python) and no bare `return` at
  top level (module scope — use `sys.exit()`). Extract the `-c` body and run
  it standalone against sample input.
- **Waybar `style.css` is GTK CSS, not browser CSS.** The parser rejects
  comma-separated keyframe selectors — `0%, 100% { }` fails with "Expected
  closing bracket after keyframes block". One selector per keyframe rule.
- **`iso/stage.sh` must copy EVERYTHING `install.sh` reads from `$REPO_DIR`.**
  If `install.sh` deploys from `$REPO_DIR/<dir>`, stage.sh must stage `<dir>`
  into `/opt/navi-iso` **and** the installer's preflight must require it.
  (Missing `scripts/` once meant fresh ISOs silently skipped `navi-update`.)
- **Runtime rules before the window maps.** Sleep-then-float races slow
  machines; install the rule first, then launch.
- **Local trees can be stale in EITHER direction.** Before pushing, fetch
  each affected file's remote version and diff — the local copy is not
  automatically the newest.
- **`gh-push-mika` overwrites remote files with LOCAL content** (and stores
  `*.sh` as mode 100755). A stale local tree once shipped an ISO package
  list with the network stack silently dropped. Fetch + diff first, every
  time. (Transient HTTP 422 "Tree SHA does not exist" at commit creation:
  retry once before investigating.)
- **Field failures get field-grade diagnostics.** When a launcher misbehaves
  on hardware, it must leave logs (`mod-open.log`); diagnose the live
  output, not the mocks.

## naviApps catalog rule

The store page must **NEVER `fetch()` its catalog** — `file://` CORS plus
Chromium ignoring CLI flags when already running breaks it. The catalog
ships **inlined** as `wired/naviApps/catalog.js`, generated from
`apps.json` by `wired/naviApps/build-catalog.sh`. **Re-run the script and
push BOTH files whenever `apps.json` changes.**

## Seamless GIF loops

Modulo-wrapping a position does NOT make a loop seamless. Every translating
element must move an **integer number of spatial periods per loop**
(`v * loop_seconds ≡ 0 mod period`). Verify frame N against frame 0
numerically, not by vibes. (Keep flagship gifpapers under GitHub's 25 MB
file limit so they can ship in the repo.)

## Website conventions

- Repo: `xvoidsx/enter-the-wired`. Pages: index, roadmap, news, manual
  (mirrored from `wired/manual/` — **update both copies together**),
  sponsors, labs, community.
- Download buttons always track `/releases/latest`, never a pinned tag.
- **News archive convention:** the 3 newest posts stay full; older posts move
  into the "older signals" archive as `<details>` collapsibles. When adding
  a post, demote the 4th-newest.
- Footer creed: "open models, open web" — quiet, not shouty.

## House conventions

- **Time:** 24-hour clock, day-first dates ("17 September", "09 September").
- **Commits:** descriptive messages; reference what the change fixes and why.
- Don't delete or drop unmigrated legacy pieces (power menu, remoji, learn
  help system, polybar battery script, alternate themes, Chromium theme
  data) without Raven's explicit decision — keep/drop calls are his.
- When Raven asks what you think, give a real opinion with tradeoffs named,
  not a poll.

## The one-sentence version

Probe, don't guess; test the real shape on real hardware; preserve the
user's customizations; log everything; and never claim what you haven't
verified.
