package config

import (
	"maps"
	"strings"
	"testing"
)

func FuzzParseDotEnvData(f *testing.F) {
	for _, data := range []string{
		"NODE_PORT=2222\nLOW_MEMORY=1\n",
		"export NODE_PORT='2222'\n# comment\n",
		"NODE_PORT=2222\nNODE_PORT=3333\n",
		"EMPTY=\nVALUE=a=b=c\n",
		"=missing-key\n",
		"not-an-assignment\n",
	} {
		f.Add([]byte(data))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxDotEnvBytes+1 {
			t.Skip()
		}
		first, firstErr := parseDotEnvData("node.env", data)
		second, secondErr := parseDotEnvData("node.env", data)
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("parseDotEnvData changed error outcome: first=%v second=%v", firstErr, secondErr)
		}
		if firstErr != nil {
			return
		}
		if !maps.Equal(first, second) {
			t.Fatalf("parseDotEnvData is not deterministic: first=%#v second=%#v", first, second)
		}
		for key := range first {
			if key == "" || key != strings.TrimSpace(key) {
				t.Fatalf("parseDotEnvData accepted a non-canonical key %q", key)
			}
		}
	})
}
