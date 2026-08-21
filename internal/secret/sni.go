package secret

import (
	"crypto/hkdf"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"regexp"
)

const sniHKDFInfo = "rw-v1"

var (
	sniPEMBoundaryRe = regexp.MustCompile(`-----[^-]+-----`)
	sniTLDs          = [...]string{"com", "net", "org", "io", "dev", "app"}
)

// DeriveSNI implements the server-name derivation used by official Node 3.3.x.
func DeriveSNI(caCertPEM, jwtPublicKey string) (string, error) {
	jwtCanonical := canonicalPEMBody(jwtPublicKey)
	caCanonical := canonicalPEMBody(caCertPEM)
	ikm := make([]byte, 0, len(jwtCanonical)+len(caCanonical))
	ikm = append(ikm, jwtCanonical...)
	ikm = append(ikm, caCanonical...)

	okm, err := hkdf.Key(sha256.New, ikm, nil, sniHKDFInfo, 22)
	if err != nil {
		return "", fmt.Errorf("derive SNI: %w", err)
	}
	return hex.EncodeToString(okm[:16]) + "." +
		hex.EncodeToString(okm[16:21]) + "." +
		sniTLDs[int(okm[21])%len(sniTLDs)], nil
}

// MatchesSNI compares an offered server name with the precomputed expected
// value without revealing partial matches through the comparison itself.
func MatchesSNI(offered, expected string) bool {
	if len(offered) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(offered), []byte(expected)) == 1
}

func canonicalPEMBody(pemText string) []byte {
	withoutBoundaries := sniPEMBoundaryRe.ReplaceAllString(pemText, "")
	canonical := make([]byte, 0, len(withoutBoundaries))
	for i := 0; i < len(withoutBoundaries); i++ {
		character := withoutBoundaries[i]
		if character >= 'A' && character <= 'Z' ||
			character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' ||
			character == '+' || character == '/' || character == '=' {
			canonical = append(canonical, character)
		}
	}
	return canonical
}
