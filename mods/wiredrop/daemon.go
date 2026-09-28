package main

import (
	"context"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// runDaemon is `wiredrop daemon`: discovery + HTTPS server + receive
// consent + control socket. It runs until SIGINT/SIGTERM.
func runDaemon(cfg *Config, paths *Paths) error {
	if err := os.MkdirAll(paths.RuntimeDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DownloadDir, 0o755); err != nil {
		return fmt.Errorf("wiredrop: download dir: %w", err)
	}

	cert, err := loadOrCreateCert(paths.CertDir)
	if err != nil {
		return err
	}
	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return fmt.Errorf("wiredrop: parse cert: %w", err)
	}
	fp := fingerprintOf(x509Cert)

	peers := NewPeerCache(filepath.Join(paths.RuntimeDir, "peers.json"))
	known, err := LoadKnownHosts(paths.KnownHosts)
	if err != nil {
		return err
	}

	model := deviceModel()
	info := DeviceInfo{
		Alias:       cfg.Alias,
		Version:     ProtoVersion,
		DeviceModel: &model,
		DeviceType:  DeviceTypeDesktop,
		Fingerprint: fp,
		Port:        cfg.Port,
		Protocol:    "https",
		Download:    false, // download API is v2
	}

	srv := NewServer(cfg, paths, info, cert, peers, known)
	consenter := NewConsenter(known, peers)
	srv.SetConsent(consenter.Ask)
	srv.SetResolver(consenter.Resolve)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// HTTPS server (bind scope enforced in listenAddrs).
	srvErr := make(chan error, 1)
	go func() {
		srvErr <- srv.Start(listenAddrs(), cfg.Port)
	}()
	// Wait for the bind so the log line below reports the real port
	// (Start resolves port 0 to an ephemeral port asynchronously).
	for i := 0; i < 100 && srv.ActualPort() == 0; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	// Discovery: multicast + tailscale + cold-start LAN sweep.
	go startDiscovery(ctx, srv)
	// Control socket for `wiredrop ctl`.
	ctlStop := make(chan struct{})
	defer close(ctlStop)
	go func() {
		if err := consenter.serveControl(filepath.Join(paths.RuntimeDir, "control.sock"), ctlStop); err != nil {
			log.Printf("wiredrop: control socket: %v", err)
		}
	}()

	log.Printf("wiredrop: listening as %q on port %d (fingerprint %s)",
		cfg.Alias, srv.ActualPort(), shortFP(fp))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case s := <-sig:
		log.Printf("wiredrop: %v — shutting down", s)
	case err := <-srvErr:
		return err
	}
	cancel()
	srv.Stop()
	return nil
}

// deviceModel is the model string in our announce. Keep it generic.
func deviceModel() string { return "navi" }
