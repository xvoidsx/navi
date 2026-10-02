# Tachibana — navi's Nix experiment

**Status:** experimental. This branch is a probe, not a commitment.
**Codename:** tachibana (navi 3). Tachibana Labs: the experimental wing.

## What this is

A whole-hog attempt at re-basing navi on NixOS. Try the idea fully, find the
gates, and if we hit a wall, fall back to the hybrid (Nix on Debian) or
Debian-level immutability. The walls are the data.

## Gates (decided 2026-10-02, before starting)

If any of these fails hard, pivot — don't push through on sunk cost:

1. **Cloudbook boots it.** NixOS runs the wired desktop (sway) on the Acer
   Aspire Cloudbook acceptably. Perma-computing is non-negotiable.
2. **Agent stack works.** ollama, wisp, herdr — the native agent layer —
   functional, not just installed.
3. **Sacred/enforced boundary is clean.** The config boundary has an
   architectural answer, not a hack. See philosophy below.
4. **Installer story is tractable.** Doesn't need to be solved — just not
   a nightmare.

## Philosophy: the OS decides the system, the user decides their space

Evolving from "the OS suggests, the user decides." navi is opinionated and
that's a strength — people choose navi *because* it has a point of view.

- **System-owned (enforced):** the theme, the agent wiring, the panel, the
  webapp platform (Chromium install, policies, wrapper scripts, launcher
  infra, blackice forcelist). This is what makes navi *navi*. It works a
  certain way, guaranteed, on every machine.
- **User-owned (sacred):** dotfiles, keybindings, workflow, browser profile,
  user-installed extensions and themes. Forever untouched by the system.

Nix makes this boundary architectural instead of policy-based: what's in the
system config is enforced, what's in `~/.config` is yours. No more
maintaining the line with diff scripts and good intentions.

Test case: the web layer. System owns the *platform* (installation, policy,
wrappers). User owns the *experience* (default browser pick, profile,
extensions). If we can articulate this cleanly for Chromium, we have the
template for everything else.

## Packaging: navipkgs

Own package layer via flake + overlay pinning upstream nixpkgs to an exact
commit — **not** a full nixpkgs fork. Same reproducibility and control,
fraction of the maintenance burden.

### What needs derivations

- **wisp** — Bun-built TUI. Build from source in the derivation long-term
  (vendored deps; sandbox has no network). Prebuilt binary is the lazy path.
- **navi-mods** — Go (`buildGoModule`), should be straightforward. Wrinkle:
  the `replace` directives pointing at `../theme` need handling (vendor or
  restructure source).
- **Electron apps** — fiddly (FHS expectations, binary downloads at install
  time). nixpkgs has Electron; it's annoying, not impossible.
- **Chromium webapp layer** — `programs.chromium` module covers policies and
  extensions declaratively. Wrapper scripts (`navi-webapp`,
  `navi-browser-run`) become proper packages with declared deps.
- **Hey Lain** — Python venv approach doesn't translate. Deps become Nix
  derivations; rethink, don't just repackage.

### The iceberg: FHS assumptions

Hundreds of shell scripts assume `/usr/bin`, `/usr/share`, `/etc` exist.
In NixOS they barely do. Every script needs deps declared or patched.
Not hard per-script — but it's *volume*. This determines whether the
rewrite is months or the better part of a year. Audit before estimating.

## Roadmap

1. **Design doc** (this file) — capture decisions so we don't re-litigate.
2. **Minimal boot** — barest NixOS flake: sway + waybar + alacritty on
   screen. VM first, then Cloudbook. Highest-signal test.
3. **One navi-mod packaged** — prove the navipkgs pipeline end to end.
4. **FHS audit** — catalog every assumption. Turn vibes into a count.
5. **Web layer** — Chromium module, wrappers, policies, launchers.
6. **wisp derivation** — Bun packaging.
7. **Agent stack** — ollama, herdr, the native layer.
8. **Installer story** — how does someone actually install tachibana?

## Non-goals (for now)

- Pi/ARM images. x86_64 first; ARM is a later gate.
- Replacing the Debian-based mika/eiri lines. They ship regardless.
- Perfection. This is a probe — rough edges are expected and informative.
