package math

import (
	"os"
	"testing"

	"github.com/kovetskiy/mark/v16/chrome"
)

// browser is the one the PNG tests rasterise in, started by the first.
var browser = chrome.NewBrowser()

// TestMain shuts down the browser the PNG tests start, rather than leaving it
// orphaned. See the note on d2's TestMain.
func TestMain(m *testing.M) {
	code := m.Run()
	browser.Close()
	os.Exit(code)
}
