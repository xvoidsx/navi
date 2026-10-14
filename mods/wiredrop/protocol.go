// Package main implements wiredrop — xvoidsx's own LocalSend.
//
// wiredrop speaks the LocalSend protocol v2.2
// (https://github.com/localsend/protocol) wholesale: multicast discovery,
// a two-way /register handshake, and the upload API (prepare-upload,
// upload, cancel) over HTTPS with a self-signed certificate whose SHA-256
// is the device fingerprint. That buys interop with the official LocalSend
// apps on phones and desktops for free; wiredrop's value-add is everything
// around the wire: Tailscale-aware discovery, the TOFU trust ceremony,
// dunst receive-consent, and the nightshadeNeon TUI/CLI.
package main

import "time"

// Protocol constants. Defaults come straight from the LocalSend spec:
// multicast group 224.0.0.167, port 53317 for both UDP and TCP.
const (
	// ProtoVersion is the protocol version we announce. The spec document
	// is v2.2 (its JSON examples still show "2.0" — stale examples).
	ProtoVersion = "2.2"

	// MulticastAddr is the LocalSend discovery group.
	MulticastAddr = "224.0.0.167:53317"

	// DefaultPort is the TCP port both sides expect by default. If it is
	// taken, the daemon binds the next free port and announces it —
	// discovery carries the port, so this degrades cleanly.
	DefaultPort = 53317

	// apiPrefix prefixes every LocalSend v2 route.
	apiPrefix = "/api/localsend/v2"

	// DeviceTypeDesktop is our deviceType. The enum is UI-only per the
	// spec (mobile/desktop/web/headless/server); unknown values fall back
	// to desktop in the official client.
	DeviceTypeDesktop = "desktop"

	// announceInterval is how often the daemon re-announces on multicast.
	announceIntervalSeconds = 30
)

// consentTimeoutSeconds is how long the daemon waits for the human to
// accept/decline an incoming transfer before defaulting to DECLINE.
// A var (not const) so tests can shrink it; production stays 60.
var consentTimeoutSeconds time.Duration = 60

// DeviceInfo is the LocalSend device object. One struct serves every
// shape via omitempty: the multicast announce carries port/protocol and
// announce:true; the /register response carries neither (per the spec's
// documented response shape).
type DeviceInfo struct {
	Alias       string  `json:"alias"`
	Version     string  `json:"version"`
	DeviceModel *string `json:"deviceModel"` // nullable
	DeviceType  string  `json:"deviceType"`
	Fingerprint string  `json:"fingerprint"`
	Port        int     `json:"port,omitempty"`
	Protocol    string  `json:"protocol,omitempty"` // "https" — we never do http
	Download    bool    `json:"download"`           // download API active (v2; we don't)
	Announce    *bool   `json:"announce,omitempty"` // multicast only
}

// FileMetadata carries optional timestamps for a file.
type FileMetadata struct {
	Modified *string `json:"modified,omitempty"`
	Accessed *string `json:"accessed,omitempty"`
}

// FileMeta describes one file in a prepare-upload request.
type FileMeta struct {
	ID       string        `json:"id"`
	FileName string        `json:"fileName"`
	Size     int64         `json:"size"`
	FileType string        `json:"fileType"`
	SHA256   *string       `json:"sha256,omitempty"` // nullable; we always send it
	Preview  *string       `json:"preview,omitempty"`
	Metadata *FileMetadata `json:"metadata,omitempty"`
}

// PrepareUploadRequest is POST /prepare-upload's body.
type PrepareUploadRequest struct {
	Info  DeviceInfo          `json:"info"`
	Files map[string]FileMeta `json:"files"`
}

// PrepareUploadResponse is POST /prepare-upload's 200 body: the session
// id plus a per-file token map. Files the receiver won't take are simply
// absent from the map (partial accept).
type PrepareUploadResponse struct {
	SessionID string            `json:"sessionId"`
	Files     map[string]string `json:"files"`
}

// infoResponse returns the shape the spec documents for GET /info and
// the POST /register response: no port, no protocol, no announce.
func (d DeviceInfo) infoResponse() DeviceInfo {
	return DeviceInfo{
		Alias:       d.Alias,
		Version:     d.Version,
		DeviceModel: d.DeviceModel,
		DeviceType:  d.DeviceType,
		Fingerprint: d.Fingerprint,
		Download:    d.Download,
	}
}
