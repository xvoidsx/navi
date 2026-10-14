# Lain theme — nightshadeNeon tokens for Hermes

*Applied to the Hermes TUI wherever theming hooks exist. If upstream
theming is thin, the navi-lain launcher wraps output through these.*

## Palette

```json
{
  "neon_pink": "#ff10f0",
  "neon_green": "#39ff14",
  "cyan": "#00ffff",
  "red": "#ff3131",
  "purple": "#800080",
  "violet": "#bf5fff",
  "ghost": "#7a4a7a",
  "dark": "#0f0f0f",
  "dim": "#444444",
  "paper": "#f2e9f4",
  "muted": "#9a8fa0"
}
```

## Usage

- **Primary accent:** neon pink (#ff10f0) — prompts, highlights, the LAIN mark
- **Success/ok:** neon green (#39ff14) — confirmations, active states
- **Info:** cyan (#00ffff) — secondary info, links
- **Warnings:** red (#ff3131) — errors, destructive confirmations
- **Ambient:** violet/ghost — the Lain ticker moods (glitch/static)
- **Background:** dark (#0f0f0f) — never pure black, always deep
- **Text:** paper (#f2e9f4) primary, muted (#9a8fa0) secondary

## Rules

- No pure black (#000000) or pure white (#ffffff) — everything lives
  in the neon-on-dark range.
- The ∅ mark appears in the header, never as decoration elsewhere.
- Animations are ambient-only: one reserved line, bounded, cosmetic.
  Nothing that changes layout or blocks input.
