package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha256hex(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func TestNewStaticAuthenticatorHashesAndCopies(t *testing.T) {
	t.Parallel()

	keys := map[string]Identity{
		"raw-token": {Holder: "holder-a", Tenant: "tenant-a"},
	}
	a := NewStaticAuthenticator(keys)

	// Mutating the input map after construction must not affect the
	// authenticator: we hashed and stored a copy.
	keys["raw-token"] = Identity{Holder: "holder-b"}

	id, err := a.Authenticate(context.Background(), "raw-token")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if id.Holder != "holder-a" {
		t.Fatalf("Holder = %q, want holder-a", id.Holder)
	}
}

func TestStaticAuthenticatorRejectsUnknownAndEmptyTokens(t *testing.T) {
	t.Parallel()

	a := NewStaticAuthenticator(map[string]Identity{
		"good": {Holder: "h"},
	})
	if _, err := a.Authenticate(context.Background(), "wrong"); err == nil {
		t.Fatal("expected error for unknown token")
	}
	if _, err := a.Authenticate(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestStaticAuthenticatorErrorMessageIsGeneric(t *testing.T) {
	t.Parallel()
	// The error string must not include the rejected token, the known
	// key list, or anything that would help an attacker confirm a guess.
	a := NewStaticAuthenticator(map[string]Identity{"valid-key": {Holder: "h"}})
	_, err := a.Authenticate(context.Background(), "definitely-invalid-secret")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "definitely-invalid-secret") {
		t.Fatalf("error leaked the input token: %v", err)
	}
	if strings.Contains(err.Error(), "valid-key") {
		t.Fatalf("error leaked a known key id: %v", err)
	}
}

func writeKeysFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "keys")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write keys file: %v", err)
	}
	return path
}

func TestNewStaticAuthenticatorFromKeysFileParsesEntries(t *testing.T) {
	t.Parallel()

	content := "" +
		"# Berth API keys (test fixture)\n" +
		"\n" +
		"team-a:" + sha256hex("token-a") + "\n" +
		"team-b:" + sha256hex("token-b") + "\n"

	path := writeKeysFile(t, content)
	a, err := NewStaticAuthenticatorFromKeysFile(path)
	if err != nil {
		t.Fatalf("NewStaticAuthenticatorFromKeysFile: %v", err)
	}

	id, err := a.Authenticate(context.Background(), "token-a")
	if err != nil {
		t.Fatalf("Authenticate token-a: %v", err)
	}
	if id.Holder != "team-a" || id.Tenant != "team-a" {
		t.Fatalf("Identity = %+v, want Holder/Tenant=team-a", id)
	}

	if _, err := a.Authenticate(context.Background(), "token-b"); err != nil {
		t.Fatalf("Authenticate token-b: %v", err)
	}
	if _, err := a.Authenticate(context.Background(), "token-c"); err == nil {
		t.Fatal("Authenticate unknown token must fail")
	}
}

func TestNewStaticAuthenticatorFromKeysFileRequiresPath(t *testing.T) {
	t.Parallel()
	if _, err := NewStaticAuthenticatorFromKeysFile(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestNewStaticAuthenticatorFromKeysFileMissingFile(t *testing.T) {
	t.Parallel()
	if _, err := NewStaticAuthenticatorFromKeysFile("/nonexistent/keys-file"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadKeysFileRejectsMalformedLines(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"missing colon":      "team-a abcd1234\n",
		"empty key id":       ":" + sha256hex("x") + "\n",
		"hash too short":     "team-a:abc\n",
		"hash wrong length":  "team-a:" + sha256hex("x") + "ff\n",
		"hash uppercase":     "team-a:" + strings.ToUpper(sha256hex("x")) + "\n",
		"hash non-hex chars": "team-a:" + strings.Repeat("g", 64) + "\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeKeysFile(t, content)
			if _, err := NewStaticAuthenticatorFromKeysFile(path); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestLoadKeysFileRejectsDuplicateHash(t *testing.T) {
	t.Parallel()
	hash := sha256hex("shared")
	content := "team-a:" + hash + "\nteam-b:" + hash + "\n"
	path := writeKeysFile(t, content)
	if _, err := NewStaticAuthenticatorFromKeysFile(path); err == nil {
		t.Fatal("expected error for duplicate hash across key ids")
	}
}

func TestStaticAuthenticatorReloadPicksUpChanges(t *testing.T) {
	t.Parallel()

	v1 := "team-a:" + sha256hex("token-v1") + "\n"
	path := writeKeysFile(t, v1)
	a, err := NewStaticAuthenticatorFromKeysFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(context.Background(), "token-v1"); err != nil {
		t.Fatal(err)
	}

	// Rotate: remove v1, add v2.
	v2 := "team-a:" + sha256hex("token-v2") + "\n"
	if err := os.WriteFile(path, []byte(v2), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Reload(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(context.Background(), "token-v1"); err == nil {
		t.Fatal("v1 token must be rejected after reload")
	}
	if _, err := a.Authenticate(context.Background(), "token-v2"); err != nil {
		t.Fatalf("v2 token must be accepted after reload: %v", err)
	}
}

func TestStaticAuthenticatorReloadKeepsOldStateOnFailure(t *testing.T) {
	t.Parallel()

	good := "team-a:" + sha256hex("token-good") + "\n"
	path := writeKeysFile(t, good)
	a, err := NewStaticAuthenticatorFromKeysFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Replace the file with malformed content; Reload must error and
	// preserve the previous keys.
	if err := os.WriteFile(path, []byte("garbage line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Reload(); err == nil {
		t.Fatal("expected reload to fail")
	}
	if _, err := a.Authenticate(context.Background(), "token-good"); err != nil {
		t.Fatalf("previous keys must be preserved after failed reload: %v", err)
	}
}

func TestStaticAuthenticatorReloadWithoutFileErrors(t *testing.T) {
	t.Parallel()
	a := NewStaticAuthenticator(map[string]Identity{"k": {}})
	if err := a.Reload(); err == nil {
		t.Fatal("expected error reloading an authenticator with no file path")
	}
}
