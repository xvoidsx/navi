# AGENTS.md — navi-bluetooth

> **Read this before touching this component.** `navi-bluetooth` is the one
> mod where a silent failure looks exactly like "the button did nothing".
> Everything below was learned the expensive way; the details live in
> `PAIRING-POSTMORTEM.md` and `BLUETOOTH-JOURNEY.md`.

navi-bluetooth is the nightshadeNeon TUI for Bluetooth on navi: pair,
trust, connect, disconnect, forget, plus adapter power / discoverability /
scan. Bubble Tea + Lip Gloss, native BlueZ D-Bus backend, system bus only.

---

## The one bug that broke everything

```go
// WRONG — this shipped and made pairing fail 100% of the time
call := obj.Go("org.bluez.Device1.Pair", 0, nil, nil)
```

`Object.Go`'s signature is:

```go
func (o *Object) Go(method string, flags Flags, ch chan *Call, args ...interface{}) *Call
```

The fourth parameter is `ch`. Anything after it is **variadic**. So that
trailing `nil` was not "no arguments" — it became *one argument whose value
was nil*. godbus then computes `SignatureOf(nil)` → `getSignature(nil)` →
**nil pointer dereference**, and it happens *before the message is sent*.

The fix:

```go
// RIGHT — no trailing nil. Device1.Pair takes no arguments.
call := obj.Go("org.bluez.Device1.Pair", 0, nil)
```

**Rule: a variadic D-Bus call takes its arguments last, and "no arguments"
means no trailing `nil`.** It compiles. It vets clean. It dies at runtime,
only on the exact call the user just made. Audit every new `obj.Go(` and
`obj.Call(` for a stray `nil` in the argument position.

`TestPairAsyncSendsNoArguments` in `bluez_test.go` guards this on the wire.
Do not delete or weaken it. It has been verified to fail when the bug is
reintroduced.

---

## Why it was invisible (read this before adding any new command)

Three things had to fail at once for the above to present as "nothing
happens":

1. The panic ran inside a **Bubble Tea command goroutine**. `main()` has a
   `defer recover()` writing to `panic.log`, but a deferred recover only
   covers the **main goroutine**. A panic in a command goroutine kills the
   process outright and `panic.log` stays empty.
2. Launched from a **waybar/rofi panel button**, the terminal's stderr is
   closed. The panic text went to a pty nobody was reading.
3. So the visible result was a window that closed instantly, every time.

**Rule: any long-running command that can fail must recover locally and
return an error message the status line can show.** A widget that dies from a
launcher is indistinguishable from a widget that ignores you. `beginPair`
now does this; any new command touching D-Bus must too.

---

## Agent capability: `DisplayOnly`, deliberately

We register **`DisplayOnly`** and call `RequestDefaultAgent`.

**Do not "upgrade" this to `KeyboardDisplay` or `DisplayYesNo`.** It is
tempting — they sound better and §5c of `BLUETOOTH-JOURNEY.md` used to say
so — but the Magic Keyboard's firmware handles a DisplayOnly host and
auto-confirms cleanly, and is unhappy with the more capable ones. The agent's
advertised capability feeds the **IO-capability exchange**; the spec's lookup
table then picks the association model. You do not get to choose the model,
and asking for more capability is not better.

> Honest status: "the keyboard rejects a DisplayYesNo host" comes from btmon,
> not from an A/B test. `DisplayOnly` works and is proven. Treat the stronger
> claim as unverified and don't build on it.

---

## What pairing actually looks like

The callback BlueZ fires *is* the flow selector. Don't predict it — render
each one. But know the ground truth for the hardware we have:

| Device | BlueZ callback | Association model | What the user does |
|--------|----------------|--------------------|---------------------|
| Magic Keyboard | `RequestConfirmation` | Numeric Comparison | **Nothing.** Auto-approved. |
| Legacy PIN device | `RequestPinCode` | Legacy pairing | Type a PIN |
| Host must type | `RequestPasskey` | Passkey Entry | Type a passkey here |
| Device shows a code | `DisplayPasskey` | Passkey Entry | Read it off the device |

The Magic Keyboard has **no display and no keypad**. It cannot show a code and
cannot send one back, so Passkey Entry is physically unavailable to it. The
host displays, the keyboard auto-confirms, done. Whole ceremony: **~1.4 s.**

If someone reports "it asks me to confirm a code and my keyboard has no
screen" — that is **correct behaviour**, not a bug. The keyboard confirms
itself; we just approve on the user's behalf. What *is* a bug is making a
keyboard wait on a human for that approval (see classification, below).

Numeric Comparison is confirmed on the wire. See
`PAIRING-POSTMORTEM.md` §3 for the trace.

---

## Device classification: Icon and Class first, name last

`classifyDevice` (in `bluez.go`) decides a device's kind in this priority
order:

1. **`Icon`** — `input-keyboard`, `audio-headset`, … most reliable
2. **Service UUIDs** — `0x1124` HID, `0x110B`/`0x110E` audio
3. **Class of Device** — major `0x05` peripheral; minor `0x10` keyboard,
   `0x20` pointing device
4. **Name** — last resort only

Live values for the Magic Keyboard:

```
Name: "Keyboard"     ← generic, and it NEVER resolves to "Magic Keyboard"
Icon: "input-keyboard" ← decisive
Class: 0x2540          ← independently says keyboard
UUIDs: []              ← empty until bonded
```

**Never gate a flow decision on the name.** This keyboard advertises as the
literal string `"Keyboard"` forever. The Python prototype's `is_keyboard()`
name heuristic is a trap that happens to work; don't copy it.

Two auto-approve paths exist and both matter:

- `RequestConfirmation` — auto-approve keyboards/mice/trackpads so nobody
  approves a code for their own keyboard. Uses `isInputDeviceKind()`, which
  checks Kind **and** name.
- `AuthorizeService` — auto-allow when `Trusted` or an input device. BlueZ
  asks once per service and those asks overlap; blocking the whole UI on
  them is what kept the device from settling.

For non-input devices (phones, headsets) `RequestConfirmation` *does* prompt,
and the copy says *"if your device shows a number too, check they match"* —
honest, because those devices may have a screen. Do not say "the keyboard
confirms automatically" to a headset.

---

## Agent callbacks must always be answered

Every `Agent1` method that waits on a channel **must** get a reply, or BlueZ
sits there until its bonding timeout expires.

- Multiple callbacks can be in flight at once — `AuthorizeService` arrives
  once per discovered service.
- `model.pairQueue` is a **FIFO of `pendingPrompt`**, not one slot per
  response type. It was one slot per type, and a second prompt overwrote the
  first channel: the first handler blocked forever and pairing timed out with
  no error anywhere.
- `endPair` and `clearPairResp` **reject** every outstanding prompt rather
  than dropping the reference. Dropping it leaves an agent method blocked.

If you add a callback, follow that pattern: enqueue a prompt, answer exactly
one, always reply.

`pairEventKind` distinguishes `pairPromptPasskey` (approve this number) from
`pairPromptPasskeyEnter` (type a number here). They used to share a kind,
which worked only because the handler also assigned `screen` directly — one
refactor away from telling someone to type a number they were meant to
approve. Keep them distinct.

---

## Diagnostics — log before you theorise

The previous session lost a full day guessing which IO capability the keyboard
wanted (§2b of `BLUETOOTH-JOURNEY.md`). The bug after that was found in a
single trace run. **Always capture before changing logic.**

### `--trace-pair <MAC>` — headless instrumented agent

Runs the *real* `naviAgent` with a scripted policy and logs every callback,
with timestamps and the device's classification inputs, to
`~/.local/share/navi/navi-bluetooth/agent-trace.log`.

```
navi-bluetooth --trace-pair 04:69:F8:DA:18:E4
```

A pairing that "does nothing" now leaves a trace saying which callback fired
and why the agent answered the way it did. The trace hooks are no-ops unless
tracing is on, so the TUI pays nothing for them.

### `NAVI_BT_LIVE=1` — hardware test for the real model path

`bluez_live_test.go` drives the actual Bubble Tea model against a real
adapter — discovery → device list → `beginPair` → `Device1.Pair` → trust →
connect — with no terminal in the loop. Skipped unless the env var is set.

```
NAVI_BT_LIVE=1 NAVI_BT_MAC=04:69:F8:DA:18:E4 go test -run TestLive -v -timeout 140s
```

Leave `NAVI_BT_MAC` unset to auto-target the first keyboard-shaped device.

### Logs live in three files, all under `~/.local/share/navi/navi-bluetooth/`

| File | What |
|------|------|
| `startup.log` | process entry — separates `init()` death from `main()` death |
| `panic.log` | main-goroutine panics only (**not** command panics) |
| `agent-trace.log` | every agent callback, only with `--trace-pair` |

**`panic.log` being empty does not mean nothing panicked.** Command panics
never reach it. That gap is exactly what hid the bug above.

---

## Build and test

Go is **not** a runtime dependency and **is not installed on navi machines**
by default. Production gets the committed prebuilt binary. To build:

```bash
cd mods/navi-bluetooth
go build -o navi-bluetooth .
```

To stamp the version (the committed convention):

```bash
go build -ldflags "-X main.buildCommit=local+$(date +%Y%m%d-%H%M%S)" -o navi-bluetooth .
```

`--version` prints it. Unstamped builds print `dev` — don't ship one by
accident.

```bash
go vet ./...
go test ./...                              # hermetic, no hardware, no root
NAVI_BT_LIVE=1 go test -run TestLive -v    # hardware
```

`bt_test.go` is **not** gofmt-clean and was left that way on purpose — it's
untouched pre-existing state, don't churn it in an unrelated diff. Every file
this change-set touched is gofmt-clean.

### Test notes, if you extend them

- `bluez_test.go` stands up a **private `dbus-daemon`** and exports a fake
  `org.bluez.Device1`. Use this, not a mock of our own code — the bug lived
  in godbus argument marshalling, which no mock would have caught.
- `dbus.Dial` returns an **unauthenticated** connection. You must call
  `conn.Auth(nil)` then `conn.Hello()` or every method call just hangs. This
  cost real time.
- The fake's `Pair()` deliberately takes **no arguments**. That is the
  assertion: zero args succeeds, a leaked argument returns `InvalidArgs`. A
  variadic fake **cannot** be used for this — godbus counts the variadic
  parameter as one argument, so it rejects a legitimately empty call.
- `dbus-daemon --nofork` stays in the foreground; read the address off its
  stdout rather than `CombinedOutput()`, or it blocks forever.

---

## Hardware notes (Apple)

- **Magic Keyboard / Magic Mouse have no pairing button.** Flip the side
  switch off, wait ~2 s, on. The device then advertises for a *short window*
  only.
- Consequence for testing: **power the device on immediately before the
  run.** Repeated attempts fail with an empty device list, which reads as
  "discovery is broken" and is not. Confirm with an independent
  `bluetoothctl --timeout 20 scan on` before blaming the code.
- The bench unit is `04:69:F8:DA:18:E4`, USB ID `05ac:0267` (Apple, Inc.),
  modalias `bluetooth:v004Cp0267d0160` (company ID `0x004c` = Apple). Don't
  hardcode any of it — the UI must classify by `Icon`/`Class`, and a
  different Magic Keyboard may present differently. It advertises under the
  bare name `Keyboard`.
- They doze aggressively. If bonding fails with a page timeout, tap a key.

---

## Deploy

Only the **binary** is deployed — `install.sh` calls
`install_mod "navi-bluetooth" "navi-bluetooth"`, and `iso/stage.sh` stages
`navi-bluetooth/navi-bluetooth`. The `.go` sources are not needed on a user
machine, so shipping the fix is a matter of committing the rebuilt binary.

```bash
install.sh --deploy-only          # or:
doas install -m 0755 mods/navi-bluetooth/navi-bluetooth /usr/bin/navi-bluetooth
```

Runtime dependencies: `bluez` (bluetoothd, `bluetoothctl`), and `python3-dbus`
+ `python3-gi` for the legacy `scripts/navi-bt-agent` prototype.

---

## ⚠️ Stale trees — commit from ONE place only

Three copies of this repo exist on this machine. **Only one is live:**

| Path | State |
|------|-------|
| `~/.cache/navi/upstream` | **LIVE.** git repo, origin `xvoidsx/navi`. Has `bluez.go`. |
| `/opt/navi-iso/mods/navi-bluetooth` | ISO staging snapshot, 1 Oct. No `bluez.go`, 4.4 MB binary. |
| `~/Documents/navi/mods/navi-bluetooth` | Old clone, 1 Oct. No `bluez.go`, 4.4 MB binary. |

The latter two predate the D-Bus rewrite entirely. Committing from them would
revert the whole backend. If a push looks like it's missing the fix, check
which tree you built in. (Per the repo rules: local trees can be stale in
**either** direction — fetch and diff before pushing.)

---

## Files touched by the 2026-10-04 pairing fix

Everything below changed in one change-set. Commit them together.

### Source

| File | Change |
|------|--------|
| `bluez.go` | **`PairAsync` nil-argument fix** (the root cause). `AuthorizeService` auto-allows for trusted/input devices. New `isInputDeviceKind()` helper. Trace hooks on every agent callback. gofmt. |
| `main.go` | Single-slot response channels replaced by the `pendingPrompt` FIFO. `clearPairResp`/`endPair` now reject outstanding prompts. `beginPair` passes its already-classified device via `device.toBlueZ()` and recovers panics into a status message. `pairPromptPasskeyEnter` handling. Device-appropriate Numeric Comparison copy. Queued-step indicator. **Deleted dead `screenAppleHelp` + `viewAppleHelp`.** |
| `bt.go` | New `device.toBlueZ()` (model → backend). Split `pairPromptPasskeyEnter` out of the overloaded `pairPromptPasskey`. Added the `dbus` import. |
| `agenttrace.go` | **New.** `--trace-pair <MAC>` headless instrumented agent + the trace hooks it enables. |

### Tests

| File | Change |
|------|--------|
| `bluez_test.go` | **New.** `TestPairAsyncSendsNoArguments` — the regression guard, on the wire, over a private `dbus-daemon`. `TestNilArgPanicsSignatureOf` pins the mechanism. |
| `bluez_live_test.go` | **New.** `NAVI_BT_LIVE=1` hardware tests: `TestLiveDeviceList`, `TestLivePairKeyboard`. |

### Docs

| File | Change |
|------|--------|
| `PAIRING-POSTMORTEM.md` | **New.** Full postmortem: symptom, root cause with the panic trace, why it was invisible, the hardware-truth flow, all latent bugs, verification evidence, and an explicit **"Still unverified"** list. |
| `BLUETOOTH-JOURNEY.md` | **Edited.** Corrected §2a, §5c and §9, which were wrong or self-contradictory (§5c said register `KeyboardDisplay`, contradicting §4b of the same file). Extended the key-files table and open-questions list. |
| `AGENTS.md` | **New.** This file. |

### Binary

| File | Change |
|------|--------|
| `navi-bluetooth` | Rebuilt prebuilt binary (tracked in git by convention). Stamped `dev` — **restamp before committing**, see Build above. |

`bt.go`'s existing `bt_e2e_test.go` and `bt_test.go` were **not** modified.

---

## Still unverified — do not assume these work

- Reconnection after **reboot**, and after a keyboard **power-cycle**. The
  bond is stored and trusted so BlueZ *should* auto-connect. Should ≠ verified.
- **Phone and headset pairing.** Only a keyboard was ever exercised. The
  `RequestConfirmation` prompt path for non-input devices has never run on
  real hardware through this UI.
- **`AuthorizeService` reaching a human.** Now auto-allowed for input
  devices, so that prompt is untested end to end.
- The **`DisplayYesNo`-rejection** claim (see above).
- **Multiple simultaneous keyboards**, and list layout with a full list.
- **Legacy PIN pairing** (`RequestPinCode`) — no device available.
- **Slow-hardware timing.** The auto-approve path has no timing dependence,
  but the 90 s pairing timeout has not been exercised on slow storage. Per
  the repo rules, Cloudbook results outrank assumptions.

If you test any of these, update this file and `PAIRING-POSTMORTEM.md` §6.

---

## The short version

Register `DisplayOnly`. Let BlueZ pick the flow and render each callback
honestly. Classify by `Icon` and `Class`, never by name. Never pass a
trailing `nil` to a variadic D-Bus call. Never let an agent callback go
unanswered, and never let a panic escape a TUI command. Log the callbacks
before you theorise. And test on the real thing — mocks passed while
pairing was 100% broken.