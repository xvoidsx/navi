# ╔══════════════════════════════════════════════════════════════╗
# ║  navi-qute — qutebrowser config for the wired                ║
# ║  nightshadeNeon theme · keyboard-driven · opinionated        ║
# ╚══════════════════════════════════════════════════════════════╝

# Must be first: don't load GUI-configured settings
config.load_autoconfig(False)

# ── nightshadeNeon palette ──
PINK = "#ff2d95"
PINK_DIM = "#8f1a56"   # hairline borders; bright pink reads as a divider line
CYAN = "#00ffff"
RED = "#ff3131"
PURPLE = "#b967ff"
GREEN = "#39ff14"
GREEN_CALM = "#8fbc8f" # desaturated green for insert mode; see note below
INK = "#06140a"        # text on the calm green — deeper than BG for contrast
BG = "#0d0d14"
FG = "#e0e0e0"
MUTED = "#888888"

# ── Dark mode EVERYWHERE ──
c.colors.webpage.darkmode.enabled = True
c.colors.webpage.darkmode.policy.images = "never"
c.colors.webpage.darkmode.policy.page = "always"
c.colors.webpage.preferred_color_scheme = "dark"
# page background before first paint — no white flash, ever
c.colors.webpage.bg = BG

# ── User agent ─────────────────────────────────────────────────────────────
# qutebrowser identifies itself as "...QtWebEngine/x.y.z ..." and several
# sites reject that outright. Present as Chrome stable instead.
#
# Track this against navi's chromium package: apt-cache policy chromium, or
# grep Version from `dpkg -l chromium`. Chrome's UA string does not carry a
# real minor version, so bumping only the major is correct.
config.set(
    'content.headers.user_agent',
    'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/155.0.0.0 Safari/537.36',
    'accounts.google.com',
)

# ── Appearance ──
c.colors.completion.fg = FG
c.colors.completion.odd.bg = BG
c.colors.completion.even.bg = BG
c.colors.completion.category.fg = PINK
c.colors.completion.category.bg = BG
c.colors.completion.item.selected.fg = BG
c.colors.completion.item.selected.bg = PINK
c.colors.completion.match.fg = CYAN

c.colors.statusbar.normal.fg = FG
c.colors.statusbar.normal.bg = BG
# Insert mode is calmer than the rest of the palette on purpose. Full neon
# green behind dark text is legible but glaring, and the indicators pile up
# on that one segment, so it uses a desaturated green instead. The keyhint
# widget (c.colors.keyhint.bg below) stays full neon — you read it
# deliberately, not constantly.
c.colors.statusbar.insert.fg = INK
c.colors.statusbar.insert.bg = GREEN_CALM
c.colors.statusbar.command.fg = CYAN
c.colors.statusbar.command.bg = BG
c.colors.statusbar.url.fg = FG
c.colors.statusbar.url.success.https.fg = GREEN

c.colors.tabs.bar.bg = BG
c.colors.tabs.odd.fg = MUTED
c.colors.tabs.odd.bg = BG
c.colors.tabs.even.fg = MUTED
c.colors.tabs.even.bg = BG
c.colors.tabs.selected.odd.fg = BG
c.colors.tabs.selected.odd.bg = PINK
c.colors.tabs.selected.even.fg = BG
c.colors.tabs.selected.even.bg = PINK
c.colors.tabs.indicator.start = PINK
c.colors.tabs.indicator.stop = CYAN

# Neon edges. qutebrowser 3.4.0 only exposes eight border colour keys, and
# none of them sit on the tab bar or statusbar — those are the two places a
# hairline would help most, so there is no way to draw one here. What is
# available: prompts, messages and completion rows.
c.colors.prompts.border = PINK_DIM
c.colors.messages.info.border = CYAN
c.colors.messages.warning.border = "#ffaa00"
c.colors.messages.error.border = RED
c.colors.completion.item.selected.border.bottom = PINK
c.colors.completion.category.border.bottom = PINK_DIM

c.colors.hints.fg = BG
c.colors.hints.bg = PINK
c.colors.hints.match.fg = CYAN

c.colors.downloads.bar.bg = BG
c.colors.downloads.start.fg = BG
c.colors.downloads.start.bg = CYAN

# Context menu, prompts, etc. — dark
c.colors.contextmenu.menu.bg = BG
c.colors.contextmenu.menu.fg = FG
c.colors.contextmenu.selected.bg = PINK
c.colors.contextmenu.selected.fg = BG
c.colors.prompts.bg = BG
c.colors.prompts.fg = FG
c.colors.prompts.selected.bg = PINK

# Fonts — navi system typefaces
c.fonts.default_family = "Noto Sans"
c.fonts.default_size = "10pt"
c.fonts.web.family.standard = "Noto Sans"
c.fonts.web.family.fixed = "JetBrains Mono"
# chrome UI in the terminal typeface — the full neon-terminal feel
c.fonts.tabs.selected = "10pt JetBrains Mono"
c.fonts.tabs.unselected = "10pt JetBrains Mono"
c.fonts.statusbar = "10pt JetBrains Mono"

# ── Behavior ──
c.tabs.show = "multiple"
c.tabs.position = "top"
c.tabs.title.format = "{audio}{current_title}"
c.statusbar.show = "always"
c.completion.shrink = True

# External editor for textareas: navi's default terminal + editor
c.editor.command = ["alacritty", "-e", "nvim", "{file}"]

# Start page (auto-located — startpage.html ships beside this file)
import os
_qute_dir = os.path.dirname(os.path.abspath(__file__))
_startpage = os.path.join(_qute_dir, "startpage.html")
c.url.start_pages = [f"file://{_startpage}"]
c.url.default_page = f"file://{_startpage}"

# Adblocking: python-adblock when present, hosts lists as fallback
# (for the full engine: pip install --break-system-packages adblock)
try:
    import adblock  # noqa: F401
    c.content.blocking.method = "adblock"
except ImportError:
    c.content.blocking.method = "hosts"

# Custom EasyList/EasyPrivacy URLs
c.content.blocking.hosts.lists = [
    "https://easylist.to/easylist/easylist.txt",
    "https://easylist.to/easylist/easyprivacy.txt",
]

# ── navi bangs ──
c.url.searchengines = {
    "DEFAULT": "https://search.brave.com/search?q={}",
    "!yt": "https://www.youtube.com/results?search_query={}",
    "!gh": "https://github.com/search?q={}",
    "!w": "https://en.wikipedia.org/wiki/Special:Search?search={}",
    "!so": "https://stackoverflow.com/search?q={}",
    "!mdn": "https://developer.mozilla.org/en-US/search?q={}",
    "!npm": "https://www.npmjs.com/search?q={}",
    "!pypi": "https://pypi.org/search/?q={}",
    "!arch": "https://wiki.archlinux.org/index.php?search={}",
    "!g": "https://www.google.com/search?q={}",
    "!ddg": "https://duckduckgo.com/?q={}",
    "!sp": "https://startpage.com/sp/search?query={}",
    "!r": "https://www.reddit.com/search/?q={}",
    "!hn": "https://hn.algolia.com/?q={}",
    "!radio": "https://xvoidsx.github.io/navi-radio/?q={}",
    "!nl": "https://neighborli.xyz/search?q={}",
    "!apps": "file:///usr/share/navi/wired/naviApps/index.html?q={}",
    "!wired": "https://navi.xvoidsx.org/?q={}",
}

# ── Full chrome theming (nightshadeNeon everywhere) ──
# Completion popup
c.colors.completion.scrollbar.fg = PINK
c.colors.completion.scrollbar.bg = BG

# Downloads
c.colors.downloads.system.fg = "none"
c.colors.downloads.system.bg = "none"

# Keyhint widget
c.colors.keyhint.fg = FG
c.colors.keyhint.suffix.fg = CYAN
c.colors.keyhint.bg = BG

# Messages (info/warning/error)
c.colors.messages.info.fg = FG
c.colors.messages.info.bg = BG
c.colors.messages.info.border = CYAN
c.colors.messages.warning.fg = BG
c.colors.messages.warning.bg = "#ffaa00"
c.colors.messages.warning.border = "#ffaa00"
c.colors.messages.error.fg = "#ffffff"
c.colors.messages.error.bg = PINK
c.colors.messages.error.border = PINK

# Statusbar
c.colors.statusbar.passthrough.fg = BG
c.colors.statusbar.passthrough.bg = CYAN
c.colors.statusbar.private.fg = FG
c.colors.statusbar.private.bg = PURPLE
c.colors.statusbar.progress.bg = PINK
c.colors.statusbar.url.warn.fg = "#ffaa00"

# Tabs
c.colors.tabs.pinned.selected.odd.fg = BG
c.colors.tabs.pinned.selected.odd.bg = CYAN
c.colors.tabs.pinned.selected.even.fg = BG
c.colors.tabs.pinned.selected.even.bg = CYAN
c.colors.tabs.pinned.odd.fg = CYAN
c.colors.tabs.pinned.odd.bg = BG
c.colors.tabs.pinned.even.fg = CYAN
c.colors.tabs.pinned.even.bg = BG

# Tab padding and indicator
c.tabs.padding = {"top": 6, "bottom": 6, "left": 10, "right": 10}
c.tabs.indicator.width = 3
c.tabs.favicons.show = "always"

# Statusbar padding
c.statusbar.padding = {"top": 4, "bottom": 4, "left": 8, "right": 8}

# Hints styling
c.hints.chars = "asdfghjkl"
c.hints.uppercase = True
c.fonts.hints = "bold 11pt JetBrains Mono"

# Tab behaviour — Chrome-like: elide the middle of long titles and show a
# tooltip with the full URL.
c.tabs.tooltips = True
c.tabs.title.elide = "middle"
c.tabs.width = 180

# Completion popup — readable timestamps in the history column.
c.completion.timestamp_format = "%Y-%m-%d %H:%M"

# ── Chrome opacity ─────────────────────────────────────────────────────────
# Fully opaque nightshadeNeon. Transparency was tried and rejected: without
# blur behind it, transparent bars just let the wallpaper through raw, and
# Sway has no compositor-side blur. If a blurring compositor ever comes
# along, c.window.transparent plus "transparent" bar colours is the switch
# to flip — colours take "transparent" or hex, but not rgba, so there is no
# partial alpha either way.
TRANSPARENT = False

if TRANSPARENT:
    c.window.transparent = True
    c.colors.statusbar.normal.bg = "transparent"
    c.colors.statusbar.command.bg = "transparent"
    c.colors.tabs.bar.bg = "transparent"
    c.colors.tabs.odd.bg = "transparent"
    c.colors.tabs.even.bg = "transparent"
    # Keep chrome legible over an unknown wallpaper.
    c.colors.statusbar.normal.fg = FG
    c.colors.tabs.odd.fg = FG
    c.colors.tabs.even.fg = FG
    c.colors.tabs.selected.odd.bg = PINK
    c.colors.tabs.selected.even.bg = PINK

# ── Keybindings ────────────────────────────────────────────────────────────
config.bind(",n", "open https://neighborli.xyz")
config.bind(",r", "open https://xvoidsx.github.io/navi-radio/")
config.bind(",g", "open https://glyyph.app")
config.bind(",x", "open https://xvoidsx.org")
config.bind(",a", "open file:///usr/share/navi/wired/naviApps/index.html")
config.bind(",d", "config-cycle colors.webpage.darkmode.enabled true false")

# navi pages on the wired
config.bind(",m", "open https://rav3ndust.xyz/wiki/x3nyth.html")
config.bind(",1", "open https://rav3ndust.xyz/now.html")
config.bind(",2", "open https://rav3ndust.xyz/blog.html")
config.bind(",3", "open https://rav3ndust.xyz/wiki/wiki.html")

# View control without a mouse. Note the syntax is <Ctrl-...>, not <C-...>;
# qutebrowser rejects <C-*> outright and aborts loading the rest of the file.
config.bind("<Ctrl-h>", "navigate prev")
config.bind("<Ctrl-l>", "navigate next")
config.bind("<Ctrl-d>", "scroll-page down 0.5")
config.bind("<Ctrl-u>", "scroll-page up 0.5")
config.bind("gu", "navigate back")
config.bind("gd", "navigate forward")
config.bind("gi", "navigate home")
config.bind("gq", "tab-close")
config.bind("gJ", "tab-focus next")
config.bind("gK", "tab-focus prev")
config.bind("B", "open -b https://neighborli.xyz")
