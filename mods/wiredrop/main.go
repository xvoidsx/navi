package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const version = "1.0.0"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--dump" {
		dumpSample()
		return
	}
	if len(os.Args) < 2 {
		// Bare `wiredrop`: TUI when interactive, help otherwise.
		if isTTY() {
			os.Exit(runTUIEntry())
		}
		usage()
		os.Exit(2)
	}
	paths, err := resolvePaths()
	if err != nil {
		fatal(err)
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		fatal(err)
	}

	switch os.Args[1] {
	case "daemon":
		if err := runDaemon(cfg, paths); err != nil {
			fatal(err)
		}
	case "tui":
		os.Exit(runTUIEntry())
	case "ls":
		runList(paths)
	case "send":
		runSend(cfg, paths, os.Args[2:])
	case "ctl":
		runCtl(paths, os.Args[2:])
	case "forget":
		runForget(paths, os.Args[2:])
	case "fingerprint", "fp":
		runFingerprint(paths, cfg)
	case "--version", "-v", "version":
		fmt.Println("wiredrop", version)
	case "--help", "-h", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "wiredrop: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "wiredrop: %v\n", err)
	os.Exit(1)
}

func isTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func usage() {
	fmt.Print(`wiredrop — xvoidsx's own LocalSend. LAN + Tailscale file drops.

  wiredrop              open the TUI (interactive terminals)
  wiredrop daemon       run the discovery + receive daemon
  wiredrop ls           list nearby devices
  wiredrop send <file…> <device>
                        send files (device = alias or fingerprint prefix)
  wiredrop ctl accept|decline <sessionId>
                        decide a pending incoming transfer
  wiredrop forget <alias>
                        drop a pinned device (after a fingerprint change)
  wiredrop fingerprint  show this machine's fingerprint + word ceremony

The daemon speaks LocalSend v2.2 both directions, so the LocalSend app
on your phone just works. No resume in v1 — interrupted transfers
restart from zero.
`)
}

func runTUIEntry() int {
	paths, err := resolvePaths()
	if err != nil {
		fatal(err)
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		fatal(err)
	}
	if err := runTUI(cfg, paths); err != nil {
		fatal(err)
	}
	return 0
}

// runList prints the peer cache: alias, fingerprint, addresses, last-seen.
func runList(paths *Paths) {
	peers := NewPeerCache(filepath.Join(paths.RuntimeDir, "peers.json")).List()
	known, _ := LoadKnownHosts(paths.KnownHosts)
	if len(peers) == 0 {
		fmt.Println("no devices seen yet — is the daemon running? (`systemctl --user start wiredrop`)")
		return
	}
	for _, p := range peers {
		trust := "new"
		if known != nil && known.Check(p.Alias, p.Fingerprint) == TrustKnown {
			trust = "trusted"
		}
		age := time.Since(p.LastSeen)
		seen := "just now"
		if age > time.Minute {
			seen = fmt.Sprintf("%dm ago", int(age.Minutes()))
		}
		fmt.Printf("● %-20s  %-12s  [%s]  %s  %s\n",
			p.Alias, trust, p.Via, strings.Join(p.Addresses, ","), seen)
		fmt.Printf("  fingerprint: %s\n", p.Fingerprint)
	}
}

// runSend is `wiredrop send <file…> <device> [--pin=…] [--yes]`.
func runSend(cfg *Config, paths *Paths, args []string) {
	var pin string
	var yes bool
	var rest []string
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--pin="):
			pin = strings.TrimPrefix(a, "--pin=")
		case a == "--yes" || a == "-y":
			yes = true
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) < 2 {
		fmt.Fprintln(os.Stderr, "usage: wiredrop send <file…> <device> [--pin=…] [--yes]")
		os.Exit(2)
	}
	files, target := rest[:len(rest)-1], rest[len(rest)-1]

	peers := NewPeerCache(filepath.Join(paths.RuntimeDir, "peers.json"))
	known, err := LoadKnownHosts(paths.KnownHosts)
	if err != nil {
		fatal(err)
	}
	peer, err := resolvePeer(peers, target)
	if err != nil {
		fatal(err)
	}

	// TOFU gate. --yes confirms unknown devices non-interactively (the
	// ceremony still prints); a changed fingerprint always refuses.
	if err := checkTrust(known, peer.Alias, peer.Fingerprint); err != nil {
		te, ok := err.(*trustError)
		if !ok {
			fatal(err)
		}
		if te.kind == "changed" {
			fatal(err)
		}
		fmt.Printf("new device %q — confirm its fingerprint:\n", te.alias)
		fmt.Printf("  🔑 %s\n  %s\n", te.words, te.fingerprint)
		if !yes {
			fmt.Print("accept and pin this device? [y/N] ")
			if !readYes() {
				fmt.Println("not pinned — transfer cancelled.")
				os.Exit(1)
			}
		} else {
			fmt.Println("(pinned via --yes)")
		}
		if err := known.Confirm(te.alias, te.fingerprint); err != nil {
			fatal(err)
		}
	}

	cert, err := loadOrCreateCert(paths.CertDir)
	if err != nil {
		fatal(err)
	}
	x509Cert, err := parseCert(cert)
	if err != nil {
		fatal(err)
	}
	model := deviceModel()
	self := DeviceInfo{
		Alias: cfg.Alias, Version: ProtoVersion,
		DeviceModel: &model, DeviceType: DeviceTypeDesktop,
		Fingerprint: fingerprintOf(x509Cert), Port: cfg.Port, Protocol: "https",
	}

	var total int64
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			total += st.Size()
		}
	}
	fmt.Printf("→ %s (%d file(s), %s)\n", peer.Alias, len(files), humanSize(total))
	last := ""
	err = sendFiles(cfg, self, peer, files, pin, true, &cert,
		func(name string, sent, total int64) {
			pct := 0
			if total > 0 {
				pct = int(sent * 100 / total)
			}
			line := fmt.Sprintf("\r  %-30s %3d%% %s/%s", name, pct, humanSize(sent), humanSize(total))
			if line != last {
				fmt.Print(line)
				last = line
			}
		})
	fmt.Println()
	if err != nil {
		fatal(err)
	}
	fmt.Println("✓ delivered")

	history := NewHistory()
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	history.Append(TransferRecord{
		Time: time.Now(), Direction: "sent", Peer: peer.Alias,
		Files: names, Bytes: total, Fingerprint: peer.Fingerprint,
	})
}

func readYes() bool {
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(sc.Text()))
	return s == "y" || s == "yes"
}

// runCtl is `wiredrop ctl accept|decline <sessionId>`.
func runCtl(paths *Paths, args []string) {
	if len(args) != 2 || (args[0] != "accept" && args[0] != "decline") {
		fmt.Fprintln(os.Stderr, "usage: wiredrop ctl accept|decline <sessionId>")
		os.Exit(2)
	}
	sock := filepath.Join(paths.RuntimeDir, "control.sock")
	if err := ctlCall(sock, args[0], args[1]); err != nil {
		fatal(err)
	}
	fmt.Println("ok")
}

// runForget drops a pinned device.
func runForget(paths *Paths, args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: wiredrop forget <alias>")
		os.Exit(2)
	}
	known, err := LoadKnownHosts(paths.KnownHosts)
	if err != nil {
		fatal(err)
	}
	if known.Forget(args[0]) {
		fmt.Printf("forgot %q — its next sighting runs the ceremony again.\n", args[0])
	} else {
		fmt.Printf("no pinned device named %q.\n", args[0])
	}
}

// runFingerprint shows this machine's identity for out-of-band comparison.
func runFingerprint(paths *Paths, cfg *Config) {
	cert, err := loadOrCreateCert(paths.CertDir)
	if err != nil {
		fatal(err)
	}
	x509Cert, err := parseCert(cert)
	if err != nil {
		fatal(err)
	}
	fp := fingerprintOf(x509Cert)
	fmt.Printf("alias:        %s\n", cfg.Alias)
	fmt.Printf("fingerprint:  %s\n", fp)
	fmt.Printf("words:        %s\n", fingerprintWords(fp))
}
