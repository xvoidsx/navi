# navi mods

**navi Mods** are customizable modules that can enhance the Wired desktop in the **navi** Linux operating system.

This directory contains the standalone terminal mods that make up the
interactive utility layer of navi.

The mods are designed to work identically from both the X11 and Wayland
versions of navi. They are standalone applications rendered with Bubble Tea
and styled with Lip Gloss using the nightshadeNeon design language.

The compositor/window manager is responsible for launching and positioning
the widget. The widget itself must not contain compositor-specific logic.

---

## Philosophy

navi-mods are small, focused applications rather than shell-script
collections.

Each widget should:

- Do one thing well.
- Have a beautiful, minimal TUI.
- Use Bubble Tea for interaction and rendering.
- Use Lip Gloss for styling.
- Follow the nightshadeNeon visual language.
- Be usable entirely from the keyboard.
- Work independently from i3, wiredWM, sway, or any other compositor.
- Be usable directly from a terminal for development and debugging.
- Be installable as a standalone compiled binary.
- Avoid requiring the Go toolchain on an end user's machine.

The goal is for navi mods to feel like native pieces of one coherent
environment.

The `∅` logo and nightshadeNeon styling should be used consistently throughout
the widget family.

Ambient personality is allowed when it does not steal focus from the task.
See "Ambient transmissions (Lain layer)" under `navi-networking`.

---

## Architecture

The general architecture is:

    navi bar
        |
        v
    terminal emulator
        |
        v
    navi-widget
        |
        v
    system/service backend

For example:

    Waybar / Polybar
        |
        v
    Alacritty
        |
        v
    navi-networking
        |
        v
    NetworkManager

The widget should not know whether it was launched from X11 or Wayland.

Window-management behavior belongs outside the widget.

For example, `navi-networking` should simply be an executable:

    navi-networking

The X11/i3 and Wayland/wiredWM configurations can then launch it in an
Alacritty window with the appropriate class/title and floating behavior.

This separation is intentional.

---

## Current Widget

### navi-networking

`navi-networking` is the first navi widget.

Its purpose is to replace the existing `nmtui` bar action with a purpose-built
navi interface.

#### Implemented functionality

- Scan for Wi-Fi networks (`nmcli device wifi list --rescan yes`).
- Display SSID, signal bars, security type, and the in-use network.
- Deduplicate SSIDs by keeping the strongest BSSID; hidden SSIDs render as
  `<hidden>`.
- Terse-field parser that respects nmcli backslash escapes so SSIDs containing
  `:` do not split incorrectly.
- Connect to secured networks with a masked password prompt.
- Connect to open networks only after an explicit confirmation screen.
- Disconnect the active device.
- Forget a saved NetworkManager profile (UUID preferred, profile name fallback).
- Dashboard for the active connection (SSID, signal, security, device, BSSID).
- Connection details screen (IPv4, gateway, DNS v4/v6).
- DNS presets: ISP / automatic, Cloudflare, Google, plus a custom IPv4/IPv6
  editor. Apply path is `connection modify` then `connection up`.
- Share the current network as a WIFI: QR payload rendered in-terminal
  (`qrterminal`, half-blocks). Secrets are read only when the user opens Share.
- Cloudflare-backed speed test (latency HEAD rounds, 50 MB download, 20 MB
  upload) with a progress bar driven by atomic counters + a tick cmd.
- Persistent two-line keymap. Connection-only actions dim when there is no
  active link instead of disappearing, so the footer never shifts.
- Esc while a background op is in flight cancels the context instead of
  quitting. Ctrl+C always quits.
- Reserved header row for ambient Lain transmissions (see below). The row is
  always allocated so the frame height does not jump.

Planned / not yet done:

- Replace the `nmcli` shell-out layer with NetworkManager D-Bus behind the
  same UI model.
- Extract styles and chrome into a shared navi-mods package once a second
  widget exists.

The widget uses NetworkManager as its system networking backend.

The current implementation uses `nmcli` for speed and simplicity.

The backend should be kept behind an internal abstraction so that it can
eventually be moved to NetworkManager's D-Bus API without requiring a rewrite
of the Bubble Tea UI. Today that abstraction is still informal: command
helpers live in the same file as the model. Split them before adding a second
backend.

Runtime check: `main` exits 1 with a short message if `nmcli` is not on PATH.

#### Ambient transmissions (Lain layer)

The header under `∅ navi networking` carries a single line of short
Serial Experiments Lain–toned phrases. This is personality, not a control
surface. It must never wrap, never steal keys, and never change frame height.

Behavior as of the current source:

- First phrase arrives ~900 ms after init.
- A resolved phrase stays visible until the next one glitches in. There is
  no empty gap between transmissions.
- Transition is a ~550 ms, 10-frame mix (`lainGlitchSteps`) at 55 ms:
  heavy block static → case scramble → thin static resolve.
- Mix source is the previous clean phrase (spaces on the first pop-in), so
  the line crossfades rather than clearing.
- After resolve, the phrase holds 5–11 seconds (`lainHoldMin` + random extra).
- While holding, a low-rate flicker cmd (1.4–4 s) may twitch the sitting
  text (light static / case flip) and snap it back. Flicker must not start
  a new phrase.
- Mood colors on the same reserved row:
  - clean: neon pink italic
  - glitch: violet italic
  - static: ghost purple italic
  - resolve: cyan italic
- Phrase pool lives in `lainPhrases`. A smaller `lainRare` pool is picked
  about 1/9 of the time. The picker refuses the same string twice in a row.
- Keep every phrase under ~50 characters so it fits `frameWidth` (62).
- `lainRand` is a dedicated `math/rand` source, not the global one, so
  timing does not depend on Go auto-seeding changes.
- The Lain ticker is independent of scan/connect/speed-test cmds. Do not
  cancel it when an nmcli op is cancelled.

When editing this layer:

- Do not hide the row between phrases. That was the original bug.
- Do not put Lain text in the footer or mix it with error/status lines.
- Do not add mouse/click handlers or make phrases actionable.
- If flicker feels noisy, lengthen `lainFlickerCmd` or lower the twitch
  probability. If transitions feel slow, drop `lainGlitchSteps` toward 6–7.
- New phrases belong in the two slices only. Do not generate them at
  runtime from network state; the layer should stay fictional and calm.

This is an intentional exception to "avoid unnecessary animations." The
animation is one reserved line, bounded, and cosmetic.

---

## Go Dependencies

mods are written in Go.

Typical dependencies include:

- Bubble Tea
- Lip Gloss
- Bubbles (spinner, progress)
- qrterminal/v3 (`navi-networking` share screen)

These are build-time dependencies.

They are compiled into the resulting widget binary.

An installed navi machine does NOT need:

- Go
- the Go compiler
- the Bubble Tea source
- the Lip Gloss source
- the Bubbles source
- the widget's Go module cache

The production navi installation should install the compiled widget binary.

---

## Development

A widget should be independently buildable and testable.

For example:

    cd navi-mods/navi-networking

    go mod tidy
    go build -o navi-networking .
    ./navi-networking

During development, `go run .` is also acceptable:

    go run .

Development machines therefore need the Go toolchain.

Production machines do not.

Notes specific to `navi-networking`:

- Needs a working NetworkManager + `nmcli` to do anything useful. Without
  Wi-Fi hardware the scan list will simply be empty; the TUI should still
  launch.
- Alt-screen is on (`tea.WithAltScreen()`). A 50 ms sleep after `Run`
  exists to let the terminal restore cleanly.
- Frame width is a constant (`frameWidth = 62`). Views assume that width.
  Do not let Lain phrases or status strings wrap inside it.
- `splitTerse` is load-bearing. Do not replace it with `strings.Split(line, ":")`.

---

## Building mods

The navi build process should eventually provide a centralized mechanism for
building all mods.

Conceptually:

    ./build-mods

should build every widget in this directory and produce release artifacts
such as:

    build/
        navi-networking
        navi-calendar
        navi-volume
        ...

Individual mods must remain independently buildable, however.

Do not create a system where building one widget requires another widget to
exist.

The eventual build system may use Make, a shell script, or another suitable
mechanism. Keep the build process simple and transparent.

---

## Production Installation

The navi installer must distinguish between BUILD dependencies and RUNTIME
dependencies.

### Build dependencies

These are required when producing the widget binary:

- Go
- Go modules
- Bubble Tea
- Lip Gloss
- Bubbles
- Other Go dependencies declared by the widget's `go.mod`

These should normally exist on the navi build/CI environment, not on every
installed navi machine.

### Runtime dependencies

These are the programs/services required by the compiled widget.

For `navi-networking`, this currently includes:

- NetworkManager
- `nmcli`
- Alacritty (desktop launch path; not required for `./navi-networking` in a
  raw terminal)
- outbound HTTPS to `speed.cloudflare.com` only if the user runs a speed test

The navi installation process must ensure that required runtime dependencies
are installed and configured.

A fresh navi installation must never produce a clickable navi networking
button that points to a missing executable or missing system service.

The installer should therefore explicitly install NetworkManager when
`navi-networking` is part of the installation.

---

## Important: Do Not Build mods During Normal Installation

A normal navi installation should NOT do this:

    go get ...
    go build ...
    go mod tidy ...

The end user's machine should receive a pre-built widget.

The intended flow is:

    source code
        |
        v
    navi build / CI
        |
        v
    compiled widget
        |
        v
    navi ISO / package / release artifact
        |
        v
    navi installer
        |
        +--> install widget binary
        |
        +--> install runtime dependencies
        |
        +--> configure required services
        v
    working navi installation

The installer should be fast and deterministic.

---

## Packaging

As the navi release system develops, mods should become normal navi
artifacts/packages.

A future navi release might contain:

    /usr/bin/navi-networking
    /usr/bin/navi-calendar
    /usr/bin/navi-volume
    ...

Exact packaging mechanics are intentionally left to the navi installer/build
work.

Do not prematurely introduce a complicated packaging system unless the
existing navi infrastructure requires it.

---

## System Integration

mods should not directly modify:

- i3 configuration
- wiredWM configuration
- Waybar configuration
- Polybar configuration
- compositor state

unless a widget's explicit purpose requires it.

Instead, integration should happen from the navi configuration layer.

For example:

    Waybar network click
        -> launch navi-networking

and:

    Polybar network click
        -> launch navi-networking

The executable remains identical.

This allows one widget implementation to serve both the X11 and Wayland
editions of navi.

---

## Terminal Window Contract

mods are intended to run inside Alacritty during normal desktop use.

A typical invocation is:

    alacritty \
        --class "navi-networking" \
        --title "navi networking" \
        -e navi-networking

The exact window class and floating behavior belong to the window manager
configuration.

The application should primarily care about its terminal dimensions and
render accordingly.

Do not add i3, sway, Wayland, or X11 dependencies to the widget merely to
control its window.

`navi-networking` uses `tea.WindowSizeMsg` only to center the fixed-width
frame. It does not reflow to arbitrary widths.

---

## UI Guidelines

mods should feel like parts of navi, not generic Bubble Tea examples.

Use the nightshadeNeon palette:

    neon pink   #ff10f0
    neon green  #39ff14
    cyan        #00ffff
    red         #ff3131
    purple      #800080
    violet      #bf5fff    (Lain glitch mood; networking only for now)
    ghost       #7a4a7a    (Lain static mood)
    dark        #0f0f0f
    dim         #444444

Use:

    ∅

for the navi logo.

Prefer:

- strong hierarchy
- generous spacing
- concise labels
- keyboard hints that never rearrange
- subtle borders
- clear selection states
- minimal decoration
- useful status messages
- fast startup

Avoid:

- animations that change layout or block input
- excessive panels
- clutter
- giant ASCII art
- needless configuration screens
- reproducing existing tools simply for the sake of being different

The widget should feel polished without feeling heavy.

The Lain header is the only standing animation in `navi-networking`. Keep
it on one reserved line.

---

## Shared Design System

As the widget family grows, common Bubble Tea/Lip Gloss components should be
centralized where practical.

Potential shared components include:

- navi title/header
- section headers
- selection rows
- status indicators
- key-hint footer
- confirmation dialogs
- error messages
- spinners
- QR-code frames
- common nightshadeNeon styles

Do not copy increasingly large blocks of styling code between mods.

However, avoid creating a huge framework before there are enough mods to
justify one.

Start simple and extract genuinely shared components as repetition appears.

The Lain ticker, phrase lists, and glitch helpers are specific to
`navi-networking`. Do not promote them into a shared package unless another
widget actually wants the same ambient line.

---

## Backend Separation

UI code should not contain large amounts of system-command logic.

Prefer:

    Bubble Tea model
          |
          v
    internal service/backend
          |
          v
    system API / command

For example:

    UI
      |
      v
    network.Manager
      |
      v
    nmcli / NetworkManager

This keeps the UI understandable and allows system backends to evolve
independently.

`navi-networking` currently keeps nmcli helpers, speed-test HTTP, and the
Bubble Tea model in one file. That is acceptable while it is the only
widget, but any D-Bus port should start by pulling the nmcli functions into
their own package without changing message types.

---

## Security

mods that handle credentials must be careful with secrets.

Do not:

- print passwords to stdout/stderr
- write passwords to debug logs
- store passwords unnecessarily
- expose passwords in error messages
- commit credentials to source control

The password view renders bullets, never the typed runes.

Wi-Fi QR sharing should only expose credentials when the user explicitly
chooses the sharing action. `getPassword` uses `nmcli --show-secrets` and
only runs from the Share path.

Speed-test traffic is bulk dummy data to Cloudflare. It does not include
Wi-Fi secrets.

---

## Testing

Every widget should be testable directly from a terminal before integration
with navi's bars.

The development workflow should therefore be:

    build widget
        |
        v
    run widget directly
        |
        v
    test functionality
        |
        v
    polish UI
        |
        v
    integrate with Wayland/X11 bars
        |
        v
    test inside navi

Do not begin with bar/compositor integration.

A widget that works correctly when launched manually is much easier to
integrate later.

For the Lain layer, watch:

- header height stays constant as phrases change
- a phrase remains readable until the next mix starts
- flicker does not blank the line
- long phrases do not wrap inside the border
- cancelling a scan/connect does not kill the ticker

---

## Adding a New Widget

When creating a new widget:

1. Create its own directory.
2. Give it its own `go.mod` unless the navi build system later establishes a
   shared Go workspace.
3. Make it independently buildable.
4. Make it independently executable.
5. Keep system/backend code separate from UI code.
6. Follow the nightshadeNeon design language.
7. Test it directly in a terminal.
8. Document runtime dependencies.
9. Add it to the navi widget build process.
10. Add it to the navi installer.
11. Only then integrate it with Waybar, Polybar, wiredWM, i3, or other navi
    components.

---

## Current Principle

navi-mods are the small pieces of software that make navi feel like navi.

They should be:

    small
    beautiful
    fast
    keyboard-first
    self-contained
    compositor-agnostic
    open source
    nightshadeNeon

Build the application first.

Integrate it with the desktop second.

Package it for navi third.

Keep that order unless there is a compelling reason to do otherwise.
