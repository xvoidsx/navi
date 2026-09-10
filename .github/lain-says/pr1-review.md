## ∅ Review: Mika restructure

**What this PR does:** lays the new repository foundation — the entire desktop
configuration layer moves to `/wired`, `install.sh` becomes the provisioning
entry point, and the repo gains its identity (navi os-release, fastfetch
config, CONTRIBUTORS, and yours truly). 62 files added, zero modified or
deleted — the old layout sits undisturbed underneath.

**What's solid:**
- The `/wired`-as-desktop-layer split is the right call, and it's cleanly executed
- Additive-only means this is trivially reviewable and revertible
- `install.sh` is executable and passes syntax checks; the backports handling is idempotent
- Dropping i3status/i3blocks in favor of Polybar on X11 matches how navi actually runs today

**Before this merges:**
1. **The clean-machine test is the gate.** Nothing here has run on real hardware
   yet — SDDM theme, identity files, wallpaper startup, all of it. The weekend
   run (Sept 12–13) decides.
2. **Missing pieces in the tree:** the wallpaper binaries (`lain3wp.jpg`,
   `navi-lain.gif`) aren't in the draft yet; the mpvpaper fork still needs its
   fixes; the agent installers (ollama, opencode, omp) aren't implemented.
3. **Open decisions:** `/usr/bin` vs `/usr/local/bin` for deployed commands;
   the mpvpaper-first/failure-fallback startup still needs a real session test.

**Verdict:** keep as draft. This is exactly the right foundation, but merging
before the clean-machine run would bless an untested installer. Prove it on
hardware first, then ship it.

— Lain ∅
