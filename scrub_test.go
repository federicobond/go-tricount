package tricount

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// scrubFields are JSON string fields whose values must never reach a committed
// fixture.
var scrubFields = []string{
	"token",
	"client_public_key",
	"server_public_key",
	"encryption_key",
	"public_identifier_token",
	"app_installation_uuid",
	"session_token",
}

// scrubSecrets redacts sensitive values from a recorded response body: the
// fields above wherever they appear, plus any literal strings given (the
// device's app id and public key, the fixture's own sharing token).
func scrubSecrets(body []byte, literals []string) []byte {
	out := body
	for _, field := range scrubFields {
		re := regexp.MustCompile(`("` + field + `"\s*:\s*")(?:[^"\\]|\\.)*(")`)
		out = re.ReplaceAll(out, []byte("${1}REDACTED${2}"))
	}
	for _, literal := range literals {
		if len(literal) < 8 {
			// Too short to be a secret and too likely to appear by accident.
			continue
		}
		out = bytes.ReplaceAll(out, []byte(literal), []byte("REDACTED"))
	}
	return out
}

func TestScrubSecretsRedactsKnownFields(t *testing.T) {
	body := []byte(`{"Response":[
		{"Token":{"token":"sess-super-secret","created":"2026-01-01 00:00:00.000000"}},
		{"UserPerson":{"id":79290957}}
	]}`)

	got := scrubSecrets(body, nil)
	if bytes.Contains(got, []byte("sess-super-secret")) {
		t.Errorf("token survived scrubbing:\n%s", got)
	}
	if !bytes.Contains(got, []byte(`"token":"REDACTED"`)) {
		t.Errorf("token was not replaced with a placeholder:\n%s", got)
	}
	// Unrelated fields must survive, or the fixture is useless.
	if !bytes.Contains(got, []byte("79290957")) {
		t.Errorf("scrubbing removed unrelated data:\n%s", got)
	}
	var v any
	if err := json.Unmarshal(got, &v); err != nil {
		t.Errorf("scrubbed body is not valid JSON: %v\n%s", err, got)
	}
}

func TestScrubSecretsRedactsSessionKeys(t *testing.T) {
	// The session response carries an encryption key and the server's public
	// key alongside the token. All three must go.
	body := []byte(`{"Response":[
		{"Token":{"token":"sess-abc"}},
		{"EncryptionKey":{"encryption_key":"2b31ef62caed89cc719f282b97dc277c"}},
		{"ServerPublicKey":{"server_public_key":"-----BEGIN PUBLIC KEY-----\nabc\n-----END PUBLIC KEY-----"}}
	]}`)
	got := scrubSecrets(body, nil)
	for _, secret := range []string{"sess-abc", "2b31ef62caed89cc719f282b97dc277c", "BEGIN PUBLIC KEY"} {
		if bytes.Contains(got, []byte(secret)) {
			t.Errorf("%q survived scrubbing:\n%s", secret, got)
		}
	}
	var v any
	if err := json.Unmarshal(got, &v); err != nil {
		t.Errorf("scrubbed body is not valid JSON: %v\n%s", err, got)
	}
}

func TestScrubSecretsRedactsSharingToken(t *testing.T) {
	body := []byte(`{"Response":[{"Registry":{"id":1,"public_identifier_token":"tSECRET123","title":"Trip"}}]}`)
	got := scrubSecrets(body, nil)
	if bytes.Contains(got, []byte("tSECRET123")) {
		t.Errorf("sharing token survived scrubbing:\n%s", got)
	}
	if !bytes.Contains(got, []byte("Trip")) {
		t.Errorf("scrubbing removed the title:\n%s", got)
	}
}

func TestScrubSecretsRedactsLiterals(t *testing.T) {
	appID := "11111111-2222-4333-8444-555555555555"
	key := "-----BEGIN RSA PUBLIC KEY-----\nZmFrZQ==\n-----END RSA PUBLIC KEY-----\n"
	body := []byte(`{"app_id":"` + appID + `","note":"` + strings.ReplaceAll(key, "\n", `\n`) + `"}`)

	got := scrubSecrets(body, []string{appID, key})
	if bytes.Contains(got, []byte(appID)) {
		t.Errorf("app id survived scrubbing:\n%s", got)
	}
}

func TestScrubSecretsIgnoresShortLiterals(t *testing.T) {
	// A two-character literal would corrupt half the document.
	body := []byte(`{"title":"Trip to Rome","currency":"EUR"}`)
	got := scrubSecrets(body, []string{"to", "EUR"})
	if !bytes.Equal(got, body) {
		t.Errorf("a short literal was scrubbed:\n%s", got)
	}
}

func TestScrubSecretsHandlesEscapedQuotes(t *testing.T) {
	body := []byte(`{"token":"abc\"def","keep":"value"}`)
	got := scrubSecrets(body, nil)
	if bytes.Contains(got, []byte(`abc\"def`)) {
		t.Errorf("a token containing an escaped quote survived:\n%s", got)
	}
	var v any
	if err := json.Unmarshal(got, &v); err != nil {
		t.Errorf("scrubbed body is not valid JSON: %v\n%s", err, got)
	}
}
