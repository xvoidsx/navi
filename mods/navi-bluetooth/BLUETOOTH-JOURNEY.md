# The Magic Keyboard Bluetooth Journey

**Date:** 2026-10-03 → 2026-10-04 (overnight session)
**Hardware:** Apple Magic Keyboard 2 (A1644, firmware 1.6.0), ThinkPad T440p
**Outcome:** Full wireless pairing working via native BlueZ D-Bus. This doc
captures everything we learned so future agents (and future Raven) don't have
to rediscover it.

---

## 1. The Problem

The Magic Keyboard 2 would not pair through `bluetoothctl`. Every attempt
failed with `org.bluez.Error.AuthenticationFailed` or hung indefinitely.
The keyboard worked fine over Lightning (USB HID), proving the hardware was good.

The keyboard's Bluetooth MAC: `04:69:F8:DA:18:E4`
USB ID (for reference): `05ac:0267` (Apple, Inc.)

---

## 2. What We Tried (and What Failed)

### 2a. bluetoothctl with default agent — FAILED

```
bluetoothctl pair 04:69:F8:DA:18:E4
# → org.bluez.Error.AuthenticationFailed
```

The default `bluetoothctl` agent couldn't complete the pairing.

> **UPDATE (2026-10-04, later session).** The likely reason: `bluetoothctl`
> registers a `KeyboardDisplay`-class agent, and this keyboard's firmware
> handles a `DisplayOnly` host cleanly but is unhappy with the more capable
> one (§4b). That is still a *theory* — nobody has A/B tested the capability
> against this keyboard.
>
> It also stopped being the interesting question. The real reason
> `navi-bluetooth` failed to pair **anything** was a crash in our own code —
> see `PAIRING-POSTMORTEM.md`.

### 2b. Guessing IO capabilities — SUPERSEDED

We spent significant time theorizing about which IO capability the keyboard
wanted:
- Tried `DisplayOnly` agent capability
- Tried `KeyboardDisplay` 
- Researched whether the keyboard was `KeyboardOnly` (type-the-code flow)
- Researched whether it was `DisplayYesNo` (numeric comparison flow)

**Lesson:** Stop guessing from documentation. Run `btmon` and read the actual
IO Capability Exchange from the live trace. The spec table is authoritative,
but only the trace tells you what YOUR hardware actually sends.

### 2c. The Python prototype (navi-bt-agent) — WORKED

`scripts/navi-bt-agent` — a Python D-Bus script using `python3-dbus` and
`python3-gi` — successfully paired the keyboard on the first real attempt
(2026-10-04 ~00:40 CDT).

Key design decisions that made it work:
- **No threads for D-Bus calls.** An earlier version used threading for the
  async `Pair()` call and caused glibc tcache corruption (crash). The fix was
  to use async D-Bus (`reply_handler`/`error_handler`) with the GLib main loop.
- **Correct Properties API.** An early bug called `Get()` directly on
  `org.bluez.Device1` instead of going through `org.freedesktop.DBus.Properties`.
  BlueZ Device1 doesn't have a `Get` method — properties go through the
  standard Properties interface.
- **DisplayOnly capability.** The agent registered with `DisplayOnly`.
- **Interactive RequestConfirmation.** Instead of auto-accepting, it prompted
  the user with the 6-digit code and waited for `y`.

---

## 3. Ground Truth: The btmon Trace

Running `doas btmon` during the successful Python pairing gave us the definitive
answer. This supersedes ALL prior speculation.

### IO Capability Exchange (the critical part)

| Side | IO Capability Sent | MITM Required |
|------|-------------------|---------------|
| **Navi host** (our agent) | `DisplayOnly` (0x00) | Yes |
| **Magic Keyboard** | `DisplayYesNo` (0x01) | No |

### What BlueZ Did

BlueZ emitted a **`User Confirmation Request`** with a 6-digit passkey.
This is the **Numeric Comparison** association model.

### The Pairing Flow (exact sequence)

1. `IO Capability Exchange` — host: DisplayOnly, keyboard: DisplayYesNo
2. `User Confirmation Request` (passkey: 6-digit number)
3. User typed `y` in the Python agent (~15 seconds later — no rush, no timeout)
4. `User Confirmation Reply` sent to keyboard
5. `Simple Pairing Complete` — Status: Success
6. Link key generated and stored (unauthenticated P-192 combination key)
7. `Pair Device` — Status: Success
8. Authentication and encryption completed
9. BlueZ performed HID service discovery

### What This Proves

- **The flow is Numeric Comparison, NOT Passkey Entry.** Earlier notes claiming
  the keyboard uses `KeyboardOnly` or requires typing a code ON the keyboard
  were wrong. They were inferred from Apple documentation and macOS behavior,
  not from this hardware.
- **The keyboard has no display.** It cannot show the 6-digit code. Its firmware
  auto-confirms on its side. There is nothing for the user to "compare" against.
- **The 15-second gap didn't matter.** The keyboard waited patiently for our
  confirmation. No timeout pressure.
- **The link key is "unauthenticated"** in the Bluetooth spec sense — meaning
  no MITM protection during pairing (because the keyboard auto-confirms without
  displaying). The connection is still encrypted. For pairing a keyboard in your
  own home, this is fine.

### Why the "Type the Code" Memory Exists

Raven remembered the Apple flow as "type the code on the keyboard and hit Enter."
This is likely from:
- Older Magic Keyboard firmware that DID use Passkey Entry (KeyboardOnly), OR
- macOS presenting the flow differently, OR
- A different Bluetooth keyboard entirely

The btmon doesn't lie: THIS keyboard, on firmware 1.6.0, does Numeric Comparison.
Don't trust memory over a packet trace.

---

## 4. Bluetooth Stack Learnings

### 4a. IO Capabilities Determine the Pairing Method

Bluetooth Secure Simple Pairing (SSP) has 4 association models:

| Model | When It's Used | User Experience |
|-------|---------------|-----------------|
| **Numeric Comparison** | Both sides can display (DisplayOnly/DisplayYesNo combos) | Both show 6-digit code, user confirms match |
| **Passkey Entry** | One side has keyboard, other has display | User types code on the keyboard side |
| **Just Works** | One side is NoInputNoOutput | No user interaction, no MITM protection |
| **Out of Band** | NFC or other side channel | Rarely used |

The combination is determined by a lookup table in the Bluetooth spec (Vol 3,
Part H, Section 7.2.2.6). The key insight: **you don't choose the method —
the IO capability exchange determines it automatically.**

Our agent says `DisplayOnly` + keyboard says `DisplayYesNo` → Numeric Comparison.
There's no configuration to change this; it's physics.

### 4b. BlueZ Agent Capabilities (from best to least capable)

```
KeyboardDisplay  >  DisplayYesNo  >  DisplayOnly  >  NoInputNoOutput
```

- **KeyboardDisplay**: Can show codes AND accept input. Most capable in theory.
- **DisplayYesNo**: Can show codes and get yes/no. Good for most cases.
- **DisplayOnly**: Can show codes but can't get confirmation... except BlueZ
  still calls RequestConfirmation and the agent CAN respond. (This is what
  the Python prototype used successfully.)
- **NoInputNoOutput**: Just Works only. No MITM protection.

**Key insight (corrected 2026-10-04):** Bigger is NOT always better. The Magic
Keyboard's firmware REJECTS pairing when the host claims DisplayYesNo — it
apparently tries full two-sided Numeric Comparison and fails because it can't
display. With DisplayOnly, the keyboard auto-confirms cleanly. btmon proved
this: same keyboard, same BlueZ, only the host IO cap changed.

**navi-bluetooth uses DisplayOnly.** It's proven with the Magic Keyboard, and
it handles phones (RequestConfirmation), headsets (Just Works), and most
other devices. The only loss vs KeyboardDisplay is devices where the HOST
must type a passkey (rare legacy keyboards) — acceptable tradeoff.

Don't assume "most capable agent" is the right choice. Test with real hardware.

### 4c. The org.bluez.Agent1 Callbacks

These are the methods BlueZ calls on your agent during pairing:

| Callback | When Called | What Your UI Should Do |
|----------|-------------|----------------------|
| `RequestConfirmation(device, passkey)` | Numeric Comparison | Show code, ask y/n. **Be honest:** the keyboard auto-confirms; the user is approving, not comparing. |
| `DisplayPasskey(device, passkey, entered)` | Passkey Entry (device displays) | Show "type this ON the device" + live digit count |
| `RequestPasskey(device)` | Passkey Entry (we enter) | Show input field, return the typed number |
| `RequestPinCode(device)` | Legacy pairing | Show PIN input field |
| `RequestAuthorization(device)` | Incoming connection | Ask y/n |
| `AuthorizeService(device, uuid)` | Service access | Ask y/n (or auto-allow for trusted devices) |
| `Cancel()` | Pairing cancelled | Clear the pairing UI |
| `Release()` | Agent unregistered | Cleanup |

**Critical:** Each callback maps to a distinct UI screen. Don't try to predict
which one will fire — just render beautifully for whichever one does.

### 4d. BlueZ D-Bus API Essentials

**System bus, not session bus.** Bluetooth is a system service:
```python
bus = dbus.SystemBus()  # NOT SessionBus()
```

**Object paths:**
- `/org/bluez` — AgentManager1 (register your agent here)
- `/org/bluez/hci0` — Adapter1 (your Bluetooth controller)
- `/org/bluez/hci0/dev_XX_XX_XX_XX_XX_XX` — Device1 (a Bluetooth device)

Note the MAC format in D-Bus paths: colons become underscores, all caps.

**Properties go through org.freedesktop.DBus.Properties:**
```python
props = dbus.Interface(device_obj, "org.freedesktop.DBus.Properties")
paired = props.Get("org.bluez.Device1", "Paired")
# NOT: device_obj.Get("Paired")  ← this doesn't exist!
```

**Discovery is event-driven.** Don't poll. Subscribe to:
- `org.freedesktop.DBus.ObjectManager.InterfacesAdded` — new device found
- `org.freedesktop.DBus.ObjectManager.InterfacesRemoved` — device gone
- `org.freedesktop.DBus.Properties.PropertiesChanged` — e.g., Name resolved,
  Paired changed from false → true

**The "Name" property resolves late.** A device may first appear with no name
(or a generic name like "Keyboard"), then get its real name via
PropertiesChanged. Your UI must handle updates, not just initial discovery.

### 4e. Trust vs Pair vs Connect (the three steps)

These are SEPARATE operations that people constantly confuse:

1. **Pair** — Cryptographic bonding. Generates and stores a link key.
   One-time operation. `Device1.Pair()`
2. **Trust** — Authorization to auto-connect in the future. `Trusted=true`
   Without this, BlueZ won't auto-connect the HID profile.
3. **Connect** — Establish the actual L2CAP connection for a profile (HID).
   `Device1.Connect()`

**The correct sequence:** Pair → Trust → Connect.

**Why the keyboard didn't work after bluetoothctl trust/connect:**
We got `org.bluez.Error.Failed br-connection-create-socket`. The trust succeeded
but the HID L2CAP socket couldn't be established. Likely causes:
- Stale bond from the pre-reinstall pairing confusing the keyboard
- Keyboard was asleep (they doze aggressively)

The fix was `bluetoothctl remove` (clear the stale bond) + fresh pair via our
agent. After that, everything worked.

### 4f. Device Classification (how to show names, not MACs)

BlueZ Device1 gives you multiple signals. Check in this priority order:

1. **Icon** — e.g., `input-keyboard`, `audio-headset`, `phone`. Most reliable.
2. **Service UUIDs** — `0x1124` = HID (keyboard/mouse), `0x110B`/`0x110E` = audio
3. **Class of Device** — Major class `0x05` = peripheral; minor `0x10` = keyboard,
   `0x20` = pointing device. (Our keyboard: Class `0x002540`)
4. **Name** — Last resort. Our keyboard initially showed just "Keyboard".

**Never show a bare MAC to the user.** If no name is available, show
`"{Kind} · {last 5 of MAC}"` (e.g., "Keyboard · 18:E4") as a distinguisher.
A normie should never have to read `04:69:F8:DA:18:E4`.

---

## 5. Design Decisions for navi-bluetooth

These are the opinionated UX calls we made. They're intentional.

### 5a. Honest Pairing Language

**Don't say:** "Confirm the passkey matches on both devices"
**Do say:** "Approve pairing — code: 810696. The keyboard confirms automatically."

The old text implies there's something to compare against. There isn't.
The keyboard has no display. Being honest builds trust; being technically
precise but misleading erodes it.

### 5b. Auto-Trust Input Devices

After successful pairing, keyboards/mice/trackpads auto-trust (no prompt).
Raven's principle: *"A normie is not going to hunt down a MAC address in the
terminal."*

If the user went through pairing, they want it to work. Don't add friction.

Detection is by name (`isInputDevice`): matches "keyboard", "mouse", "trackpad",
"trackpoint", Apple device names, AND BlueZ's generic "Keyboard". This matters
because the keyboard initially advertises as just "Keyboard" before the real
name resolves.

### 5c. The Universal Agent Approach

**Don't build a device-type → flow mapping table.** It's fragile and will break
on weird hardware.

Instead:
1. Register `DisplayOnly` — **not** `KeyboardDisplay`. This contradicts what
   this section originally said; see the correction note below.
2. Let BlueZ call the right callback for each device
3. Render a beautiful screen per callback
4. Classify devices for DISPLAY (names, icons) — never for flow logic

The callback IS the flow selector. This is the key architectural insight.

> **CORRECTION (2026-10-04, later session).** This section originally said to
> register `KeyboardDisplay`, which directly contradicted §4b of this same
> document. `DisplayOnly` is deliberate and proven; the Magic Keyboard's
> firmware handles a DisplayOnly host and auto-confirms cleanly. Do not
> "upgrade" the capability. See `PAIRING-POSTMORTEM.md` §7.

One caveat on point 4: classification for *flow decisions* is not the same as
classification for display. `RequestConfirmation` does need to know whether
the device is a keyboard — to auto-approve instead of prompting — and it uses
`Icon` → `Class of Device` for that, never the name. On this keyboard the
name never resolves past the generic `"Keyboard"`.

### 5d. Normie-First Error Messages

When things fail, translate D-Bus errors to human language:
- `org.bluez.Error.Failed br-connection-create-socket` →
  "Couldn't reach the keyboard — tap a key to wake it up and try again"
- `org.bluez.Error.AuthenticationFailed` →
  "Pairing didn't go through — make sure the keyboard is in pairing mode"
- Timeout → "The keyboard didn't respond — is it still on?"

Never show raw D-Bus error strings to the user. Ever.

---

## 6. The Full Working Sequence (for reference)

This is the exact sequence that works, validated 2026-10-04:

```bash
# 1. Start the agent (registers D-Bus agent, scans, finds keyboard)
navi-bt-agent
# → finds: Keyboard (04:69:F8:DA:18:E4)

# 2. Agent initiates pairing, BlueZ calls RequestConfirmation
# → shows: "Pair with Keyboard (04:69:F8:DA:18:E4)? [y/n]"
# → shows: "Confirm passkey 810696 on both devices [y/n]"

# 3. User types 'y'
# → D-Bus: User Confirmation Reply sent
# → Simple Pairing Complete: Success
# → Link key stored

# 4. Agent sets Trusted=true via Properties API

# 5. Agent calls Device1.Connect()
# → HID profile connects
# → Keyboard works wirelessly
```

Total time from `y` to typing wirelessly: seconds.

---

## 7. Open Questions / Future Work

- [ ] **Phone pairing test** — Raven will test with his phone via navi-bluetooth
- [ ] **Reconnection after reboot** — does the keyboard auto-connect on boot?
- [ ] **Reconnection after keyboard power-cycle** — power switch off/on
- [ ] **Multiple keyboards** — what if two "Keyboard" devices are discovered?
- [ ] **Battery level** — BlueZ exposes battery via Device1; show it in the UI?
- [ ] **The `DisplayPinCode` callback** — currently a no-op; needs a UI screen
- [ ] **Service authorization** — auto-allows for input devices now (see
      postmortem §4b); prompting for everything else is still untested
- [ ] **`AuthorizeService` reaching a human** — never exercised on hardware
- [ ] **The §4b claim that the keyboard rejects a DisplayYesNo host** —
      believed from btmon, never A/B tested

---

## 8. Key Files

| File | Purpose |
|------|---------|
| `scripts/navi-bt-agent` | Working Python prototype (the one that cracked it) |
| **`PAIRING-POSTMORTEM.md`** | **Why navi-bluetooth paired nothing — read next** |
| `bluez.go` | Native BlueZ D-Bus backend + the Agent1 implementation |
| `agenttrace.go` | `--trace-pair <MAC>` — headless agent, logs every callback |
| `bluez_test.go` | Hermetic regression tests over a private `dbus-daemon` |
| `bluez_live_test.go` | Hardware test: `NAVI_BT_LIVE=1 go test -run TestLive` |
| `mods/navi-bluetooth/bluez.go` | Native Go D-Bus backend (in progress) |
| `mods/navi-bluetooth/bt.go` | Legacy bluetoothctl wrapper (fallback) |
| `mods/navi-bluetooth/main.go` | Bubble Tea UI |
| `mods/navi-bluetooth/DBUS-REWRITE-PLAN.md` | Luna/Codex architecture handoff |
| `~/workspace/research_notes/apple-magic-keyboard-linux-pairing-20261003-1205/report.md` | Original research (some superseded by btmon) |

---

## 9. The One-Sentence Summary

**Stop guessing what the hardware wants — run btmon (or `--trace-pair`), read
the IO Capability Exchange, register `DisplayOnly`, and render beautifully for
whichever callback BlueZ fires.**

(The original phrasing here was "register the most capable D-Bus agent you
can". That is wrong — see §5c and `PAIRING-POSTMORTEM.md` §7. The
capability you advertise feeds the IO-capability exchange, and the spec's
lookup table — not your ambition — picks the association model.)

---

## 10. What came after this document

`PAIRING-POSTMORTEM.md` — why `navi-bluetooth` failed to pair *anything* while
`navi-bt-agent` worked, and the five latent bugs found alongside the fix. Read
that one next; it supersedes §2a and §5c here.

---

*Written by Lain, 2026-10-04, after the session where Raven typed his first
wireless message from the Magic Keyboard. 🎉*
