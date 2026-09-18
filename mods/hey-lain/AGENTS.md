# hey-lain — voice assistant for Navi (Debian 13, sway-based wiered WM)

Tap-to-talk desktop voice agent named **Lain**. `Alt+V` tap starts mic
capture, second tap stops it; speech is transcribed locally, answered by a
local LLM (plus a deterministic intent layer), acted on via sway IPC, and
spoken back with a local Kokoro female voice. Fully on-device: no cloud,
no auth, no accounts.

## How it works (one utterance, end to end)

1. **Tap 1** — sway runs `bin/toggle.sh`. No recorder is active, so it execs
   `bin/record-start.sh`: a unique session wav is created under
   `${XDG_RUNTIME_DIR:-/tmp}/hey-lain/` (tmpfs, gone on reboot), `pw-record`
   captures 16kHz mono into it, and `bin/listen-overlay.sh show` pops a
   nightshadeNeon "LISTENING" overlay (floating + sticky `foot` window,
   top-center, above all windows).
2. **Tap 2** — sway runs `toggle.sh` again. A recorder IS active, so it execs
   `bin/hey-lain.sh`, which:
   - `bin/record-stop.sh` kills the recorder (pidfile INT→TERM→KILL plus a
     `pkill` pattern sweep so orphans are impossible) and prints the session
     wav path. The overlay STAYS UP through everything below (HEARD →
     THINKING → ACTING states) and hides only when speech begins.
   - `bin/listen.py` transcribes it with `faster-whisper tiny.en` (CPU int8).
     **The wav is deleted immediately after transcription**, before any
     LLM/network work. Empty results speak "I didn't catch that…".
   - `bin/brain.sh` turns the transcript into a reply: deterministic intents
     first (instant `[ACTION: …]` tags for commands), local Ollama chat
     (`gemma3:270m` + `SYSTEM_PROMPT.txt`) for everything else, spoken
     offline fallback if the LLM is unreachable.
   - `bin/actions.sh` splits the reply into side effect + speech: it
     executes leading `[ACTION: name args]` tags in order (notify, open,
     volume, media, brightness, lock, screenshot, close) and prints the
     speech-only text.
   - `bin/speak.py` synthesizes the speech with Kokoro (`af_heart` female
     voice) sentence-by-sentence, streaming raw s16le PCM straight into
     `aplay` — zero wav files. The `notify-send "Hey Lain!"` notification
     fires WITH the first audible audio, not before synthesis.
3. **During processing** the `flock` single-flight lock is held: extra taps
   get a polite "Working on it" notice instead of stacking runs. A 1s
   debounce swallows key-bounce/double-fire.

## Components

- `bin/toggle.sh` — the ONLY keybind entry point. Debounce + processing-lock
  check + recorder detection verified against `/proc/<pid>/cmdline` (never
  bare `pgrep -f`, which self-matches, and never `$!` pidfiles alone, which
  go stale — both caused real orphan-recorder bugs here).
- `bin/record-start.sh` / `bin/record-stop.sh` — capture lifecycle. One
  unique `input-<timestamp>-<pid>.wav` per session; current path tracked in
  `$RUNTIME/current`. `arecord` is the fallback if `pw-record` is missing.
  `record-start` verifies the recorder survives launch and starts
  `bin/mic-level.py` (live mic levels → `$RUNTIME/mic-level`, stdlib-only,
  guarded against doubling); `record-stop` kills it (pidfile + pattern
  sweep) and drops the level file. `record-stop`
  reports byte counts to the log.
- `bin/mic-level.py` — tails the in-progress recording wav (robust wav-header
  skip), publishes 0-100 peak levels of the most recent ~50ms to
  `$RUNTIME/mic-level` at 10Hz; exits when the wav stops growing.
- `bin/listen.py` (runs on `venv/bin/python`) — STT guards, in order:
  reject unreadable files, skip clips under 0.4s, transcribe with VAD,
  retry without VAD if empty, drop no-speech hallucinations (tiny.en hears
  "Hello"/"Thank you" in room tone: all segments `no_speech_prob > 0.7`
  with <25 chars of text counts as silence). Errors go to stderr and are
  logged by the caller — never swallowed.
- `bin/brain.sh` — intent layer, then LLM. Intents handled deterministically:
  `open/launch/start <app|site>` (site-name map incl. discord/spotify web
  apps + bare-domain→https), COMPOUND `open <site> and search|play <query>`
  and general multi-intent requests with several ordered actions
  (per-site search URLs via `site_search`: youtube/github/reddit/wikipedia/
  stackoverflow/amazon/twitch/spotify templates, site-restricted DDG
  fallback, plain DDG last resort — MUST run before the plain-open rule or
  the whole phrase becomes one app name), `search|look up|google`,
  weather queries (→fetch-weather, location extracted from transcript,
  ask-back if empty), news/headlines (→fetch-news, topic or top stories),
  volume up/down/mute, media play-pause/next/prev,
  brightness up/down,   screenshot, lock, close-window, dismiss/thanks (→overlay-hide), with "hey lain /
  please / could you" prefix stripping. Anything else goes to Ollama `/api/chat`
  (`BRAIN_MODEL` default `gemma4:31b-cloud` via the localhost proxy — no key
  handling needed in scripts, ~0.8s/reply. Token cap is per-model
  (gpt-oss reasons: 400, else 150 — lowering gpt-oss truncates replies).
  Falls back to local `gemma3:270m`, then to a spoken offline line. `BRAIN_URL`, `BRAIN_TIMEOUT` overrides, `num_predict`
  capped at 120 for voice-length replies).
- `bin/actions.sh` — ONE leading `[ACTION: name args]` per reply; speech
  text is computed AFTER the side effect (missing apps get an honest
  "couldn't find X" instead of a false "Opening…"). Actions:
  `notify|open|search|fetch-weather|fetch-news|volume|media|
  brightness|lock|screenshot|close`.
  `search` opens a DuckDuckGo query in `chromium --app=`. URL-shaped
  targets (contain `://` or a dot) open via
  `swaymsg exec "chromium --app=<url>"`; bare words launch as apps only if
  installed (binary, .desktop, or flatpak match). ALL spawns run in
  FOREGROUND (no `&`): the old caller ran us inside
  `$(...)`, and bash reaps backgrounded children when a command-substitution
  subshell exits — that silently murdered every window spawn. Callers must
  use the temp-file convention, never `SPEECH=$(... actions.sh)`.
  Side-effect stdout ALWAYS goes to stderr (the log): swaymsg's
  `[{"success": true}]` was once SPOKEN ALOUD via speech.txt.
  `HEY_LAIN_DRY_RUN=1` prints the parsed action without executing —
  mandatory for tests (never open windows, kill windows, or lock the
  screen as a "test"). A fake-`swaymsg`-on-PATH test verified the exact
  `chromium --app=` command construction.
- `bin/fetch-weather.sh <place>` — speakable current conditions + 3-day
  outlook ("Coming up: Saturday high…"). open-meteo PRIMARY (structured,
  geocode→forecast); wttr.in FALLBACK with STRICT validation (must contain
  a temperature like `83°F` and no "not found/unknown/error" — wttr returns
  chatty error pages, e.g. OpenCage failures, that were once SPOKEN ALOUD).
  Tiny towns (Mina/Cove AR) miss open-meteo geocoding and land on wttr
  (current only); garbage exits 1 → browser fallback. Query logged to
  stderr. Brain strips trailing day-ranges ("for the next three days") and
  leading articles before calling.
- `bin/fetch-news.sh [topic]` — top-4 Google News RSS headlines as speakable
  "1: … 2: …" text (" - Source" suffixes stripped, stdlib only). Empty
  topic = top stories. Nonzero exit → browser fallback.
- `bin/speak.py` (runs on the pipx `kokoro-cli` python, NOT the venv) —
  sentence-chunked streaming synthesis; notification+first-audio sync fix.
  Engine selected by `config.json: tts_engine` (`TTS_ENGINE` overrides):
  **piper** chain = tmpfs PCM cache → `piper-serve` (:8766) → piper CLI
  one-shot → kokoro service → kokoro local; **kokoro** chain skips piper.
  Cache keys include engine id. aplay stderr goes to the log; dead aplay
  restarts mid-stream instead of truncating. Per-chunk timings via
  `HEY_LAIN_LOG`. Measured on i5-4210M: piper cache HIT ~2.3s total,
  piper warm ~2.8x FASTER than realtime, piper CLI ~7s (model load),
  kokoro warm ~2x slower than realtime, cold int8 ~6.6x (one 3-chunk reply
  once took 253s). Backends serialize, so overlap comes from pipe
  backpressure only — keep replies SHORT.
- `bin/listen-overlay.sh show|hide|state NAME [text]` — nightshadeNeon
  `foot` overlay state machine: LISTENING → HEARD (transcript shown) →
  THINKING → ACTING → SPEAKING (Lain's reply stays visible as a
  conversation view; "tap alt+v to talk back", next tap resets to
  LISTENING). Writers drop `$RUNTIME/overlay-{state,text}` files; the foot
  loop polls them at ~8fps. Floating + sticky + `border pixel 2`,
  top-center. Launched with `setsid` (survives parent death); real foot
  PID re-resolved via `pgrep -f "app_id …"` because setsid forks (never
  trust `$!`). It must NEVER take focus (breaks key events).
  Frame loop never full-clears (that was the flicker): cursor-home redraw
  plus one clear on state transitions (geometry changes there).
  `record-stop` does NOT hide it; `hey-lain.sh` drives states. The
  auto-hide watcher defaults OFF (`OVERLAY_AUTOHIDE_SECS=0`): the user
  closes manually via voice ("dismiss"/"thanks" → overlay-hide action),
  next tap, or `bin/listen-overlay.sh hide`. show/hide/state all log to
  hey-lain.log with PIDs — check there first if the window vanishes.
- `bin/hey-lain.sh` — orchestrator. `flock -n` single-flight, EXIT/INT/TERM
  trap that deletes all session audio and hides the overlay UNLESS state is
  SPEAKING (conversation view persists after success). speak.py runs in the
  FOREGROUND (no exec) so the trap still works; lock stays held through
  speech (no barge-in: mic+speakers = echo in the next transcript).
  `HEY_LAIN_LOG` is exported for speak.py; stage timings (stop/stt/brain/
  speak seconds) are logged per run — read them before optimizing latency.
- `bin/warmup.sh` — starts `piper-serve` + `kokoro-serve`, pre-seeds the
  TTS cache with Lain's 12 most-spoken lines (`piper-seed-cache.sh`,
  ~6s via piper), warms STT+LLM (~30s background at login; add
  `exec …/bin/warmup.sh` to sway config). Brain sends `keep_alive=30m`
  so Ollama holds gemma between utterances. `record-stop` kill-waits are
  deliberately short (~1s).
- `SYSTEM_PROMPT.txt` — Lain's voice persona + action protocol (what the
  LLM reads). `config.json` — voice (`af_heart`), STT model, sample rates,
  theme note. `sway-bindings.conf` — the canonical one-line binding.
  `setup.sh` + `requirements.txt` — venv + `faster-whisper` install.
  `venv/` — STT venv. `log/hey-lain.log` — persistent timestamped debug
  log for every stage. **Read the log before theorizing.**

## Environment (all verified on this machine)

- Navi = Debian 13 (trixie), Wayland, sway IPC via `swaymsg`
  (`wiered` is sway-compatible). Output `eDP-1` 1366x768. PipeWire/Pulse
  (`pw-record`/`paplay`/`pactl` present), Intel HDA mic + analog out.
- Sway config is ROOT-OWNED: all keybind edits go through the user via
  `sudoedit ~/.config/sway/config` + `swaymsg reload`. Current binding:
  `bindsym --no-repeat Alt+v exec /home/rav3ndust/hey-lain/bin/toggle.sh`
  (`--no-repeat` is load-bearing — key-repeat flaps the toggle without it).
- `dunst` already matches nightshadeNeon (pink `#ff10f0` on dark).
  Palette: pink `#ff10f0`, green `#39ff14`, cyan `#00ffff`, red `#ff3131`,
  white `#ffffff`, black `#000000`.
- `chromium` at `/usr/bin/chromium` is the site browser (always `--app=`).
  `playerctl`, `brightnessctl`, `grim`, `swaylock`, `foot`, `wofi` present.
- Ollama at `127.0.0.1:11434`: `gemma3:270m` (voice brain, ~3s/reply),
  `glm-5.2:cloud`, `gemma4:31b-cloud`. `OLLAMA_API_KEY` is set.
- TTS: Piper `en_US-libritts_r-medium` (female, 22050Hz, `voices/`) by default
  via `bin/piper-serve.py` (:8766, resident voice — ~2.8x faster than
  realtime). Kokoro (`kokoro-cli 0.4.0`, `af_heart`, 24kHz, variant `full`:
  fp32 measured ~2x FASTER than int8 on pre-VNNI Intel) remains as
  fallback. NEVER run `kokoro speak` (writes wav files into its recordings
  dir) — use `bin/speak.py`, which streams raw PCM with zero files.
  `bin/kokoro-serve.sh` manages the warm Kokoro server (:8765).
  `feed()` slices PCM into ~100ms frames and publishes per-frame peak levels
  to `$RUNTIME/voice-level` (pipe backpressure paces the emits to the audio);
  the file is removed when playback ends.
- `bin/overlay-visualizer.sh` — the foot overlay renderer. Both directions of
  the conversation get REAL waveforms: LISTENING draws a scrolling green
  waveform from `$RUNTIME/mic-level` history, SPEAKING a pink one from
  `$RUNTIME/voice-level` history (48 columns x 3 rows, bottom-anchored `#`
  bars). Between them: HEARD = cyan static burst, THINKING = pink pulse,
  ACTING = green/pink shimmer sweep, WAITING = cyan breathing glow. Histories
  clear on state change; cursor-home redraw (no flicker), one full clear on
  transitions. ASCII-only by design (font safety).
- `opencode run` hangs/errors when shelled per utterance (no headless auth
  path here) — do NOT use it in the voice loop. Upgrade path for session
  memory: `opencode serve` + fixed session id as a future `BRAIN_URL`.

## Verified behaviors (tested, not assumed)

- Toggle tap→record (overlay ON, exactly one recorder, unique session file)
  →tap→stop+transcribe+brain+actions+speak, with zero leftover processes
  or audio files (`ps` + runtime-dir listing clean every run).
- Brain replies in ~3s; 12 properly-tagged intent phrasings tested
  (`open youtube` → `[ACTION: open https://www.youtube.com]`).
- Notification/voice sync: notify fires with first audio, no wavs created
  (kokoro recordings dir count unchanged across runs).
- Action side effects verified end-to-end (fake-`swaymsg` capture + live
  dunst delivery). Regression lesson: side-effect stdout must NEVER reach
  speech output — swaymsg's `[{"success": true}]` was once SPOKEN ALOUD.
  `hey-lain.sh` now also logs every brain REPLY for post-mortems.
- Failure modes degrade to speech: no audio → "No audio captured";
  empty transcript → "I didn't catch that"; LLM down → offline echo reply.

## Known limits / next steps

- No session memory yet (each utterance is stateless; `opencode serve`
  backend would fix this).
- Brain is cloud (gemma4:31b-cloud, needs network + Ollama Cloud access);
  local 270m fallback covers outages. STT+TTS stay on-device regardless.
- Overlay position is hardcoded for 1366x768; multi-monitor needs `$RUNTIME`
  geometry lookup.
- `setup.sh` apt-installs need sudo; venv + model prefetch already done.

## Working rules for agents

- COORDINATE TAPS: the user physically taps `Alt+V`. Your background taps
  and theirs interleave and corrupt each other's runs. Either THEY drive
  (you read the log) or YOU drive (they keep hands off keys). Say which.
- After every run: `ps` for strays (`pw-record`, overlay `foot`, `aplay`,
  your own test shells) and `ls` the runtime dir. Kill YOUR processes by
  PID. Never touch the user's long-lived `opencode` TUI.
- Timed-out tool calls can KEEP RUNNING in the persistent shell — always
  re-check `ps` after a timeout before starting the next test.
- Side-effecting actions (`open` real URLs, `close`, `lock`, `swaymsg kill`)
  are tested with `HEY_LAIN_DRY_RUN=1` or a fake `swaymsg` on PATH — never
  for real.
- First model hits (STT/TTS/LLM loads) take ~10s: generous timeouts for
  pipeline runs, short ones for state checks.
- Sway config is root-owned: propose exact lines for the user to paste,
  don't attempt to write it.

## Upgrade notes (September 2026)

- `install.sh` is the portable fresh-system installer. It installs the
  Debian runtime packages, creates `venv`, installs faster-whisper and Piper,
  prefetches `tiny.en`, and prints the exact Sway binding. It is safe to rerun;
  use `--skip-apt` when packages already exist.
- Piper remains the default TTS engine and uses the bundled
  `voices/en_US-libritts_r-medium.onnx`. Kokoro is optional fallback only.
  Interpreter paths are no longer tied to `/home/rav3ndust`.
- `bin/sway-control.py` is the allow-listed Sway tree/controller layer. New
  workspace, focus, move, floating, fullscreen, layout, scratchpad, and focus
  direction intents route through it. Test with `HEY_LAIN_DRY_RUN=1`.
- The foot overlay now uses a dedicated `bin/overlay-visualizer.sh` in the
  terminal alternate screen. It is a text-free nightshadeNeon visualizer with
  state-specific bars, pulse motifs, and concise directions. States include
  SPEAKING and WAITING; transcript/reply text is intentionally not rendered.
- Leading action tags may be chained for compound requests. Common STT app-name
  variants are normalized before launch, including neighborly/neighbor-li for
  neighborli, fire fox for firefox, and Google Chrome for chromium.
- See `NOTES.md` for the user-facing upgrade and installation notes.
