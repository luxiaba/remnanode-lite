package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luxiaba/remnanode-lite/internal/secret"
	"golang.org/x/sys/unix"
)

func TestValidateSecretCommand(t *testing.T) {
	validJSON := validSecretJSON(t)
	valid := base64.StdEncoding.EncodeToString(validJSON)
	invalidJSON := base64.StdEncoding.EncodeToString(append(validJSON, []byte(`trailing}`)...))

	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "valid", input: valid, want: 0},
		{name: "invalid json", input: invalidJSON, want: 1},
		{name: "oversized", input: strings.Repeat("A", secret.MaxEncodedBytes+1), want: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := validateSecretCommand(strings.NewReader(test.input), &stderr); got != test.want {
				t.Fatalf("validateSecretCommand() = %d, want %d; stderr=%q", got, test.want, stderr.String())
			}
		})
	}
}

func TestValidateSecretCommandAcceptsExactLimitWithLineEnding(t *testing.T) {
	validJSON := validSecretJSON(t)
	rawLength := base64.StdEncoding.DecodedLen(secret.MaxEncodedBytes)
	prefix := validJSON[:len(validJSON)-1]
	const extraPrefix = `,"extra":"`
	const suffix = `"}`
	fillerLength := rawLength - len(prefix) - len(extraPrefix) - len(suffix)
	if fillerLength < 0 {
		t.Fatal("test payload exceeds encoded limit")
	}
	raw := append(append(append([]byte(nil), prefix...), extraPrefix...), strings.Repeat("a", fillerLength)...)
	raw = append(raw, suffix...)
	encoded := base64.StdEncoding.EncodeToString(raw)
	if len(encoded) != secret.MaxEncodedBytes {
		t.Fatalf("encoded length = %d, want %d", len(encoded), secret.MaxEncodedBytes)
	}

	var stderr bytes.Buffer
	if code := validateSecretCommand(strings.NewReader(encoded+"\r\n"), &stderr); code != 0 {
		t.Fatalf("validateSecretCommand() = %d, want 0; stderr=%q", code, stderr.String())
	}
}

func TestCanonicalizeSecretCommand(t *testing.T) {
	t.Parallel()

	canonical := base64.StdEncoding.EncodeToString(validSecretJSON(t))
	source := filepath.Join(t.TempDir(), "secret.key")
	if err := os.WriteFile(source, []byte(canonical+"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name  string
		path  string
		stdin string
	}{
		{name: "file", path: source},
		{name: "stdin", path: "-", stdin: canonical + "\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := canonicalizeSecretCommand(test.path, strings.NewReader(test.stdin), &stdout, &stderr); code != 0 {
				t.Fatalf("canonicalizeSecretCommand() = %d, stderr=%q", code, stderr.String())
			}
			if got := stdout.String(); got != canonical {
				t.Fatalf("canonical output = %q, want %q", got, canonical)
			}
		})
	}
}

func TestCanonicalizeSecretCommandRejectsUnsafeFileWithoutOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	canonical := base64.StdEncoding.EncodeToString(validSecretJSON(t))
	regular := filepath.Join(dir, "regular.key")
	if err := os.WriteFile(regular, []byte(canonical), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "symlink.key")
	if err := os.Symlink(regular, symlink); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := canonicalizeSecretCommand(symlink, strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatal("symlink Secret Key source unexpectedly succeeded")
	}
	if stdout.Len() != 0 {
		t.Fatalf("failed command leaked canonical secret to stdout: %q", stdout.String())
	}

	fifo := filepath.Join(dir, "secret.fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		var fifoStdout, fifoStderr bytes.Buffer
		done <- canonicalizeSecretCommand(fifo, strings.NewReader(""), &fifoStdout, &fifoStderr)
	}()
	select {
	case code := <-done:
		if code == 0 {
			t.Fatal("FIFO Secret Key source unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO Secret Key source blocked instead of failing closed")
	}
}

func TestCanonicalizeSecretCommandReportsOutputFailure(t *testing.T) {
	t.Parallel()

	canonical := base64.StdEncoding.EncodeToString(validSecretJSON(t))
	var stderr bytes.Buffer
	if code := canonicalizeSecretCommand("-", strings.NewReader(canonical), errorWriter{}, &stderr); code == 0 {
		t.Fatal("output failure unexpectedly succeeded")
	}
	if !strings.Contains(stderr.String(), "write canonical SECRET_KEY") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func validSecretJSON(t testing.TB) []byte {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "remnanode-test"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}

	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	payload := secret.Payload{
		CACertPEM:    certPEM,
		JWTPublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})),
		NodeCertPEM:  certPEM,
		NodeKeyPEM:   string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRuntimeEnvPathHonorsFixedLauncherPath(t *testing.T) {
	t.Setenv("REMNANODE_ENV", "/etc/remnanode-lite/node.env")
	if got := runtimeEnvPath(); got != "/etc/remnanode-lite/node.env" {
		t.Fatalf("runtimeEnvPath() = %q", got)
	}
}
