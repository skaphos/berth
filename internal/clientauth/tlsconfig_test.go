package clientauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadTLSConfigEmpty(t *testing.T) {
	cfg, err := LoadTLSConfig("", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RootCAs != nil {
		t.Error("expected RootCAs to be nil when no CA bundle is provided")
	}
	if cfg.ServerName != "" {
		t.Errorf("expected empty ServerName, got %q", cfg.ServerName)
	}
	if cfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be false by default")
	}
	if cfg.MinVersion != 0x0303 {
		t.Errorf("expected MinVersion TLS1.2 (0x0303), got %#x", cfg.MinVersion)
	}
}

func TestLoadTLSConfigWithCABundle(t *testing.T) {
	t.Parallel()

	pemBytes := generateTestCAPEM(t)
	path := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write CA bundle: %v", err)
	}

	cfg, err := LoadTLSConfig(path, "berth.test.example", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RootCAs == nil {
		t.Fatal("expected RootCAs to be populated")
	}
	if cfg.ServerName != "berth.test.example" {
		t.Errorf("expected ServerName to be set, got %q", cfg.ServerName)
	}
}

func TestLoadTLSConfigMissingFile(t *testing.T) {
	t.Parallel()

	_, err := LoadTLSConfig(filepath.Join(t.TempDir(), "nope.crt"), "", false)
	if err == nil {
		t.Fatal("expected error for missing CA bundle file")
	}
}

func TestLoadTLSConfigInvalidPEM(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "junk.crt")
	if err := os.WriteFile(path, []byte("not a cert"), 0o600); err != nil {
		t.Fatalf("write junk: %v", err)
	}
	_, err := LoadTLSConfig(path, "", false)
	if err == nil {
		t.Fatal("expected error for non-PEM content")
	}
}

func TestLoadTLSConfigInsecureSkipVerify(t *testing.T) {
	t.Parallel()

	cfg, err := LoadTLSConfig("", "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify to be true")
	}
}

// generateTestCAPEM produces a self-signed CA certificate in PEM form,
// purely so tests can exercise the PEM-parsing path without shipping a
// fixture file.
func generateTestCAPEM(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "berth-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
