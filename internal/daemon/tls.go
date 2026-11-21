package daemon

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// The daemon serves TLS on 127.0.0.1 with a certificate from a CA of its own, which sessions trust through NODE_EXTRA_CA_CERTS. The user's Claude credential reaches the daemon, so a program that takes the port while the daemon is down must not be able to read it: without the daemon's key it cannot complete the handshake.
//
// The CA's private key is never written: it signs the server certificate once and is gone, so nobody who reads the state folder later can make the CA vouch for anything else. It carries no name constraints: Claude Code's TLS stack refuses every certificate under a CA that constrains IP addresses (UNSUPPORTED_CONSTRAINT_TYPE), measured with Claude Code 2.1.330.

func tlsDir(dir string) string         { return filepath.Join(dir, "tls") }
func caPath(dir string) string         { return filepath.Join(tlsDir(dir), "ca.pem") }
func serverCertPath(dir string) string { return filepath.Join(tlsDir(dir), "server.pem") }
func serverKeyPath(dir string) string  { return filepath.Join(tlsDir(dir), "server-key.pem") }

// BundlePath is the CA file a routed session trusts: the user's own extra CAs, if any, followed by NeoSwitch's.
func BundlePath(dir string) string { return filepath.Join(tlsDir(dir), "bundle.pem") }

// serverValidity is how long the server certificate lasts. Platform verifiers refuse server certificates valid for more than 398 days, so it stays under that; the CA, which nothing verifies that way, lasts as long. Both are made again together, since the CA's key is gone: renewTLS does it only while no daemon runs, so no session trusting the old CA is still connected.
const serverValidity = 397 * 24 * time.Hour

// ensureTLS creates the CA and server certificate when they are missing or unreadable, and renews them when they near expiry and renew allows it.
func ensureTLS(dir string, renew bool) error {
	if _, err := tls.LoadX509KeyPair(serverCertPath(dir), serverKeyPath(dir)); err == nil {
		if cert, err := readCert(serverCertPath(dir)); err == nil && (!renew || time.Until(cert.NotAfter) > 30*24*time.Hour) {
			if _, err := readCert(caPath(dir)); err == nil {
				return nil
			}
		}
	}
	if err := os.MkdirAll(tlsDir(dir), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(tlsDir(dir), 0o700); err != nil {
		return err
	}
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	loopback := net.IPv4(127, 0, 0, 1).To4()
	caTemplate := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "NeoSwitch local CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(serverValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create NeoSwitch's local CA: %w", err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		return err
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(serverValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{loopback},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create NeoSwitch's local certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return err
	}
	// The key goes first: a certificate without its key would make every later start fail.
	if err := WriteFileAtomic(serverKeyPath(dir), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	if err := WriteFileAtomic(serverCertPath(dir), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}), 0o600); err != nil {
		return err
	}
	return WriteFileAtomic(caPath(dir), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600)
}

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	return n
}

func readCert(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("not a PEM certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}

// serverTLS is the daemon's listener configuration.
func serverTLS(dir string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(serverCertPath(dir), serverKeyPath(dir))
	if err != nil {
		return nil, fmt.Errorf("load NeoSwitch's local certificate: %w", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
}

// clientTLS trusts only NeoSwitch's CA, so the CLI and the hooks never send the token to a program that took the port.
func clientTLS(dir string) (*tls.Config, error) {
	data, err := os.ReadFile(caPath(dir))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("NeoSwitch's local CA is unreadable")
	}
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
}

// WriteBundle writes the CA file a routed session trusts. NODE_EXTRA_CA_CERTS set in project settings replaces the one in the user's environment, which may hold a corporate CA Claude Code needs, so that file's certificates come first. userCAs is the path from the environment of the session being switched, or "".
func WriteBundle(dir, userCAs string) (string, error) {
	ca, err := os.ReadFile(caPath(dir))
	if err != nil {
		return "", err
	}
	var bundle []byte
	if userCAs != "" && userCAs != BundlePath(dir) {
		if data, err := os.ReadFile(userCAs); err == nil {
			bundle = append(bundle, data...)
			if len(data) > 0 && data[len(data)-1] != '\n' {
				bundle = append(bundle, '\n')
			}
		}
	}
	bundle = append(bundle, ca...)
	if err := WriteFileAtomic(BundlePath(dir), bundle, 0o644); err != nil {
		return "", err
	}
	return BundlePath(dir), nil
}
