package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// loadOrCreateCert returns the daemon's TLS identity, generating and
// persisting a self-signed ECDSA certificate on first run. The private key
// is stored at mode 0600. The certificate's SHA-256 (DER) is the LocalSend
// fingerprint — stable across restarts, which is what makes TOFU work.
func loadOrCreateCert(dir string) (tls.Certificate, error) {
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			cert, err := tls.LoadX509KeyPair(certPath, keyPath)
			if err == nil {
				return cert, nil
			}
			// Corrupt pair — fall through and regenerate. A new cert
			// means a new fingerprint, which peers will (correctly)
			// treat as a fingerprint change until re-confirmed.
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return tls.Certificate{}, fmt.Errorf("wiredrop: cert dir: %w", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("wiredrop: keygen: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("wiredrop: serial: %w", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "wiredrop"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"wiredrop"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("wiredrop: create cert: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("wiredrop: marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("wiredrop: write key: %w", err)
	}
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, fmt.Errorf("wiredrop: write cert: %w", err)
	}
	return tls.LoadX509KeyPair(certPath, keyPath)
}

// fingerprintOf returns the LocalSend fingerprint for a certificate: the
// hex SHA-256 of the raw DER bytes. In HTTPS mode every peer computes the
// same value from the TLS handshake, so no fingerprint is ever trusted
// from a JSON body alone — fingerprintFromConn always re-derives it from
// the connection.
func fingerprintOf(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// parseCert parses the leaf of a tls.Certificate.
func parseCert(c tls.Certificate) (*x509.Certificate, error) {
	if len(c.Certificate) == 0 {
		return nil, fmt.Errorf("wiredrop: empty certificate chain")
	}
	return x509.ParseCertificate(c.Certificate[0])
}

// fingerprintFromConn derives the peer fingerprint from the TLS
// connection state of an inbound request. This is the value TOFU pins —
// never the "fingerprint" string inside a JSON body, which anyone can lie
// about. (Outbound client connections use InsecureSkipVerify against
// self-signed certs, so the client derives it the same way from
// PeerCertificates[0].)
func fingerprintFromConn(state tls.ConnectionState) (string, bool) {
	if len(state.PeerCertificates) == 0 {
		return "", false
	}
	return fingerprintOf(state.PeerCertificates[0]), true
}
