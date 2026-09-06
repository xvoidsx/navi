# navi mods

<img width="880" height="599" alt="image" src="https://github.com/user-attachments/assets/d166b925-ab34-4a44-966e-cad2ab05a283" />

###### the `navi-networks` mod running a speed test

**navi mods** are modules that live in the system's waybar, whether it is `waybar` (for navi's Wayland session), or `polybar` (for navi's X session).

### the mods

We include 3 default **navi mods**:

- `navi-networks`: A utility allowing you to connect to networks, check your internet speed, set DNS, and share the network with a QR code.
- `navi-calendar`: A utility allowing you to view the calendar.
- `navi-audio`: A utility allowing you to control the volume of attached devices such as speakers, output sources, and more.

### mods design

**navi mods** are written in Go using [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss) for making a beautiful TUI.

The windows are rendered in Alacritty, and the theme is drenched in our [nightshadeNeon](https://rav3ndust.xyz/wiki/nightshadeNeon.html) theme, just like the rest of **navi**.

### future mods

We plan on building scaffolding for other people to easily be able to build and apply their own **navi mods** in the future.
