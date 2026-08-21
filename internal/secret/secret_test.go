package secret

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestParseSecretKey(t *testing.T) {
	want := newTestPayload(t, validTestCertificateTimes())
	escaped := want
	escaped.CACertPEM = strings.ReplaceAll(escaped.CACertPEM, "\n", `\n`)
	escaped.JWTPublicKey = strings.ReplaceAll(escaped.JWTPublicKey, "\n", `\n`)
	escaped.NodeCertPEM = strings.ReplaceAll(escaped.NodeCertPEM, "\n", `\n`)
	escaped.NodeKeyPEM = strings.ReplaceAll(escaped.NodeKeyPEM, "\n", `\n`)

	payload, err := Parse(encodeTestPayload(t, escaped, ""))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if payload.CACertPEM != strings.TrimSpace(want.CACertPEM) ||
		payload.JWTPublicKey != strings.TrimSpace(want.JWTPublicKey) ||
		payload.NodeCertPEM != strings.TrimSpace(want.NodeCertPEM) ||
		payload.NodeKeyPEM != strings.TrimSpace(want.NodeKeyPEM) {
		t.Fatal("Parse did not normalize all PEM fields")
	}
}

func TestParseSecretKeyBase64Encodings(t *testing.T) {
	payload := newTestPayload(t, validTestCertificateTimes())
	raw := marshalTestPayload(t, payload, `"extra":"???"`)
	tests := []struct {
		name     string
		encoding *base64.Encoding
	}{
		{name: "standard", encoding: base64.StdEncoding},
		{name: "raw standard", encoding: base64.RawStdEncoding},
		{name: "URL", encoding: base64.URLEncoding},
		{name: "raw URL", encoding: base64.RawURLEncoding},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded := test.encoding.EncodeToString(raw)
			got, err := Parse(encoded)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if got.CACertPEM != strings.TrimSpace(payload.CACertPEM) {
				t.Fatal("unexpected parsed payload")
			}
		})
	}
}

func TestParseSecretKeyAllowsUnknownFields(t *testing.T) {
	payload := newTestPayload(t, validTestCertificateTimes())
	tests := []struct {
		name  string
		extra string
	}{
		{name: "string", extra: `"extra":"value"`},
		{name: "number", extra: `"extra":42`},
		{name: "null", extra: `"extra":null`},
		{name: "nested object", extra: `"extra":{"nested":[true,{"value":1}]}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded := base64.StdEncoding.EncodeToString(marshalTestPayload(t, payload, test.extra))
			if _, err := Parse(encoded); err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
		})
	}
}

func TestParseSecretKeyRejectsMissingFields(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(`{"caCertPem":"x"}`))
	if _, err := Parse(encoded); err == nil {
		t.Fatal("expected missing fields to fail")
	}
}

func TestParseSecretKeyRejectsInvalidJSONShape(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "trailing data", raw: `{"caCertPem":"ca","jwtPublicKey":"jwt","nodeCertPem":"cert","nodeKeyPem":"key"}trailing}`},
		{name: "duplicate field", raw: `{"caCertPem":"first","caCertPem":"second","jwtPublicKey":"jwt","nodeCertPem":"cert","nodeKeyPem":"key"}`},
		{name: "non string field", raw: `{"caCertPem":"ca","jwtPublicKey":"jwt","nodeCertPem":"cert","nodeKeyPem":42}`},
		{name: "duplicate unknown field", raw: `{"caCertPem":"ca","jwtPublicKey":"jwt","nodeCertPem":"cert","nodeKeyPem":"key","extra":1,"extra":2}`},
		{name: "second JSON document", raw: `{"caCertPem":"ca","jwtPublicKey":"jwt","nodeCertPem":"cert","nodeKeyPem":"key"} {}`},
		{name: "top level array", raw: `["ca","jwt","cert","key"]`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded := base64.StdEncoding.EncodeToString([]byte(test.raw))
			if _, err := Parse(encoded); err == nil {
				t.Fatal("expected invalid SECRET_KEY JSON to fail")
			}
		})
	}
}

func TestParseSecretKeyEncodedSizeBoundary(t *testing.T) {
	payload := newTestPayload(t, validTestCertificateTimes())
	base := marshalTestPayload(t, payload, `"extra":""`)
	rawLength := base64.StdEncoding.DecodedLen(MaxEncodedBytes)
	fillerLength := rawLength - len(base)
	if fillerLength < 0 {
		t.Fatal("test payload exceeds encoded limit")
	}
	raw := marshalTestPayload(t, payload, `"extra":"`+strings.Repeat("a", fillerLength)+`"`)
	encoded := base64.StdEncoding.EncodeToString(raw)
	if len(encoded) != MaxEncodedBytes {
		t.Fatalf("encoded length = %d, want %d", len(encoded), MaxEncodedBytes)
	}
	if _, err := Parse(encoded); err != nil {
		t.Fatalf("Parse exact limit: %v", err)
	}
	if _, err := Parse(encoded + "A"); err == nil {
		t.Fatal("expected value above encoded size limit to fail")
	}
}

type testCertificateTimes struct {
	caNotBefore   time.Time
	caNotAfter    time.Time
	nodeNotBefore time.Time
	nodeNotAfter  time.Time
}

func validTestCertificateTimes() testCertificateTimes {
	now := time.Now()
	return testCertificateTimes{
		caNotBefore:   now.Add(-time.Hour),
		caNotAfter:    now.Add(time.Hour),
		nodeNotBefore: now.Add(-time.Hour),
		nodeNotAfter:  now.Add(time.Hour),
	}
}

func newTestPayload(t testing.TB, times testCertificateTimes) Payload {
	t.Helper()
	caPublic, caPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "remnanode-test-ca"},
		NotBefore:             times.caNotBefore,
		NotAfter:              times.caNotAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caPublic, caPrivate)
	if err != nil {
		t.Fatal(err)
	}

	nodePublic, nodePrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	nodeTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "remnanode-test"},
		NotBefore:    times.nodeNotBefore,
		NotAfter:     times.nodeNotAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	nodeDER, err := x509.CreateCertificate(rand.Reader, nodeTemplate, caTemplate, nodePublic, caPrivate)
	if err != nil {
		t.Fatal(err)
	}
	nodePrivateDER, err := x509.MarshalPKCS8PrivateKey(nodePrivate)
	if err != nil {
		t.Fatal(err)
	}

	jwtPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	jwtDER, err := x509.MarshalPKIXPublicKey(jwtPublic)
	if err != nil {
		t.Fatal(err)
	}

	return Payload{
		CACertPEM:    string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})),
		JWTPublicKey: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: jwtDER})),
		NodeCertPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: nodeDER})),
		NodeKeyPEM:   string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: nodePrivateDER})),
	}
}

func encodeTestPayload(t testing.TB, payload Payload, extra string) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(marshalTestPayload(t, payload, extra))
}

func marshalTestPayload(t testing.TB, payload Payload, extra string) []byte {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if extra == "" {
		return raw
	}
	raw = raw[:len(raw)-1]
	return append(raw, []byte(","+extra+"}")...)
}
