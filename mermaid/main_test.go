package mermaid

import (
	"os"
	"testing"
)

// TestMain shuts the render engine down after the package's tests; see the
// note on d2's TestMain for why this is needed. The engine holds a Chrome
// instance in a package-level singleton that only Cleanup cancels.
func TestMain(m *testing.M) {
	code := m.Run()
	Cleanup()
	os.Exit(code)
}
