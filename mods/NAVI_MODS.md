# navi mods

<img width="880" height="599" alt="image" src="https://github.com/user-attachments/assets/d166b925-ab34-4a44-966e-cad2ab05a283" />

###### the `navi-networks` mod running a speed test

**navi mods** are modules that live in the system's panel, whether it is `waybar` (for navi's Wayland session), or `polybar` (for navi's X session).

### the mods

We include 3 default **navi mods**:

- `navi-networks`: A utility allowing you to connect to networks, check your internet speed, set DNS, and share the network with a QR code.
- `navi-calendar`: A utility allowing you to view the calendar.
- `navi-audio`: A utility allowing you to control the volume of attached devices such as speakers, output sources, and more.
- `navi-bluetooth`: A Bluetooth manager replacing blueman-applet — pair, trust, connect, disconnect, and forget devices; adapter power, discoverability, and scan toggles; headset battery via `bluetoothctl info` with a upower fallback.
- `navi-weather`: A utility for checking the weather — current conditions, the next 12 hours, and a 3-day forecast, with saved locations and an imperial/metric toggle (wttr.in backend, no API key).
- `navi-power`: Power profiles (power-profiles-daemon), honest battery readouts (sysfs + upower time estimates), and ThinkPad charge thresholds — the threshold UI only appears where the kernel exposes it.
- `navi-display`: Monitor layout without the GUI settings app — arrange, enable, rotate, scale, and set modes on every connected output (sway and i3 backends), plus a night-light schedule (wlsunset/gammastep).
- `navi-reminders`: Reminders that fire reliably — natural-language add (`dentist tomorrow 9am`), snooze, done. Backed by systemd user timers (`Persistent=true`, linger at deploy) with a `navi-reminder-fire` helper; undeliverable reminders surface as MISSED, never silently dropped.
- `navi-lain-config`: Hey Lain brain settings — pick a local or cloud model, manage the key for the chosen provider.
- `navi-agents-config`: **Navi Agent Configuration** — the system-wide API key manager for every AI provider navi knows about. Keys live in `~/.config/navi/agents.env` (mode `0600`, never logged); interactive shells export them via `wired/bashrc`, panel/rofi-launched apps inherit them through `wired/waybar/mod-open.sh`, and Hey Lain resolves them per-backend at runtime (local needs no key). The provider table is shared from `mods/agentenv` so every mod agrees on which variable each provider uses.
- `wiredrop`: xvoidsx's own LocalSend — drop files to nearby devices over LAN or Tailscale, speaking the LocalSend v2.2 protocol both ways (official phone apps interoperate). Runs as a per-user systemd service (`wiredrop daemon`); the floating TUI shows nearby devices, a six-word fingerprint ceremony pins each new device (TOFU — a changed fingerprint is a hard refusal), and incoming transfers ask via dunst with a 60-second default-decline (or `wiredrop ctl accept|decline <session>` over SSH). HTTPS only, loopback/RFC1918/tailnet binds only.

### agent keys

API keys are configured **once**, not per-app. The canonical store is `~/.config/navi/agents.env`, a `KEY=VALUE` file at mode `0600` written only by Navi Agent Configuration (or `i` to import from your current environment). The known providers and their variables:

| provider | env var |
|---|---|
| OpenAI | `OPENAI_API_KEY` |
| Anthropic | `ANTHROPIC_API_KEY` |
| OpenRouter | `OPENROUTER_API_KEY` |
| Ollama Cloud | `OLLAMA_API_KEY` |
| Google Gemini | `GEMINI_API_KEY` |
| xAI | `XAI_API_KEY` |
| DeepSeek | `DEEPSEEK_API_KEY` |
| Mistral | `MISTRAL_API_KEY` |

Custom providers can be added from the app (stored in `~/.config/navi/agents-providers.json`). Hey Lain currently speaks Ollama-style and OpenAI-compatible protocols — storing a key does not by itself teach it a new protocol.

### mods design

**navi mods** are written in Go using [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss) for making a beautiful TUI.

### wiring

**navi mods** live in `mods/widgets/` and are wired up to the window manager using launcher scripts (for example: `/usr/bin/navi-networks`.

The windows are rendered in Alacritty, and the theme is drenched in our [nightshadeNeon](https://rav3ndust.xyz/wiki/nightshadeNeon.html) theme, just like the rest of **navi**.

### future mods

We plan on building scaffolding for other people to easily be able to build and apply their own **navi mods** in the future.
