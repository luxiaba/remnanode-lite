package secret

import (
	"encoding/pem"
	"strings"
	"testing"
	"time"
)

func TestPayloadIntegrityRejectsInvalidCertificateTimes(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		times testCertificateTimes
		want  string
	}{
		{
			name: "CA not yet valid",
			times: testCertificateTimes{
				caNotBefore: now.Add(time.Hour), caNotAfter: now.Add(2 * time.Hour),
				nodeNotBefore: now.Add(-time.Hour), nodeNotAfter: now.Add(time.Hour),
			},
			want: "CA certificate: is not valid before",
		},
		{
			name: "CA expired",
			times: testCertificateTimes{
				caNotBefore: now.Add(-2 * time.Hour), caNotAfter: now.Add(-time.Hour),
				nodeNotBefore: now.Add(-time.Hour), nodeNotAfter: now.Add(time.Hour),
			},
			want: "CA certificate: expired",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := newTestPayload(t, test.times)
			if err := payload.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestPayloadIntegrityLeavesNodeCertificateTimeToTLS(t *testing.T) {
	now := time.Now()
	payload := newTestPayload(t, testCertificateTimes{
		caNotBefore: now.Add(-2 * time.Hour), caNotAfter: now.Add(time.Hour),
		nodeNotBefore: now.Add(-2 * time.Hour), nodeNotAfter: now.Add(-time.Hour),
	})
	if err := payload.Validate(); err != nil {
		t.Fatalf("official startup checks reject an expired node certificate: %v", err)
	}
}

func TestPayloadIntegrityRejectsBrokenTrustMaterial(t *testing.T) {
	valid := newTestPayload(t, validTestCertificateTimes())
	other := newTestPayload(t, validTestCertificateTimes())
	tests := []struct {
		name   string
		mutate func(*Payload)
		want   string
	}{
		{
			name: "malformed CA",
			mutate: func(payload *Payload) {
				payload.CACertPEM = "not a certificate"
			},
			want: "CA certificate: PEM certificate could not be decoded",
		},
		{
			name: "CA self-signature",
			mutate: func(payload *Payload) {
				payload.CACertPEM = corruptCertificateSignature(t, payload.CACertPEM)
			},
			want: "CA certificate is not self-signed",
		},
		{
			name: "malformed node certificate",
			mutate: func(payload *Payload) {
				payload.NodeCertPEM = "not a certificate"
			},
			want: "node certificate: PEM certificate could not be decoded",
		},
		{
			name: "node signed by another CA",
			mutate: func(payload *Payload) {
				payload.NodeCertPEM = other.NodeCertPEM
				payload.NodeKeyPEM = other.NodeKeyPEM
			},
			want: "node certificate was not signed by its CA",
		},
		{
			name: "malformed node key",
			mutate: func(payload *Payload) {
				payload.NodeKeyPEM = "not a private key"
			},
			want: "node private key: PEM private key could not be decoded",
		},
		{
			name: "node key mismatch",
			mutate: func(payload *Payload) {
				payload.NodeKeyPEM = other.NodeKeyPEM
			},
			want: "node private key does not match its certificate",
		},
		{
			name: "malformed JWT public key",
			mutate: func(payload *Payload) {
				payload.JWTPublicKey = "not a public key"
			},
			want: "JWT public key: PEM public key could not be decoded",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := valid
			test.mutate(&payload)
			if err := payload.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate error = %v, want %q", err, test.want)
			}
		})
	}
}

func corruptCertificateSignature(t *testing.T, certificatePEM string) string {
	t.Helper()
	block, _ := pem.Decode([]byte(certificatePEM))
	if block == nil || len(block.Bytes) == 0 {
		t.Fatal("invalid certificate fixture")
	}
	der := append([]byte(nil), block.Bytes...)
	der[len(der)-1] ^= 1
	return string(pem.EncodeToMemory(&pem.Block{Type: block.Type, Bytes: der}))
}
