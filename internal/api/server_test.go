package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewServerDefaultsAndOptions(t *testing.T) {
	t.Parallel()

	handler := http.NewServeMux()
	server := NewServer(
		WithAddress("127.0.0.1:9443"),
		WithHandler(handler),
		WithTLSFiles("cert.pem", "key.pem"),
	)

	if server.httpServer.Addr != "127.0.0.1:9443" {
		t.Fatalf("addr = %q, want %q", server.httpServer.Addr, "127.0.0.1:9443")
	}
	if server.httpServer.Handler != handler {
		t.Fatal("handler was not applied")
	}
	if server.tlsCertFile != "cert.pem" || server.tlsKeyFile != "key.pem" {
		t.Fatalf("tls files = %q/%q, want cert.pem/key.pem", server.tlsCertFile, server.tlsKeyFile)
	}

	defaultServer := NewServer()
	if defaultServer.httpServer.Addr != ":8443" {
		t.Fatalf("default addr = %q, want %q", defaultServer.httpServer.Addr, ":8443")
	}
	if defaultServer.httpServer.Handler == nil {
		t.Fatal("default handler is nil")
	}
}

func TestServerStartRejectsNilContext(t *testing.T) {
	t.Parallel()

	server := NewServer(WithTLSFiles("cert.pem", "key.pem"))

	//lint:ignore SA1012 intentional: this test asserts Start rejects a nil context.
	err := server.Start(nil) //nolint:staticcheck // same — keeps golangci happy too.
	if err == nil || err.Error() != "start server: nil context" {
		t.Fatalf("err = %v, want nil-context error", err)
	}
}

func TestServerStartRequiresTLSFiles(t *testing.T) {
	t.Parallel()

	server := NewServer()
	err := server.Start(context.Background())
	if err == nil || err.Error() != "tls cert and key files are required" {
		t.Fatalf("err = %v, want tls error", err)
	}
}

func TestServerStartShutsDownOnContextCancel(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeTLSFiles(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := NewServer(
		WithAddress("127.0.0.1:0"),
		WithTLSFiles(certFile, keyFile),
	)

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := server.Start(ctx)
	if !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("err = %v, want %v", err, http.ErrServerClosed)
	}
}

func writeTLSFiles(t *testing.T) (string, string) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
		},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")

	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	privateKeyDER := x509.MarshalPKCS1PrivateKey(privateKey)
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privateKeyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	return certFile, keyFile
}
