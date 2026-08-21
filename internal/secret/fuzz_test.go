package secret

import (
	"encoding/base64"
	"testing"
)

func FuzzParse(f *testing.F) {
	valid := marshalTestPayload(f, newTestPayload(f, validTestCertificateTimes()), `"extra":"???"`)
	encodedSeeds := make(map[string]struct{}, 4)
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		encoded := encoding.EncodeToString(valid)
		if _, duplicate := encodedSeeds[encoded]; duplicate {
			f.Fatalf("base64 fuzz seed %q is not distinct", encoded)
		}
		encodedSeeds[encoded] = struct{}{}
		if _, err := Parse(encoded); err != nil {
			f.Fatalf("valid base64 fuzz seed failed to parse: %v", err)
		}
		f.Add(encoded)
	}
	for _, raw := range []string{
		`{"caCertPem":"ca","caCertPem":"duplicate","jwtPublicKey":"jwt","nodeCertPem":"cert","nodeKeyPem":"key"}`,
		`{"caCertPem":"ca","jwtPublicKey":"jwt","nodeCertPem":"cert","nodeKeyPem":"key"} {}`,
		`{"caCertPem":"ca"}`,
	} {
		f.Add(base64.StdEncoding.EncodeToString([]byte(raw)))
	}
	f.Add("not-base64")

	f.Fuzz(func(t *testing.T, encoded string) {
		if len(encoded) > MaxEncodedBytes+1 {
			t.Skip()
		}
		payload, err := Parse(encoded)
		if err != nil {
			return
		}
		if err := payload.Validate(); err != nil {
			t.Fatalf("Parse accepted an invalid payload: %v", err)
		}
		again, err := Parse(encoded)
		if err != nil || again != payload {
			t.Fatalf("Parse is not deterministic: first=%#v second=%#v error=%v", payload, again, err)
		}
	})
}
