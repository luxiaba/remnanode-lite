package secret

import "testing"

func TestDeriveSNIMatchesOfficialNode(t *testing.T) {
	ca := "-----BEGIN CERTIFICATE-----\nQ 0 E=\r\n-----END CERTIFICATE-----"
	jwt := "-----BEGIN PUBLIC KEY-----\r\nSldU\n-----END PUBLIC KEY-----"

	got, err := DeriveSNI(ca, jwt)
	if err != nil {
		t.Fatal(err)
	}
	const want = "7ecd20b759eb52efe7fa95eea11174c5.0c2fd54c7b.dev"
	if got != want {
		t.Fatalf("DeriveSNI() = %q, want official Node result %q", got, want)
	}
}

func TestMatchesSNIRequiresExactValue(t *testing.T) {
	const expected = "7ecd20b759eb52efe7fa95eea11174c5.0c2fd54c7b.dev"
	tests := []struct {
		name    string
		offered string
		want    bool
	}{
		{name: "exact", offered: expected, want: true},
		{name: "empty"},
		{name: "case differs", offered: "7ECD20B759EB52EFE7FA95EEA11174C5.0c2fd54c7b.dev"},
		{name: "suffix", offered: expected + "."},
		{name: "same length differs", offered: "8" + expected[1:]},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := MatchesSNI(test.offered, expected); got != test.want {
				t.Fatalf("MatchesSNI() = %t, want %t", got, test.want)
			}
		})
	}
}
