# navi-bluetooth: the pairing that never happened

**Date:** 2026-10-04
**Hardware:** Apple Magic Keyboard (A1644-class, USB-C/toggle switch), T440p
**Adapter:** `E8:B1:FC:3B:81:A6` (BlueZ 5.x, `bluetoothd`)
**Status:** FIXED and verified on hardware — cold pair from zero bond,
`paired / trusted / connected`, HID attached in the kernel.

Companion to `BLUETOOTH-JOURNEY.md`. That document is the *what* of BlueZ
pairing; this one is the *why nothing worked*, and it supersedes §2a and
§5c of the journey doc (both are wrong — see "Corrections" at the bottom).

---

## 1. Symptom

> "nothing is working"

`navi-bluetooth` could not pair *anything*. Pressing `p` (or `enter`) on a
discovered device produced no prompt, no error, and — when launched from a
panel button — the window simply vanished.

The companion Python agent `scripts/navi-bt-agent` paired the same keyboard
in about 1.4 seconds. Same adapter, same BlueZ, same keyboard.

---

## 2. Root cause

`mods/navi-bluetooth/bluez.go`, `BlueZBackend.PairAsync`:

```go
call := obj.Go("org.bluez.Device1.Pair", 0, nil, nil)
```

`org.bluez.Device1.Pair` takes **no arguments**. godbus's method is:

```go
func (o *Object) Go(method string, flags Flags, ch chan *Call, args ...interface{}) *Call
```

The fourth parameter is `ch`; the fifth onwards is **variadic**. So the
trailing `nil` was not "no arguments" — it became *one real argument whose
value was nil*:

```
args == []interface{}{nil}          // len 1
if len(args) > 0 {
    msg.Headers[FieldSignature] = MakeVariant(SignatureOf(args...))
}
```

`SignatureOf(nil)` → `getSignature(nil)` → **nil pointer dereference**.

```
panic: runtime error: invalid memory address or nil pointer dereference
github.com/godbus/dbus/v5.getSignature({0x0, 0x0}, …)   sig.go:59
github.com/godbus/dbus/v5.(*Object).createCall(…)        object.go:123
main.(*BlueZBackend).PairAsync(…)                        bluez.go:501
```

The fix is to leave the argument list empty:

```go
call := obj.Go("org.bluez.Device1.Pair", 0, nil)
```

Every zero-argument call site was audited. `PairAsync` was the only one
carrying the extra `nil`; `StartDiscovery`, `StopDiscovery`, `Connect`,
`Disconnect`, `GetManagedObjects` were all already correct.

### Why it was invisible

Three separate things had to fail at once for this to read as "nothing
happens":

1. **The panic was unrecoverable in context.** It happened inside a Bubble
   Tea command. `main()` has a `defer recover()` that logs to `panic.log`,
   but a deferred recover in `main` only covers the *main goroutine*. A panic
   in a command goroutine kills the process outright.
2. **stderr went nowhere.** Launched from a waybar/rofi panel button, the
   terminal's stderr is closed. The panic text was written to a pty nobody
   was reading.
3. **`panic.log` was empty.** Only main-goroutine panics were ever logged, so
   the one file that could have explained it never got the entry.

The net effect: an unconditional crash on the primary action, surfacing as
silence. `startup.log` showed the process starting and nothing else.

> **Lesson (general):** a Bubble Tea command that panics takes the whole TUI
> with it, and from a panel launcher that is indistinguishable from "the
> button did nothing". Long-running widget commands that can fail should
> recover locally and return an error message. `beginPair` now does.

---

## 3. What the flow actually is (hardware truth)

The journey doc's btmon reading was right, and this run confirms it on the
wire. Full capture in `~/.local/share/navi/navi-bluetooth/agent-trace.log`:

```
[02:59:38.932] TARGET  name="Keyboard" icon="input-keyboard" class=0x2540 kind=Keyboard
[02:59:39.849] RequestConfirmation(device=/org/bluez/hci0/dev_04_69_F8_DA_18_E4, passkey=137463)
[02:59:39.849] DECIDE RequestConfirmation: AUTO-APPROVE — input device
[02:59:40.413] EVENT DeviceUpdated paired=true
[02:59:41.258] PAIR OK
[02:59:41.280] SetTrusted OK
[02:59:41.406] Connect OK
```

**BlueZ calls `RequestConfirmation`. That is Numeric Comparison.** Whole
ceremony: **1.4 seconds.**

There is no Passkey Entry here, and there is nothing for the user to
compare. The Magic Keyboard has no display; its firmware auto-confirms on
its side. So for this device the honest flow is: *agent approves, keyboard
confirms itself, done.*

### Device classification is what makes the auto-approve work

`classifyDevice` decides "is this an input device" from BlueZ's properties,
in priority order: **Icon → service UUIDs → Class of Device → name.**

Live values for this keyboard:

| Property | Value |
|----------|-------|
| `Name` | `Keyboard` (generic — never resolves to "Magic Keyboard") |
| `Icon` | `input-keyboard` ← decisive |
| `Class` | `0x2540` (major `0x05` peripheral, minor `0x10` keyboard) |
| `UUIDs` | `[]` until bonded |
| `LegacyPairing` | `false` |

`Icon` and `Class` are both present *before* bonding and both independently
classify it as a keyboard. The name is the weakest signal and the last
resort — it never resolves on this hardware, so any logic that leans on the
name alone is fragile.

---

## 4. Latent bugs fixed along the way

These did not cause the reported failure, but each would have caused a
*different* silent failure later. All are in this change.

### 4a. A single response slot orphaned concurrent agent callbacks

The model held one channel per response type:

```go
pairRespBool   chan bool
pairRespUint32 chan uint32
pairRespString chan string
```

BlueZ can have several agent callbacks in flight at once — `AuthorizeService`
arrives once per discovered service. A second prompt overwrote the first
channel, so the first `AuthorizeService` handler blocked on a channel nobody
would ever write to, and BlueZ waited out its whole bonding timeout.

Replaced with a FIFO of `pendingPrompt`. Answering pops the head; the next
queued prompt surfaces instead of vanishing. `endPair` and
`clearPairResp` now *reject* every outstanding prompt rather than dropping
the references, so no agent method is ever left blocked.

### 4b. `AuthorizeService` blocked the whole UI for input devices

It unconditionally emitted a prompt and waited. The Python reference
auto-allows. For a keyboard — or anything already trusted — there is no
decision a non-technical user can usefully make, and blocking on it is
precisely what kept the device from settling. Now auto-allows for trusted
devices and input devices, and asks only for everything else.

### 4c. `beginPair` threw away the classification it already had

```go
backend.SetPairTargetByPath(dbusObjectPath(path))   // re-looks-up by path
```

`beginPair` is handed a fully classified device and then discards it in
favour of a fresh lookup, which can come back *less* informed than what the
user was shown (this keyboard's name is empty-ish until BlueZ fills it in).
The agent's `RequestConfirmation` then can't tell it's a keyboard and asks
the user to approve a code for their own keyboard. Now passes the device
the list already classified (`device.toBlueZ()`).

### 4d. Panics in pairing commands were unrecoverable

`beginPair`'s command now recovers and returns the panic as a status message
instead of taking the window down. See §2.

### 4e. Dead code told users to drop to a terminal

`screenAppleHelp` rendered *"Apple keyboards need a D-Bus agent, not
bluetoothctl. Run this in another terminal: `navi-bt-agent <mac>`"* — and was
**never assigned anywhere**, so it was unreachable. Removed, along with
`viewAppleHelp`. Dead code that instructs users to open a terminal is worse
than no code; pairing now works natively.

### 4f. The Numeric Comparison copy was device-specific

The approve screen said *"the keyboard confirms automatically"* for every
device, including headsets. Now it keys off the device kind: keyboards get
the auto-confirm line, everything else gets *"if your device shows a number
too, check they match"* — which is the honest description of Numeric
Comparison for a device that has a screen.

### 4g. `pairEventKind` conflated two opposite instructions

`RequestConfirmation` (approve this number) and `RequestPasskey` (type a
number here) both mapped to `pairPromptPasskey`. It worked only because the
handler also assigned `screen` directly; any future path that set the kind
without the screen would have told someone to type a number they were meant
to approve. Split out `pairPromptPasskeyEnter`.

---

## 5. Verification

### Hermetic (every run, no hardware)

```
go vet ./...
go test ./...
```

`TestPairAsyncSendsNoArguments` is the regression guard. It starts a private
`dbus-daemon`, exports a fake `org.bluez.Device1` whose `Pair()` takes **no
arguments**, and asserts `PairAsync` reaches it with an empty argument list
without panicking. A zero-arg handler is the whole assertion: godbus counts
a variadic parameter as one argument, so a variadic fake cannot accept a
legitimately empty call — while any leaked argument comes back as
`InvalidArgs`.

Confirmed the test actually bites: reintroducing the trailing `nil` fails it.

```
bluez_test.go:147: PairAsync panicked: runtime error: invalid memory address or nil pointer dereference
--- FAIL: TestPairAsyncSendsNoArguments
```

`TestNilArgPanicsSignatureOf` pins the mechanism itself so the reason
`PairAsync` takes no arguments lives next to the code.

### On hardware

`--trace-pair <MAC>` runs the real `naviAgent` headless with a scripted
policy, logging every callback to
`~/.local/share/navi/navi-bluetooth/agent-trace.log`. Same agent the TUI
drives, so a pairing that "does nothing" leaves evidence.

`bluez_live_test.go` drives the real model against a real adapter —
discovery → device list → `beginPair` → `Device1.Pair` → trust → connect —
with no terminal involved. Skipped unless `NAVI_BT_LIVE=1`:

```
NAVI_BT_LIVE=1 NAVI_BT_MAC=04:69:F8:DA:18:E4 go test -run TestLive -v -timeout 140s
```

Result, from a **cleared bond**:

```
--- PASS: TestLiveDeviceList (5.14s)
    found: Keyboard (04:69:F8:DA:18:E4) kind=Keyboard
--- PASS: TestLivePairKeyboard (9.50s)
    target: Keyboard (04:69:F8:DA:18:E4) paired=false
    Device1.Pair returned success
    status line the user sees: "trusted Keyboard — it will auto-connect"
    OK paired+trusted+connected: Keyboard (04:69:F8:DA:18:E4)
```

Final BlueZ state, and the kernel agreeing:

```
Paired: yes   Bonded: yes   Trusted: yes   Connected: yes
UUID: Human Interface Device... (00001124-0000-1000-8000-00805f9b34fb)

/proc/bus/input/devices:
  U: Uniq=04:69:f8:da:18:e4
  H: Handlers=sysrq kbd leds event27
```

---

## 6. Still unverified

Honest list. None of these were tested; do not assume them.

- **Reconnection after reboot**, and after a keyboard power-cycle. The bond
  is stored and trusted, so BlueZ *should* auto-connect, but "should" is not
  "verified".
- **Phone / headset pairing.** Only a keyboard was exercised. The
  `RequestConfirmation` prompt path (non-input devices) has never run on
  real hardware through this UI.
- **Service authorization prompt** (`AuthorizeService` reaching a human).
  Now auto-allowed for input devices, so this path is untested end to end.
- **The §4b claim that the Magic Keyboard *rejects* a `DisplayYesNo` host.**
  Believed from btmon, never A/B tested. We register `DisplayOnly` and it
  works; whether `DisplayYesNo`/`KeyboardDisplay` would also work is
  untested.
- **Multiple simultaneous keyboards**, and the `btBodyRows`/windowing layout
  with a full list.
- **Legacy PIN pairing** (`RequestPinCode`) — no device available.
- **Slow-hardware timing.** Per AGENTS.md, Cloudbook results outrank
  assumptions; the auto-approve path has no timing dependence, but the
  90 s pairing timeout has not been exercised on slow storage.

---

## 7. Corrections to `BLUETOOTH-JOURNEY.md`

Three claims in that document are wrong or self-contradictory. Left in
place they will mislead the next person.

| § | Claim | Reality |
|---|-------|---------|
| §2a | bluetoothctl "couldn't complete the pairing… the root cause was never fully isolated" | Partly explained: `KeyboardDisplay`-class agents and this keyboard's Numeric Comparison flow are a bad combination. But the headline failure was in *our* code, not bluetoothctl's. |
| §5c | "Register `KeyboardDisplay` (most capable agent)" | **Wrong, and contradicts §4b of the same document.** We register `DisplayOnly`. `KeyboardDisplay` is the capability we explicitly do *not* claim. |
| §9 | "register the most capable D-Bus agent you can" | **Wrong.** DisplayOnly is deliberate and proven. "Most capable" is not the goal; the IO-capability exchange decides the association model, not our ambition. |

Also worth adding to the key-files table: `bluez.go` (agent + backend),
`agenttrace.go` (`--trace-pair` diagnostics), `bluez_test.go` (hermetic
regression), `bluez_live_test.go` (`NAVI_BT_LIVE=1` hardware test).

---

## 8. House rules this earned

- **A variadic D-Bus call takes its arguments last, and "no arguments" means
  no trailing `nil`.** This reads fine and compiles fine and dies at runtime.
- **Anything launched from a panel button must fail loudly in its own
  window.** A panic in a TUI command goroutine is invisible from a launcher;
  recover locally and return a status message.
- **When a pairing "does nothing", log the agent callbacks before touching
  logic.** The bug was found in one trace run; guessing at flows cost the
  previous session a full day (§2b of the journey doc — "we spent
  significant time theorizing").
- **Test the real shape.** The regression test asserts on the wire via a
  private `dbus-daemon`, not on a mock of our own code.