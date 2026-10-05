package app

import "testing"

func TestExposed(t *testing.T) {
	for addr, want := range map[string]bool{
		":8080":          true,
		"0.0.0.0:8080":   true,
		"[::]:8080":      true,
		"192.168.1.5:80": true,
		"example.com:80": true,
		"127.0.0.1:8080": false,
		"[::1]:8080":     false,
		"localhost:8080": false,
	} {
		if got := exposed(addr); got != want {
			t.Errorf("exposed(%q) = %v, want %v", addr, got, want)
		}
	}
}
