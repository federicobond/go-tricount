package tricount

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewUUID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		u, err := newUUID()
		if err != nil {
			t.Fatalf("newUUID: %v", err)
		}
		if !uuidRe.MatchString(u) {
			t.Fatalf("newUUID = %q, not a v4 UUID", u)
		}
		if seen[u] {
			t.Fatalf("newUUID repeated %q", u)
		}
		seen[u] = true
	}
}

func TestGenerateCredentials(t *testing.T) {
	c, err := GenerateCredentials()
	if err != nil {
		t.Fatalf("GenerateCredentials: %v", err)
	}
	if !uuidRe.MatchString(c.AppID) {
		t.Errorf("AppID = %q, not a v4 UUID", c.AppID)
	}
	if !strings.HasPrefix(c.PublicKeyPEM, "-----BEGIN RSA PUBLIC KEY-----") {
		t.Errorf("PublicKeyPEM does not look like a PKCS1 PEM block:\n%s", c.PublicKeyPEM)
	}

	block, _ := pem.Decode([]byte(c.PublicKeyPEM))
	if block == nil {
		t.Fatal("PublicKeyPEM is not decodable PEM")
	}
	key, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("ParsePKCS1PublicKey: %v", err)
	}
	if got := key.N.BitLen(); got != 2048 {
		t.Errorf("key size = %d bits, want 2048", got)
	}

	other, err := GenerateCredentials()
	if err != nil {
		t.Fatalf("GenerateCredentials: %v", err)
	}
	if other.PublicKeyPEM == c.PublicKeyPEM {
		t.Error("two generated credentials share a public key")
	}
}

func TestCredentialsSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")

	c, err := GenerateCredentials()
	if err != nil {
		t.Fatalf("GenerateCredentials: %v", err)
	}
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, key := range []string{`"app_id"`, `"public_key_pem"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("saved JSON is missing %s:\n%s", key, raw)
		}
	}

	back, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if back != c {
		t.Errorf("round trip lost data: got %+v, want %+v", back, c)
	}
}

func TestCredentialsSaveRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")

	first, _ := GenerateCredentials()
	if err := first.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	second, _ := GenerateCredentials()
	if err := second.Save(path); err == nil {
		t.Fatal("Save overwrote an existing credentials file")
	}

	back, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if back != first {
		t.Error("the original credentials were not preserved")
	}
}

func TestLoadOrGenerateCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "creds.json")

	generated, err := LoadOrGenerateCredentials(path)
	if err != nil {
		t.Fatalf("first LoadOrGenerateCredentials: %v", err)
	}
	if generated.AppID == "" {
		t.Fatal("generated credentials are empty")
	}

	loaded, err := LoadOrGenerateCredentials(path)
	if err != nil {
		t.Fatalf("second LoadOrGenerateCredentials: %v", err)
	}
	if loaded != generated {
		t.Error("LoadOrGenerateCredentials regenerated instead of loading")
	}
}

func TestWriteFileAtomicLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := writeFileAtomic(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "out.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory contains %v, want just out.json", names)
	}
}
