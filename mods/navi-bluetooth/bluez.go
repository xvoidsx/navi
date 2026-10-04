// bluez.go — native BlueZ D-Bus backend for navi-bluetooth.
//
// This replaces the bluetoothctl subprocess wrapper. It talks to BlueZ
// directly over the system bus for device discovery (Device1 via
// ObjectManager) and pairing (a native Agent1 implementation).
//
// The backend never touches Bubble Tea state directly. It emits BlueZEvent
// values on a channel; the Tea model converts them to messages.

package main

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// ---------------------------------------------------------------------------
// Device model
// ---------------------------------------------------------------------------

// DeviceKind classifies a Bluetooth device for display.
type DeviceKind int

const (
	DeviceUnknown DeviceKind = iota
	DeviceKeyboard
	DeviceMouse
	DeviceTrackpad
	DeviceAudio
	DevicePhone
	DeviceComputer
)

func (k DeviceKind) String() string {
	switch k {
	case DeviceKeyboard:
		return "Keyboard"
	case DeviceMouse:
		return "Mouse"
	case DeviceTrackpad:
		return "Trackpad"
	case DeviceAudio:
		return "Audio"
	case DevicePhone:
		return "Phone"
	case DeviceComputer:
		return "Computer"
	default:
		return "Device"
	}
}

// Emoji returns a glyph for the device kind in the list.
func (k DeviceKind) Emoji() string {
	switch k {
	case DeviceKeyboard:
		return "⌨"
	case DeviceMouse:
		return "🖱"
	case DeviceTrackpad:
		return "▦"
	case DeviceAudio:
		return "🎧"
	case DevicePhone:
		return "📱"
	case DeviceComputer:
		return "💻"
	default:
		return "◌"
	}
}

// BlueZDevice is the backend's view of an org.bluez.Device1 object.
type BlueZDevice struct {
	Path      dbus.ObjectPath
	Address   string
	Name      string // best human name (Name, else Alias, else "")
	Icon      string
	Class     uint32
	UUIDs     []string
	Kind      DeviceKind
	Paired    bool
	Trusted   bool
	Connected bool
}

// isMACLike reports whether s looks like a Bluetooth MAC address
// (colons or dashes). BlueZ sets Alias to the dashed MAC when a device
// has no real name — we must not show that to users.
func isMACLike(s string) bool {
	// Strip separators and check for 12 hex digits.
	stripped := strings.ReplaceAll(strings.ReplaceAll(s, ":", ""), "-", "")
	if len(stripped) != 12 {
		return false
	}
	for _, r := range stripped {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

// DisplayName returns Name, falling back to a kind label + short address.
// It never returns a bare MAC — normies shouldn't have to read those.
func (d BlueZDevice) DisplayName() string {
	if d.Name != "" && !isMACLike(d.Name) {
		return d.Name
	}
	// Last 5 chars of MAC as a distinguisher, e.g. "Keyboard · 18:E4"
	short := d.Address
	if len(short) > 5 {
		short = short[len(short)-5:]
	}
	return fmt.Sprintf("%s · %s", d.Kind.String(), short)
}

// ---------------------------------------------------------------------------
// Classification
// ---------------------------------------------------------------------------

// classifyDevice determines the kind from Device1 properties.
// Priority: Icon → service UUIDs → Class bits → name heuristics.
func classifyDevice(props map[string]dbus.Variant) DeviceKind {
	strProp := func(key string) string {
		if v, ok := props[key]; ok {
			if s, ok := v.Value().(string); ok {
				return s
			}
		}
		return ""
	}

	// 1. Icon, e.g. "input-keyboard", "audio-headset", "phone"
	icon := strings.ToLower(strProp("Icon"))
	switch {
	case strings.Contains(icon, "keyboard"):
		return DeviceKeyboard
	case strings.Contains(icon, "mouse"):
		return DeviceMouse
	case strings.Contains(icon, "trackpad"), strings.Contains(icon, "touchpad"):
		return DeviceTrackpad
	case strings.Contains(icon, "audio"), strings.Contains(icon, "headset"),
		strings.Contains(icon, "headphone"), strings.Contains(icon, "speaker"):
		return DeviceAudio
	case strings.Contains(icon, "phone"):
		return DevicePhone
	case strings.Contains(icon, "computer"), strings.Contains(icon, "laptop"):
		return DeviceComputer
	}

	// 2. Service UUIDs
	if v, ok := props["UUIDs"]; ok {
		if uuids, ok := v.Value().([]string); ok {
			for _, u := range uuids {
				ul := strings.ToLower(u)
				switch {
				case strings.Contains(ul, "1124"): // HID
					// HID could be keyboard/mouse — check class below,
					// default to keyboard (most common case for pairing UI)
					return DeviceKeyboard
				case strings.Contains(ul, "110b"), strings.Contains(ul, "110e"),
					strings.Contains(ul, "110d"), strings.Contains(ul, "110f"):
					// A2DP / HSP / HFP audio
					return DeviceAudio
				}
			}
		}
	}

	// 3. Class of Device: major class 0x05 = peripheral
	if v, ok := props["Class"]; ok {
		if class, ok := v.Value().(uint32); ok {
			major := (class >> 8) & 0x1F
			minor := (class >> 2) & 0x3F
			if major == 0x05 { // peripheral
				switch minor {
				case 0x10: // keyboard (0x40 >> 2)
					return DeviceKeyboard
				case 0x20: // pointing (0x80 >> 2)
					return DeviceMouse
				}
				return DeviceKeyboard // generic peripheral → keyboard
			}
			if major == 0x02 { // phone
				return DevicePhone
			}
			if major == 0x01 { // computer
				return DeviceComputer
			}
			if major == 0x04 { // audio
				return DeviceAudio
			}
		}
	}

	// 4. Name heuristics (last resort)
	name := strings.ToLower(strProp("Name"))
	if name == "" {
		name = strings.ToLower(strProp("Alias"))
	}
	switch {
	case strings.Contains(name, "keyboard"):
		return DeviceKeyboard
	case strings.Contains(name, "mouse"):
		return DeviceMouse
	case strings.Contains(name, "trackpad"):
		return DeviceTrackpad
	case strings.Contains(name, "headphone"), strings.Contains(name, "headset"),
		strings.Contains(name, "speaker"), strings.Contains(name, "buds"),
		strings.Contains(name, "airpods"):
		return DeviceAudio
	case strings.Contains(name, "phone"), strings.Contains(name, "pixel"),
		strings.Contains(name, "iphone"), strings.Contains(name, "galaxy"):
		return DevicePhone
	}

	return DeviceUnknown
}

// deviceFromProps builds a BlueZDevice from a Device1 property map.
func deviceFromProps(path dbus.ObjectPath, props map[string]dbus.Variant) BlueZDevice {
	d := BlueZDevice{Path: path}
	if v, ok := props["Address"]; ok {
		if s, ok := v.Value().(string); ok {
			d.Address = s
		}
	}
	if v, ok := props["Name"]; ok {
		if s, ok := v.Value().(string); ok {
			d.Name = s
		}
	}
	if d.Name == "" {
		if v, ok := props["Alias"]; ok {
			if s, ok := v.Value().(string); ok && !isMACLike(s) {
				d.Name = s
			}
		}
	}
	if v, ok := props["Icon"]; ok {
		if s, ok := v.Value().(string); ok {
			d.Icon = s
		}
	}
	if v, ok := props["Class"]; ok {
		if c, ok := v.Value().(uint32); ok {
			d.Class = c
		}
	}
	if v, ok := props["UUIDs"]; ok {
		if u, ok := v.Value().([]string); ok {
			d.UUIDs = u
		}
	}
	if v, ok := props["Paired"]; ok {
		if b, ok := v.Value().(bool); ok {
			d.Paired = b
		}
	}
	if v, ok := props["Trusted"]; ok {
		if b, ok := v.Value().(bool); ok {
			d.Trusted = b
		}
	}
	if v, ok := props["Connected"]; ok {
		if b, ok := v.Value().(bool); ok {
			d.Connected = b
		}
	}
	d.Kind = classifyDevice(props)
	return d
}

// ---------------------------------------------------------------------------
// Backend events (backend → Tea)
// ---------------------------------------------------------------------------

// BlueZEvent is a marker for events the backend emits.
type BlueZEvent interface{ bluezEvent() }

type DeviceAddedEvent struct{ Device BlueZDevice }
type DeviceRemovedEvent struct{ Path dbus.ObjectPath }
type DeviceUpdatedEvent struct{ Device BlueZDevice }
type DiscoveryEvent struct{ Discovering bool }

func (DeviceAddedEvent) bluezEvent()   {}
func (DeviceRemovedEvent) bluezEvent() {}
func (DeviceUpdatedEvent) bluezEvent() {}
func (DiscoveryEvent) bluezEvent()     {}

// Pairing events from the Agent1 callbacks.
type PairDisplayPasskeyEvent struct {
	Device  BlueZDevice
	Passkey uint32
	Entered uint16
}
type PairConfirmEvent struct {
	Device  BlueZDevice
	Passkey uint32
	Resp    chan bool // Tea sends true=approve, false=reject
}
type PairRequestPasskeyEvent struct {
	Device BlueZDevice
	Resp   chan uint32 // Tea sends the entered passkey
}
type PairRequestPINEvent struct {
	Device BlueZDevice
	Resp   chan string // Tea sends the entered PIN
}
type PairCancelledEvent struct{}
type AuthorizeEvent struct {
	Device BlueZDevice
	UUID   string
	Resp   chan bool
}

func (PairDisplayPasskeyEvent) bluezEvent() {}
func (PairConfirmEvent) bluezEvent()        {}
func (PairRequestPasskeyEvent) bluezEvent() {}
func (PairRequestPINEvent) bluezEvent()     {}
func (PairCancelledEvent) bluezEvent()      {}
func (AuthorizeEvent) bluezEvent()          {}

// ---------------------------------------------------------------------------
// Backend
// ---------------------------------------------------------------------------

const agentPathBase = "/org/xvoidsx/navi/bluetooth/agent"

// agentPath is unique per process to avoid conflicts with stale
// registrations from crashed instances.
var agentPath = dbus.ObjectPath(agentPathBase)

func init() {
	agentPath = dbus.ObjectPath(fmt.Sprintf("%s/%d", agentPathBase, os.Getpid()))
	// Early init log: if we never see "main started" in the log, the
	// process died in init() or was killed externally.
	if dir := os.ExpandEnv("$HOME/.local/share/navi/navi-bluetooth"); dir != "" {
		os.MkdirAll(dir, 0755)
		if f, err := os.OpenFile(dir+"/startup.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			fmt.Fprintf(f, "init pid=%d at %s\n", os.Getpid(), time.Now().Format(time.RFC3339))
			f.Close()
		}
	}
}

// BlueZBackend owns the system-bus connection, discovery, and the agent.
type BlueZBackend struct {
	conn        *dbus.Conn
	adapterPath dbus.ObjectPath
	events      chan BlueZEvent

	mu      sync.Mutex
	devices map[dbus.ObjectPath]BlueZDevice
	agent   *naviAgent

	// pairTarget is the device the user explicitly chose to pair.
	// The agent checks this first — it eliminates the lookup race where
	// BlueZ calls RequestConfirmation before our discovery cache has the
	// device, which was causing blank passkeys and missed auto-confirm.
	pairTarget    BlueZDevice
	pairTargetSet bool

	// lastConnectAttempt throttles auto-connect per device, so a keyboard
	// that is merely asleep doesn't get a Connect() call every tick.
	lastConnectAttempt map[dbus.ObjectPath]time.Time

	closed chan struct{}
}

// autoConnectCooldown is how long to wait before asking BlueZ to reconnect
// the same device again.
const autoConnectCooldown = 20 * time.Second

// SetPairTarget records the device the user chose to pair.
func (b *BlueZBackend) SetPairTarget(d BlueZDevice) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pairTarget = d
	b.pairTargetSet = true
}

// SetPairTargetByPath records the pairing target by D-Bus path,
// fetching fresh properties if needed.
func (b *BlueZBackend) SetPairTargetByPath(path dbus.ObjectPath) {
	d := b.lookupDevice(path)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pairTarget = d
	b.pairTargetSet = true
}

// ClearPairTarget clears the pairing target.
func (b *BlueZBackend) ClearPairTarget() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pairTarget = BlueZDevice{}
	b.pairTargetSet = false
}

// NewBlueZBackend connects to the system bus and finds the first adapter.
func NewBlueZBackend() (*BlueZBackend, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("system bus: %w", err)
	}
	b := &BlueZBackend{
		conn:               conn,
		events:             make(chan BlueZEvent, 64),
		devices:            make(map[dbus.ObjectPath]BlueZDevice),
		lastConnectAttempt: make(map[dbus.ObjectPath]time.Time),
		closed:             make(chan struct{}),
	}
	// Find adapter
	objs, err := b.getManagedObjects()
	if err != nil {
		conn.Close()
		return nil, err
	}
	for path, ifaces := range objs {
		if _, ok := ifaces["org.bluez.Adapter1"]; ok {
			b.adapterPath = path
			break
		}
	}
	if b.adapterPath == "" {
		conn.Close()
		return nil, errors.New("no Bluetooth adapter found")
	}
	// Seed initial devices
	for path, ifaces := range objs {
		if props, ok := ifaces["org.bluez.Device1"]; ok {
			b.devices[path] = deviceFromProps(path, props)
		}
	}
	return b, nil
}

// Events returns the backend's event channel.
func (b *BlueZBackend) Events() <-chan BlueZEvent { return b.events }

// Devices returns a snapshot of known devices.
func (b *BlueZBackend) Devices() []BlueZDevice {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]BlueZDevice, 0, len(b.devices))
	for _, d := range b.devices {
		out = append(out, d)
	}
	return out
}

// Close shuts down the backend.
func (b *BlueZBackend) Close() {
	select {
	case <-b.closed:
		return
	default:
		close(b.closed)
	}
	if b.agent != nil {
		b.unregisterAgent()
	}
	b.conn.Close()
}

func (b *BlueZBackend) getManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, error) {
	obj := b.conn.Object("org.bluez", "/")
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := obj.Call("org.freedesktop.DBus.ObjectManager.GetManagedObjects", 0).Store(&objs)
	return objs, err
}

// SetPowered powers the adapter on or off.
func (b *BlueZBackend) SetPowered(on bool) error {
	obj := b.conn.Object("org.bluez", b.adapterPath)
	return obj.Call("org.freedesktop.DBus.Properties.Set", 0,
		"org.bluez.Adapter1", "Powered", dbus.MakeVariant(on)).Err
}

// StartDiscovery begins device discovery.
func (b *BlueZBackend) StartDiscovery() error {
	obj := b.conn.Object("org.bluez", b.adapterPath)
	return obj.Call("org.bluez.Adapter1.StartDiscovery", 0).Err
}

// StopDiscovery ends device discovery.
func (b *BlueZBackend) StopDiscovery() error {
	obj := b.conn.Object("org.bluez", b.adapterPath)
	return obj.Call("org.bluez.Adapter1.StopDiscovery", 0).Err
}

// Pair initiates pairing with a device.
func (b *BlueZBackend) Pair(path dbus.ObjectPath) error {
	obj := b.conn.Object("org.bluez", path)
	return obj.Call("org.bluez.Device1.Pair", 0).Err
}

// PairAsync initiates pairing without blocking; result goes to the callback.
//
// org.bluez.Device1.Pair takes NO arguments. Object.Go's signature is
// Go(method, flags, ch, args ...interface{}), so a trailing nil here is
// swallowed by the variadic as one real argument and godbus computes
// SignatureOf(nil) — a nil dereference that kills the process before the
// call is ever sent. Leave the argument list empty.
func (b *BlueZBackend) PairAsync(path dbus.ObjectPath, done func(error)) {
	obj := b.conn.Object("org.bluez", path)
	call := obj.Go("org.bluez.Device1.Pair", 0, nil)
	go func() {
		err := (<-call.Done).Err
		done(err)
	}()
}

// Connect connects a device's profiles.
func (b *BlueZBackend) Connect(path dbus.ObjectPath) error {
	obj := b.conn.Object("org.bluez", path)
	return obj.Call("org.bluez.Device1.Connect", 0).Err
}

// Disconnect disconnects a device.
func (b *BlueZBackend) Disconnect(path dbus.ObjectPath) error {
	obj := b.conn.Object("org.bluez", path)
	return obj.Call("org.bluez.Device1.Disconnect", 0).Err
}

// SetTrusted sets the Trusted property.
func (b *BlueZBackend) SetTrusted(path dbus.ObjectPath, trusted bool) error {
	obj := b.conn.Object("org.bluez", path)
	return obj.Call("org.freedesktop.DBus.Properties.Set", 0,
		"org.bluez.Device1", "Trusted", dbus.MakeVariant(trusted)).Err
}

// RemoveDevice removes (unpairs) a device.
func (b *BlueZBackend) RemoveDevice(path dbus.ObjectPath) error {
	obj := b.conn.Object("org.bluez", b.adapterPath)
	return obj.Call("org.bluez.Adapter1.RemoveDevice", 0, path).Err
}

// FindByAddress returns the device with the given MAC, if known.
func (b *BlueZBackend) FindByAddress(addr string) (BlueZDevice, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, d := range b.devices {
		if strings.EqualFold(d.Address, addr) {
			return d, true
		}
	}
	return BlueZDevice{}, false
}

// ---------------------------------------------------------------------------
// D-Bus signal subscription (discovery events)
// ---------------------------------------------------------------------------

// Watch starts forwarding ObjectManager and PropertiesChanged signals.
func (b *BlueZBackend) Watch() error {
	// ObjectManager signals (InterfacesAdded/Removed) — match by interface
	// only; BlueZ emits them from the service root and path filtering is
	// fragile across BlueZ versions.
	if err := b.conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesAdded"),
	); err != nil {
		return err
	}
	if err := b.conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.ObjectManager"),
		dbus.WithMatchMember("InterfacesRemoved"),
	); err != nil {
		return err
	}
	// Property changes on Device1
	if err := b.conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.DBus.Properties"),
		dbus.WithMatchMember("PropertiesChanged"),
	); err != nil {
		return err
	}
	ch := make(chan *dbus.Signal, 64)
	b.conn.Signal(ch)
	go b.signalLoop(ch)
	return nil
}

func (b *BlueZBackend) signalLoop(ch chan *dbus.Signal) {
	defer func() {
		if r := recover(); r != nil {
			b.logPanic("signalLoop", r)
		}
	}()
	for {
		select {
		case <-b.closed:
			return
		case sig := <-ch:
			if sig == nil {
				return
			}
			b.handleSignal(sig)
		}
	}
}

// logPanic writes a goroutine panic to the panic log.
func (b *BlueZBackend) logPanic(where string, r interface{}) {
	dir := os.ExpandEnv("$HOME/.local/share/navi/navi-bluetooth")
	os.MkdirAll(dir, 0755)
	if f, err := os.OpenFile(dir+"/panic.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		fmt.Fprintf(f, "=== goroutine panic in %s at %s ===\n%v\n%s\n\n",
			where, time.Now().Format(time.RFC3339), r, debug.Stack())
		f.Close()
	}
}

func (b *BlueZBackend) handleSignal(sig *dbus.Signal) {
	switch sig.Name {
	case "org.freedesktop.DBus.ObjectManager.InterfacesAdded":
		if len(sig.Body) < 2 {
			return
		}
		path, ok := sig.Body[0].(dbus.ObjectPath)
		if !ok {
			return
		}
		ifaces, ok := sig.Body[1].(map[string]map[string]dbus.Variant)
		if !ok {
			return
		}
		if props, ok := ifaces["org.bluez.Device1"]; ok {
			d := deviceFromProps(path, props)
			b.mu.Lock()
			b.devices[path] = d
			b.mu.Unlock()
			b.emit(DeviceAddedEvent{Device: d})
		}
	case "org.freedesktop.DBus.ObjectManager.InterfacesRemoved":
		if len(sig.Body) < 2 {
			return
		}
		path, ok := sig.Body[0].(dbus.ObjectPath)
		if !ok {
			return
		}
		b.mu.Lock()
		_, wasDevice := b.devices[path]
		delete(b.devices, path)
		b.mu.Unlock()
		if wasDevice {
			b.emit(DeviceRemovedEvent{Path: path})
		}
	case "org.freedesktop.DBus.Properties.PropertiesChanged":
		// Refresh the device from current properties
		path := sig.Path
		b.mu.Lock()
		_, known := b.devices[path]
		b.mu.Unlock()
		if !known {
			return
		}
		props := b.getDeviceProps(path)
		if props == nil {
			return
		}
		d := deviceFromProps(path, props)
		b.mu.Lock()
		b.devices[path] = d
		b.mu.Unlock()
		b.emit(DeviceUpdatedEvent{Device: d})
	}
}

func (b *BlueZBackend) getDeviceProps(path dbus.ObjectPath) map[string]dbus.Variant {
	obj := b.conn.Object("org.bluez", path)
	var props map[string]dbus.Variant
	if err := obj.Call("org.freedesktop.DBus.Properties.GetAll", 0, "org.bluez.Device1").Store(&props); err != nil {
		return nil
	}
	return props
}

func (b *BlueZBackend) emit(ev BlueZEvent) {
	select {
	case b.events <- ev:
	case <-b.closed:
	}
}

// ---------------------------------------------------------------------------
// Keeping the view honest
// ---------------------------------------------------------------------------

// ResyncDevices re-reads every known device from BlueZ and emits an update
// for each.
//
// Without this the UI depends entirely on D-Bus signals, and a single
// missed one leaves a row showing stale state until the user hits 'r'. That
// is not hypothetical: it looked exactly like "the keyboard refuses to
// reconnect" when it was really a dropped signal. Cheap insurance, called
// from the poll tick.
func (b *BlueZBackend) ResyncDevices() {
	b.mu.Lock()
	paths := make([]dbus.ObjectPath, 0, len(b.devices))
	for p := range b.devices {
		paths = append(paths, p)
	}
	b.mu.Unlock()

	for _, p := range paths {
		props := b.getDeviceProps(p)
		if props == nil {
			continue
		}
		d := deviceFromProps(p, props)
		b.mu.Lock()
		b.devices[p] = d
		b.mu.Unlock()
		b.emit(DeviceUpdatedEvent{Device: d})
	}
}

// AutoConnectInputDevices nudges BlueZ to reconnect trusted input devices
// that are bonded but not connected.
//
// BlueZ normally gets there by itself — measured on an Apple Magic
// Keyboard, roughly three seconds after the side switch comes back on. This
// is the safety net for the devices it does *not* auto-connect (older
// bonds made before Trusted was set, some headsets), and it means a
// keyboard that is merely off does not need the user to open the manager
// and press enter.
//
// Only trusted, already-paired input devices are touched: this never pairs
// anything new and never touches a device the user hasn't connected before.
// Connect() blocks for as long as the attempt takes, so each one runs on
// its own goroutine.
func (b *BlueZBackend) AutoConnectInputDevices() []dbus.ObjectPath {
	now := time.Now()
	var todo []BlueZDevice

	b.mu.Lock()
	for p, d := range b.devices {
		if d.Connected || !d.Paired || !d.Trusted {
			continue
		}
		if !isInputDeviceKind(d) {
			continue
		}
		if last, ok := b.lastConnectAttempt[p]; ok && now.Sub(last) < autoConnectCooldown {
			continue
		}
		b.lastConnectAttempt[p] = now
		todo = append(todo, d)
	}
	b.mu.Unlock()

	paths := make([]dbus.ObjectPath, 0, len(todo))
	for _, d := range todo {
		paths = append(paths, d.Path)
		d := d
		tracef("auto-connect: %s (%s) is trusted+paired but not connected",
			d.DisplayName(), d.Address)
		go func() {
			if err := b.Connect(d.Path); err != nil {
				tracef("auto-connect: %s -> %v", d.Address, err)
				return
			}
			tracef("auto-connect: %s connected", d.Address)
		}()
	}
	return paths
}

// ---------------------------------------------------------------------------
// Agent1 implementation
// ---------------------------------------------------------------------------

// naviAgent implements org.bluez.Agent1 with DisplayOnly capability.
// Callbacks that need user input emit events with response channels;
// the Tea model answers them.
type naviAgent struct {
	backend *BlueZBackend
}

func (b *BlueZBackend) registerAgent() error {
	agent := &naviAgent{backend: b}
	if err := b.conn.Export(agent, agentPath, "org.bluez.Agent1"); err != nil {
		return fmt.Errorf("export agent: %w", err)
	}
	mgr := b.conn.Object("org.bluez", "/org/bluez")
	// DisplayOnly, not KeyboardDisplay: the Magic Keyboard's firmware
	// handles DisplayOnly hosts (auto-confirms) but rejects pairing when
	// the host claims DisplayYesNo. Proven by btmon 2026-10-04.
	if err := mgr.Call("org.bluez.AgentManager1.RegisterAgent", 0, agentPath, "DisplayOnly").Err; err != nil {
		return fmt.Errorf("register agent: %w", err)
	}
	// Do NOT request default agent (bluetui doesn't either): BlueZ
	// automatically uses the calling application's agent for its own
	// Pair() calls. Claiming default can conflict with other agents.
	b.agent = agent
	return nil
}

func (b *BlueZBackend) unregisterAgent() {
	mgr := b.conn.Object("org.bluez", "/org/bluez")
	mgr.Call("org.bluez.AgentManager1.UnregisterAgent", 0, agentPath)
	// Note: godbus v5 doesn't export Unexport; the export is dropped
	// when the connection closes.
	b.agent = nil
}

// isInputDeviceKind reports whether a BlueZDevice is a keyboard, mouse, or
// trackpad — from Icon/Class/UUIDs, falling back to the name. The name may
// still be BlueZ's generic "Keyboard" (or empty) at pairing time, so the
// structural signals are checked first.
func isInputDeviceKind(d BlueZDevice) bool {
	switch d.Kind {
	case DeviceKeyboard, DeviceMouse, DeviceTrackpad:
		return true
	}
	return isInputDevice(d.Name)
}

// RegisterAgent registers our DisplayOnly agent as the default.
func (b *BlueZBackend) RegisterAgent() error { return b.registerAgent() }

// EnsureDefaultAgent re-registers our agent if needed. We don't claim
// the default slot (BlueZ uses our agent for our own Pair() calls
// automatically); this just makes sure the registration exists.
func (b *BlueZBackend) EnsureDefaultAgent() error {
	if b.agent == nil {
		return b.registerAgent()
	}
	return nil
}

func (b *BlueZBackend) lookupDevice(path dbus.ObjectPath) BlueZDevice {
	b.mu.Lock()
	if d, ok := b.devices[path]; ok {
		b.mu.Unlock()
		return d
	}
	b.mu.Unlock()
	// Cache miss (race between discovery signal and pairing request):
	// fetch fresh properties directly from BlueZ.
	if props := b.getDeviceProps(path); props != nil {
		d := deviceFromProps(path, props)
		b.mu.Lock()
		b.devices[path] = d
		b.mu.Unlock()
		return d
	}
	return BlueZDevice{Path: path}
}

// Release is called when the agent is unregistered.
func (a *naviAgent) Release() *dbus.Error {
	tracef("Release()")
	return nil
}

// AuthorizeService decides whether a device may use a service.
//
// BlueZ asks once per discovered service, and the asks can overlap. For an
// input device — or anything already trusted — there is no decision a
// non-technical user can usefully make, and blocking the whole UI on it is
// what kept the keyboard from ever settling. Auto-allow those; ask for the
// rest.
func (a *naviAgent) AuthorizeService(device dbus.ObjectPath, uuid string) *dbus.Error {
	tracef("AuthorizeService(device=%s, uuid=%s)", device, uuid)
	d := a.backend.lookupDevice(device)
	traceDevice("  resolved", d)
	if d.Trusted || isInputDeviceKind(d) {
		tracePolicy("AuthorizeService", device, "AUTO-ALLOW", "trusted or input device")
		return nil
	}
	resp := make(chan bool, 1)
	a.backend.emit(AuthorizeEvent{Device: d, UUID: uuid, Resp: resp})
	tracePolicy("AuthorizeService", device, "ASK UI", "blocking on resp chan")
	if <-resp {
		tracePolicy("AuthorizeService", device, "ALLOW", "")
		return nil
	}
	tracePolicy("AuthorizeService", device, "REJECT", "")
	return dbus.NewError("org.bluez.Error.Rejected", []interface{}{"rejected by user"})
}

// RequestPinCode asks the user to type a legacy PIN.
func (a *naviAgent) RequestPinCode(device dbus.ObjectPath) (string, *dbus.Error) {
	tracef("RequestPinCode(device=%s)", device)
	d := a.backend.lookupDevice(device)
	traceDevice("  resolved", d)
	resp := make(chan string, 1)
	a.backend.emit(PairRequestPINEvent{Device: d, Resp: resp})
	pin := <-resp
	tracef("  -> PIN returned len=%d", len(pin))
	if pin == "" {
		return "", dbus.NewError("org.bluez.Error.Canceled", []interface{}{"cancelled"})
	}
	return pin, nil
}

// RequestPasskey asks the user to type a passkey on THIS computer.
func (a *naviAgent) RequestPasskey(device dbus.ObjectPath) (uint32, *dbus.Error) {
	tracef("RequestPasskey(device=%s)", device)
	d := a.backend.lookupDevice(device)
	traceDevice("  resolved", d)
	resp := make(chan uint32, 1)
	a.backend.emit(PairRequestPasskeyEvent{Device: d, Resp: resp})
	passkey := <-resp
	// 0xFFFFFFFF sentinel = cancelled (can't send "empty" uint32)
	if passkey == 0xFFFFFFFF {
		tracef("  -> cancelled")
		return 0, dbus.NewError("org.bluez.Error.Canceled", []interface{}{"cancelled"})
	}
	tracef("  -> passkey %06d", passkey)
	return passkey, nil
}

// DisplayPasskey shows a code for the user to type ON THE DEVICE.
// No response needed — BlueZ waits for the device.
func (a *naviAgent) DisplayPasskey(device dbus.ObjectPath, passkey uint32, entered uint16) *dbus.Error {
	tracef("DisplayPasskey(device=%s, passkey=%06d, entered=%d)", device, passkey, entered)
	d := a.backend.lookupDevice(device)
	traceDevice("  resolved", d)
	a.backend.emit(PairDisplayPasskeyEvent{Device: d, Passkey: passkey, Entered: entered})
	return nil
}

// DisplayPinCode shows a PIN for legacy pairing.
func (a *naviAgent) DisplayPinCode(device dbus.ObjectPath, pincode string) *dbus.Error {
	tracef("DisplayPinCode(device=%s, pincode=%s)", device, pincode)
	d := a.backend.lookupDevice(device)
	traceDevice("  resolved", d)
	// Reuse the display-passkey event path with a string; the UI handles it.
	_ = d
	_ = pincode
	return nil
}

// withAgentRecover wraps agent D-Bus methods so a panic becomes a logged
// entry instead of killing the process.
func (a *naviAgent) withAgentRecover() {
	if r := recover(); r != nil {
		a.backend.logPanic("agent", r)
	}
}

// RequestConfirmation asks the user to approve a Numeric Comparison code.
// Input devices (keyboards/mice) auto-confirm: their firmware confirms on
// their side without displaying anything, so prompting the user to "compare"
// is theater. macOS-instant pairing for the devices that need it most.
func (a *naviAgent) RequestConfirmation(device dbus.ObjectPath, passkey uint32) (result *dbus.Error) {
	tracef("RequestConfirmation(device=%s, passkey=%06d)", device, passkey)
	a.backend.tracePairTarget("RequestConfirmation")
	defer func() {
		if r := recover(); r != nil {
			a.backend.logPanic("agent RequestConfirmation", r)
			result = dbus.NewError("org.bluez.Error.Failed", []interface{}{"agent panic"})
		}
	}()
	// Prefer the explicit pair target — the user chose this device, so we
	// know what it is even if the discovery cache hasn't caught up.
	// But if the target is empty (lookup failed), do a fresh lookup
	// instead of auto-failing the Kind check.
	a.backend.mu.Lock()
	if a.backend.pairTargetSet && a.backend.pairTarget.Path == device {
		d := a.backend.pairTarget
		a.backend.mu.Unlock()
		traceDevice("  pairTarget", d)
		// A stashed target with no classification at all means the lookup
		// failed when the pair began. Fall through to a fresh lookup below
		// rather than asking the user to approve a code for their own
		// keyboard.
		if d.Kind != DeviceUnknown || d.Name != "" {
			if isInputDeviceKind(d) {
				tracePolicy("RequestConfirmation", device, "AUTO-APPROVE", "input device")
				return nil // auto-approve keyboards/mice/trackpads
			}
			// Non-input device: prompt via UI.
			tracePolicy("RequestConfirmation", device, "ASK UI", "not an input device")
			resp := make(chan bool, 1)
			a.backend.emit(PairConfirmEvent{Device: d, Passkey: passkey, Resp: resp})
			if <-resp {
				return nil
			}
			return dbus.NewError("org.bluez.Error.Rejected", []interface{}{"rejected by user"})
		}
		// Empty target — fall through to fresh lookup below.
		tracef("  pairTarget is unclassified; doing a fresh lookup")
	} else {
		a.backend.mu.Unlock()
	}

	// Fallback: lookup (may be empty on cache miss).
	d := a.backend.lookupDevice(device)
	traceDevice("  fallback lookup", d)
	// Check Kind (from Icon/Class/UUID) as well as name — the name may
	// not have resolved yet when the callback fires.
	if isInputDeviceKind(d) {
		tracePolicy("RequestConfirmation", device, "AUTO-APPROVE", "input device")
		return nil // auto-approve keyboards/mice/trackpads
	}
	tracePolicy("RequestConfirmation", device, "ASK UI", "not an input device")
	resp := make(chan bool, 1)
	a.backend.emit(PairConfirmEvent{Device: d, Passkey: passkey, Resp: resp})
	if <-resp {
		return nil
	}
	return dbus.NewError("org.bluez.Error.Rejected", []interface{}{"rejected by user"})
}

// Cancel aborts any in-flight pairing UI.
func (a *naviAgent) Cancel() *dbus.Error {
	tracef("Cancel()")
	a.backend.emit(PairCancelledEvent{})
	return nil
}
