package mark_test

import (
	"os"
	"testing"

	"github.com/kovetskiy/mark/v16/d2"
	"github.com/kovetskiy/mark/v16/mermaid"
)

// TestMain shuts down the browsers this package starts indirectly.
//
// Rendering a d2 or mermaid fenced block goes through renderer, which drives
// the d2 and mermaid packages and their Chrome singletons, so these tests
// orphan browsers exactly as those packages' own tests do. See the note on
// d2's TestMain.
func TestMain(m *testing.M) {
	code := m.Run()
	d2.Cleanup()
	mermaid.Cleanup()
	os.Exit(code)
}
