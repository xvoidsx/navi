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
	"strings"
	"time"
	"sync"

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
	Device  BlueZDevice
	UUID    string
	Resp    chan bool
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

	closed chan struct{}
}

// NewBlueZBackend connects to the system bus and finds the first adapter.
func NewBlueZBackend() (*BlueZBackend, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("system bus: %w", err)
	}
	b := &BlueZBackend{
		conn:    conn,
		events: make(chan BlueZEvent, 64),
		devices: make(map[dbus.ObjectPath]BlueZDevice),
		closed:  make(chan struct{}),
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
func (b *BlueZBackend) PairAsync(path dbus.ObjectPath, done func(error)) {
	obj := b.conn.Object("org.bluez", path)
	call := obj.Go("org.bluez.Device1.Pair", 0, nil, nil)
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
	if err := mgr.Call("org.bluez.AgentManager1.RequestDefaultAgent", 0, agentPath).Err; err != nil {
		return fmt.Errorf("default agent: %w", err)
	}
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

// RegisterAgent registers our DisplayOnly agent as the default.
func (b *BlueZBackend) RegisterAgent() error { return b.registerAgent() }

// EnsureDefaultAgent re-asserts our agent as the default. Other Bluetooth
// managers (Blueman, bluetoothctl) register their own agents and can steal
// the default slot; call this before pairing to take it back.
func (b *BlueZBackend) EnsureDefaultAgent() error {
	if b.agent == nil {
		return b.registerAgent()
	}
	mgr := b.conn.Object("org.bluez", "/org/bluez")
	return mgr.Call("org.bluez.AgentManager1.RequestDefaultAgent", 0, agentPath).Err
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
func (a *naviAgent) Release() *dbus.Error { return nil }

// AuthorizeService auto-allows service authorization for paired input
// devices; otherwise asks via the UI.
func (a *naviAgent) AuthorizeService(device dbus.ObjectPath, uuid string) *dbus.Error {
	d := a.backend.lookupDevice(device)
	resp := make(chan bool, 1)
	a.backend.emit(AuthorizeEvent{Device: d, UUID: uuid, Resp: resp})
	if <-resp {
		return nil
	}
	return dbus.NewError("org.bluez.Error.Rejected", []interface{}{"rejected by user"})
}

// RequestPinCode asks the user to type a legacy PIN.
func (a *naviAgent) RequestPinCode(device dbus.ObjectPath) (string, *dbus.Error) {
	d := a.backend.lookupDevice(device)
	resp := make(chan string, 1)
	a.backend.emit(PairRequestPINEvent{Device: d, Resp: resp})
	pin := <-resp
	if pin == "" {
		return "", dbus.NewError("org.bluez.Error.Canceled", []interface{}{"cancelled"})
	}
	return pin, nil
}

// RequestPasskey asks the user to type a passkey on THIS computer.
func (a *naviAgent) RequestPasskey(device dbus.ObjectPath) (uint32, *dbus.Error) {
	d := a.backend.lookupDevice(device)
	resp := make(chan uint32, 1)
	a.backend.emit(PairRequestPasskeyEvent{Device: d, Resp: resp})
	passkey := <-resp
	// 0xFFFFFFFF sentinel = cancelled (can't send "empty" uint32)
	if passkey == 0xFFFFFFFF {
		return 0, dbus.NewError("org.bluez.Error.Canceled", []interface{}{"cancelled"})
	}
	return passkey, nil
}

// DisplayPasskey shows a code for the user to type ON THE DEVICE.
// No response needed — BlueZ waits for the device.
func (a *naviAgent) DisplayPasskey(device dbus.ObjectPath, passkey uint32, entered uint16) *dbus.Error {
	d := a.backend.lookupDevice(device)
	a.backend.emit(PairDisplayPasskeyEvent{Device: d, Passkey: passkey, Entered: entered})
	return nil
}

// DisplayPinCode shows a PIN for legacy pairing.
func (a *naviAgent) DisplayPinCode(device dbus.ObjectPath, pincode string) *dbus.Error {
	d := a.backend.lookupDevice(device)
	// Reuse the display-passkey event path with a string; the UI handles it.
	_ = d
	_ = pincode
	return nil
}

// RequestConfirmation asks the user to approve a Numeric Comparison code.
// Input devices (keyboards/mice) auto-confirm: their firmware confirms on
// their side without displaying anything, so prompting the user to "compare"
// is theater. macOS-instant pairing for the devices that need it most.
func (a *naviAgent) RequestConfirmation(device dbus.ObjectPath, passkey uint32) *dbus.Error {
	d := a.backend.lookupDevice(device)
	// Check Kind (from Icon/Class/UUID) as well as name — the name may
	// not have resolved yet when the callback fires.
	if d.Kind == DeviceKeyboard || d.Kind == DeviceMouse || d.Kind == DeviceTrackpad || isInputDevice(d.Name) {
		return nil // auto-approve keyboards/mice/trackpads
	}
	resp := make(chan bool, 1)
	a.backend.emit(PairConfirmEvent{Device: d, Passkey: passkey, Resp: resp})
	if <-resp {
		return nil
	}
	return dbus.NewError("org.bluez.Error.Rejected", []interface{}{"rejected by user"})
}

// Cancel aborts any in-flight pairing UI.
func (a *naviAgent) Cancel() *dbus.Error {
	a.backend.emit(PairCancelledEvent{})
	return nil
}
