//go:build windows

package shim

// Is is false on Windows, which has no PATH shims.
func Is(string) bool { return false }

// Main is never reached on Windows.
func Main() int { return 1 }
