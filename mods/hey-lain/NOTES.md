# Hey Lain upgrade notes

## Current defaults

- Piper is the default and fastest TTS engine.
- The bundled default voice is `voices/en_US-libritts_r-medium.onnx`.
- Kokoro is retained as an optional fallback; it is not required for the normal install.
- The default Sway entry point remains `bin/toggle.sh`.

## New capabilities

`bin/sway-control.py` is an allow-listed Sway controller. It resolves a
focused window, app ID, class, instance, or title through Sway's tree before
focusing, moving, closing, floating, fullscreening, changing layout, or using
the scratchpad. Use `HEY_LAIN_DRY_RUN=1` for action tests.

The deterministic voice router understands workspace changes, moving a window
to a workspace, fullscreen/floating mode, scratchpad, layout, and focus
direction. The LLM is still used for general conversation, but it never gets
to execute arbitrary shell text.

The overlay remains a focus-safe `foot` window for compatibility, but is now a
text-free visualizer. `bin/overlay-visualizer.sh` renders state-specific neon
meters, pulse motifs, and compact directions. Listening, thinking, acting,
speaking, and waiting each have their own visual rhythm.
- After playback, the overlay stays visible as `WAITING` and invites the next
  tap instead of looking frozen in `SPEAKING`.
- Leading action tags can be chained. App aliases cover common STT mistakes,
  including `neighborly`, `neighbor-li`, and `fire fox`.
- General compound requests are split into ordered actions. This works for
  arbitrary installed desktop entries and combinations such as opening an app,
  changing workspace, adjusting volume, or changing brightness in one utterance.
- Deterministic action acknowledgements are now input-seeded and varied, while
  ordinary conversation and ambiguous requests continue through Ollama using
  the persona in `SYSTEM_PROMPT.txt`.

## Installation

On a fresh Debian/Sway machine:

```sh
./install.sh
./bin/warmup.sh
```

Use `./install.sh --skip-apt` when system packages are already installed. The
installer is safe to rerun and prints the exact Sway binding to add. For a
root-owned Sway config, add it with `sudoedit` and then run `swaymsg reload`.

## Testing without side effects

```sh
HEY_LAIN_DRY_RUN=1 bin/brain.sh "move firefox to workspace three"
HEY_LAIN_DRY_RUN=1 bin/actions.sh <<'EOF'
[ACTION: sway-move firefox|3]
Moving Firefox.
EOF
```

Do not test `close`, `lock`, real browser opens, or live key taps while the
user is using the desktop. Check `log/hey-lain.log` and the runtime directory
after integration runs.
