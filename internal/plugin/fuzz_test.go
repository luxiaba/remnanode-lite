package plugin

import (
	"net/netip"
	"strings"
	"testing"
)

func FuzzNFTAddressInput(f *testing.F) {
	f.Add("203.0.113.1", 30.0)
	f.Add("2001:db8::1", 0.0)
	f.Add("::ffff:192.0.2.1", 60.0)
	f.Add("10.0.0.0/8", 1.0)
	f.Add("fe80::1%eth0", 1.0)
	f.Add("fe80::1%eth0\nadd table ip injected", 1.0)
	f.Add("203.0.113.1\nadd table ip injected", 1.0)
	f.Add("not-an-ip", -1.0)

	f.Fuzz(func(t *testing.T, raw string, timeout float64) {
		if len(raw) > maxPluginStringBytes+1 {
			t.Skip()
		}
		v4, v6 := normalizeFilterPrefixes([]string{raw})
		for _, normalized := range append(v4, v6...) {
			prefix, err := netip.ParsePrefix(normalized)
			if err != nil || prefix.Addr().Zone() != "" || prefix.Masked().String() != normalized {
				t.Fatalf("normalizeFilterPrefixes returned non-canonical prefix %q: %v", normalized, err)
			}
		}

		script, err := renderNFTBlock([]BlockIP{{IP: raw, Timeout: timeout}})
		if err == nil {
			address, parseErr := netip.ParseAddr(strings.TrimSpace(raw))
			if parseErr != nil {
				t.Fatalf("renderNFTBlock accepted an invalid address %q", raw)
			}
			if address.Zone() != "" {
				t.Fatalf("renderNFTBlock accepted a scoped address %q", raw)
			}
			if canonical := address.Unmap().String(); !strings.Contains(script, canonical) {
				t.Fatalf("nft block script %q does not contain canonical address %q", script, canonical)
			}
			if strings.ContainsAny(script, ";\r\n") {
				t.Fatalf("nft block script contains an unsafe command separator: %q", script)
			}
		}

		commands, err := renderNFTUnblock([]string{raw})
		if err == nil {
			address, parseErr := netip.ParseAddr(strings.TrimSpace(raw))
			if parseErr != nil {
				t.Fatalf("renderNFTUnblock accepted an invalid address %q", raw)
			}
			if address.Zone() != "" {
				t.Fatalf("renderNFTUnblock accepted a scoped address %q", raw)
			}
			if len(commands) != 2 {
				t.Fatalf("renderNFTUnblock returned %d commands for one address, want 2", len(commands))
			}
			canonical := address.Unmap().String()
			for _, command := range commands {
				if !strings.Contains(command, canonical) || strings.ContainsAny(command, ";\r\n") {
					t.Fatalf("unsafe nft unblock command for %q: %q", raw, command)
				}
			}
		}
	})
}
