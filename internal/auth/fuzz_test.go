package auth

import (
	"encoding/base64"
	"reflect"
	"testing"
)

const maxFuzzJWTSegmentBytes = 64 << 10

func FuzzDecodeJWTJSON(f *testing.F) {
	for _, raw := range []string{
		`{"alg":"RS256","typ":"JWT"}`,
		`{"exp":2000,"nbf":1000,"aud":["remnawave-node"]}`,
		`{"exp":2000} {}`,
		`null`,
		`[1,{"nested":true}]`,
	} {
		f.Add(base64.RawURLEncoding.EncodeToString([]byte(raw)))
	}
	f.Add("not-base64")

	f.Fuzz(func(t *testing.T, segment string) {
		if len(segment) > maxFuzzJWTSegmentBytes {
			t.Skip()
		}
		var first any
		firstErr := decodeJWTJSON(segment, &first)
		var second any
		secondErr := decodeJWTJSON(segment, &second)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("decodeJWTJSON changed error outcome: first=%v second=%v", firstErr, secondErr)
		}
		if firstErr == nil && !reflect.DeepEqual(first, second) {
			t.Fatalf("decodeJWTJSON is not deterministic: first=%#v second=%#v", first, second)
		}

		firstClaims, firstClaimsErr := decodeJWTClaims(segment)
		secondClaims, secondClaimsErr := decodeJWTClaims(segment)
		if (firstClaimsErr == nil) != (secondClaimsErr == nil) {
			t.Fatalf("decodeJWTClaims changed error outcome: first=%v second=%v", firstClaimsErr, secondClaimsErr)
		}
		if firstClaimsErr == nil {
			if firstClaims == nil {
				t.Fatal("decodeJWTClaims accepted a null claims set")
			}
			if !reflect.DeepEqual(firstClaims, secondClaims) {
				t.Fatalf("decodeJWTClaims is not deterministic: first=%#v second=%#v", firstClaims, secondClaims)
			}
		}
	})
}
