package tricount

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Credentials identify one "device" to the Tricount API. There is no
// email-and-password login: a client registers an installation UUID and an
// RSA public key, and the API issues a session token for it.
//
// The JSON shape matches what the Python tricount-api package writes to
// tricount_credentials.json, so a credentials file works with either
// library. Keep the file: it is the identity your synced tricounts hang
// off. Losing it means registering a new device and re-joining by sharing
// token.
type Credentials struct {
	AppID        string `json:"app_id"`
	PublicKeyPEM string `json:"public_key_pem"`
}

// GenerateCredentials creates a fresh installation UUID and RSA-2048 public
// key. The server requires the key to be unique per installation. The
// private half is discarded: this flow never signs anything with it.
func GenerateCredentials() (Credentials, error) {
	id, err := newUUID()
	if err != nil {
		return Credentials{}, err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return Credentials{}, fmt.Errorf("tricount: generating RSA key: %w", err)
	}
	block := &pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey),
	}
	return Credentials{AppID: id, PublicKeyPEM: string(pem.EncodeToMemory(block))}, nil
}

// LoadCredentials reads credentials previously written by Save.
func LoadCredentials(path string) (Credentials, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, err
	}
	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		return Credentials{}, fmt.Errorf("tricount: reading credentials from %s: %w", path, err)
	}
	if c.AppID == "" || c.PublicKeyPEM == "" {
		return Credentials{}, fmt.Errorf("tricount: credentials in %s are incomplete", path)
	}
	return c, nil
}

// LoadOrGenerateCredentials loads the file when it exists and otherwise
// generates credentials and saves them. It never overwrites an existing
// file.
func LoadOrGenerateCredentials(path string) (Credentials, error) {
	c, err := LoadCredentials(path)
	if err == nil {
		return c, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Credentials{}, err
	}
	c, err = GenerateCredentials()
	if err != nil {
		return Credentials{}, err
	}
	if err := c.Save(path); err != nil {
		return Credentials{}, err
	}
	return c, nil
}

// Save writes the credentials to path with mode 0600. It refuses to
// overwrite an existing file, because overwriting is how a device identity
// gets lost.
func (c Credentials) Save(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("tricount: refusing to overwrite existing credentials at %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(raw, '\n'), 0o600)
}

// newUUID returns a random version 4 UUID. Hand-rolled to keep the module
// dependency-free.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("tricount: reading randomness: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// writeFileAtomic writes data to path via a temporary file in the same
// directory followed by a rename, so a crash cannot leave a half-written
// file where a whole one used to be.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op once the rename succeeds

	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// validUUID reports whether s is a canonical 8-4-4-4-12 hexadecimal UUID.
func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}
