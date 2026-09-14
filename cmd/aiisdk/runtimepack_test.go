package main

import "testing"

func TestParseBytesReadsCountsAndBinarySuffixes(t *testing.T) {
	for raw, want := range map[string]int64{"1024": 1024, "2K": 2 << 10, "512M": 512 << 20, "8G": 8 << 30, "8GiB": 8 << 30, "1g": 1 << 30, " 7 ": 7} {
		got, err := parseBytes(raw)
		if err != nil || got != want {
			t.Fatalf("%q = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "0", "-1", "1.5G", "G", "8T", "x"} {
		if _, err := parseBytes(raw); err == nil {
			t.Fatalf("%q must be refused", raw)
		}
	}
}
