package operator

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

// LoadTLSConfig builds a *tls.Config for the Berth API client. The CA
// bundle file is appended to the system trust store so a private CA can
// coexist with publicly-trusted issuers (e.g. a corporate Ingress chain).
// An empty caBundleFile yields a config with no extra roots — the
// system trust store is used as-is. serverName, when non-empty, overrides
// the SNI / certificate name verification (useful when the API server is
// reached through a port-forward or a non-DNS hostname).
func LoadTLSConfig(caBundleFile, serverName string, insecureSkipVerify bool) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         serverName,
		InsecureSkipVerify: insecureSkipVerify, //nolint:gosec // gated by an explicit flag for dev use
	}

	if caBundleFile == "" {
		return cfg, nil
	}

	pem, err := os.ReadFile(caBundleFile) //nolint:gosec // operator-supplied path
	if err != nil {
		return nil, fmt.Errorf("read CA bundle %q: %w", caBundleFile, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("CA bundle contained no valid PEM certificates")
	}
	cfg.RootCAs = pool
	return cfg, nil
}
