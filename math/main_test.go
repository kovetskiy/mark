package math

import (
	"os"
	"testing"

	"github.com/kovetskiy/mark/v16/chrome"
)

// TestMain shuts down the browser the PNG tests start.
//
// Rendering a formula as PNG rasterises it through the same headless Chrome the
// diagram renderers use, and that browser outlives the test that started it, so
// it is closed here rather than left orphaned. See the note on d2's TestMain.
func TestMain(m *testing.M) {
	code := m.Run()
	chrome.Cleanup()
	os.Exit(code)
}
