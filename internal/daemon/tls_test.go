package daemon

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A program that took the port while the daemon was down must not get the token: the CLI and hooks trust only NeoSwitch's CA, so the handshake fails before any request is sent.
func TestClientRefusesAnImpostorOnThePort(t *testing.T) {
	s := testState(t)
	var reached atomic.Int32
	impostor := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer impostor.Close()
	s.Listen = strings.TrimPrefix(impostor.URL, "https://")
	if _, ok := Status(s); ok {
		t.Fatal("status trusted an impostor")
	}
	if _, err := Attach(s, 100, "/p", func() error { return nil }); err == nil {
		t.Fatal("attach trusted an impostor")
	}
	Detach(s, 100)
	if n := reached.Load(); n != 0 {
		t.Fatalf("the impostor received %d requests", n)
	}
}

func TestLocalCertificateIsForLoopbackOnly(t *testing.T) {
	s := testState(t)
	ca, err := readCert(caPath(s.dir))
	if err != nil {
		t.Fatal(err)
	}
	// Name constraints on IP addresses make Claude Code refuse the certificate outright.
	if !ca.IsCA || !ca.MaxPathLenZero || len(ca.PermittedIPRanges) != 0 || len(ca.PermittedDNSDomains) != 0 {
		t.Fatalf("CA: IsCA %v, path len zero %v, constraints %v %v", ca.IsCA, ca.MaxPathLenZero, ca.PermittedIPRanges, ca.PermittedDNSDomains)
	}
	server, err := readCert(serverCertPath(s.dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(server.IPAddresses) != 1 || !server.IPAddresses[0].Equal(net.IPv4(127, 0, 0, 1)) || server.NotAfter.Sub(server.NotBefore) > 398*24*time.Hour {
		t.Fatalf("server certificate: %v, valid %s", server.IPAddresses, server.NotAfter.Sub(server.NotBefore))
	}
	if err := server.CheckSignatureFrom(ca); err != nil {
		t.Fatal(err)
	}
}

// The CA's key is never kept, so nothing that reads the state folder can make the CA vouch for another host.
func TestLocalCAKeyIsNotKept(t *testing.T) {
	s := testState(t)
	entries, err := filepath.Glob(tlsDir(s.dir) + "/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if name := filepath.Base(entry); strings.Contains(name, "ca") && strings.Contains(name, "key") {
			t.Fatalf("CA key written: %s", entry)
		}
	}
}
