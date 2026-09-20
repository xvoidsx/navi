# navi mods

<img width="880" height="599" alt="image" src="https://github.com/user-attachments/assets/d166b925-ab34-4a44-966e-cad2ab05a283" />

###### the `navi-networks` mod running a speed test

**navi mods** are modules that live in the system's panel, whether it is `waybar` (for navi's Wayland session), or `polybar` (for navi's X session).

### the mods

We include 3 default **navi mods**:

- `navi-networks`: A utility allowing you to connect to networks, check your internet speed, set DNS, and share the network with a QR code.
- `navi-calendar`: A utility allowing you to view the calendar.
- `navi-audio`: A utility allowing you to control the volume of attached devices such as speakers, output sources, and more.
- `navi-weather`: A utility for checking the weather — current conditions, the next 12 hours, and a 3-day forecast, with saved locations and an imperial/metric toggle (wttr.in backend, no API key).
- `navi-lain-config`: Hey Lain brain settings — pick a local or cloud model, manage the key for the chosen provider.
- `navi-agents-config`: **Navi Agent Configuration** — the system-wide API key manager for every AI provider navi knows about. Keys live in `~/.config/navi/agents.env` (mode `0600`, never logged); interactive shells export them via `wired/bashrc`, panel/rofi-launched apps inherit them through `wired/waybar/mod-open.sh`, and Hey Lain resolves them per-backend at runtime (local needs no key). The provider table is shared from `mods/agentenv` so every mod agrees on which variable each provider uses.

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
